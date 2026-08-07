package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursorapi"
)

const cursorAPIFailureMessage = "Failed to call Cursor API"

type userIDContextKey struct{}

type cachedResponse struct {
	response  cursorapi.Response
	expiresAt time.Time
}

type cacheKey struct {
	name   string
	userID string
}

type responseCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[cacheKey]cachedResponse
}

func (c *responseCache) initialize(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
	c.entries = make(map[cacheKey]cachedResponse)
}

func (c *responseCache) get(cacheName, userID string) (cursorapi.Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cacheKey{name: cacheName, userID: userID}
	entry, ok := c.entries[key]
	if !ok {
		return cursorapi.Response{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return cursorapi.Response{}, false
	}
	return entry.response, true
}

func (c *responseCache) set(cacheName, userID string, response cursorapi.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := cachedResponse{
		response: cursorapi.Response{
			StatusCode: response.StatusCode,
			Header:     response.Header.Clone(),
			Body:       append([]byte(nil), response.Body...),
		},
		expiresAt: time.Now().Add(c.ttl),
	}
	c.entries[cacheKey{name: cacheName, userID: userID}] = entry
}

func (c *responseCache) invalidate(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.userID == userID {
			delete(c.entries, key)
		}
	}
}

func (p *Plugin) initRouter() {
	p.router = mux.NewRouter()
	api := p.router.PathPrefix("/api/v1").Subrouter()
	api.Use(p.requireUser)

	api.HandleFunc("/key", p.getKey).Methods(http.MethodGet)
	api.HandleFunc("/key", p.putKey).Methods(http.MethodPut)
	api.HandleFunc("/key", p.deleteKey).Methods(http.MethodDelete)
	api.HandleFunc("/agents", p.listAgents).Methods(http.MethodGet)
	api.HandleFunc("/agents", p.createAgent).Methods(http.MethodPost)
	api.HandleFunc("/agents/{id}", p.getAgent).Methods(http.MethodGet)
	api.HandleFunc("/agents/{id}", p.deleteAgent).Methods(http.MethodDelete)
	api.HandleFunc("/agents/{id}/archive", p.archiveAgent).Methods(http.MethodPost)
	api.HandleFunc("/agents/{id}/unarchive", p.unarchiveAgent).Methods(http.MethodPost)
	api.HandleFunc("/agents/{id}/messages", p.listAgentMessages).Methods(http.MethodGet)
	api.HandleFunc("/agents/{id}/followup", p.addFollowup).Methods(http.MethodPost)
	api.HandleFunc("/agents/{id}/runs/{runId}", p.getRun).Methods(http.MethodGet)
	api.HandleFunc("/agents/{id}/runs/{runId}/cancel", p.cancelRun).Methods(http.MethodPost)
	api.HandleFunc("/agents/{id}/runs/{runId}/stream", p.streamRun).Methods(http.MethodGet)
	api.HandleFunc("/models", p.listModels).Methods(http.MethodGet)
	api.HandleFunc("/repositories", p.listRepositories).Methods(http.MethodGet)
}

func (p *Plugin) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-Id")
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "Unauthorized", "")
			return
		}
		ctx := context.WithValue(r.Context(), userIDContextKey{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userIDFromRequest(r *http.Request) string {
	userID, _ := r.Context().Value(userIDContextKey{}).(string)
	return userID
}

func (p *Plugin) getKey(w http.ResponseWriter, r *http.Request) {
	configured, email, err := p.keyStore.Info(userIDFromRequest(r))
	if err != nil {
		p.logError("Failed to read API key configuration", err)
		writeError(w, http.StatusInternalServerError, "Failed to read API key configuration", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": configured, "email": email})
}

type putKeyRequest struct {
	APIKey string `json:"apiKey"`
}

func (p *Plugin) putKey(w http.ResponseWriter, r *http.Request) {
	var body putKeyRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		return
	}
	body.APIKey = strings.TrimSpace(body.APIKey)
	if body.APIKey == "" {
		writeError(w, http.StatusBadRequest, "API key is required", "")
		return
	}

	response, err := p.cursorClient(body.APIKey).GetMe(r.Context())
	if err != nil {
		p.logError("Failed to validate Cursor API key", err)
		writeError(w, http.StatusBadGateway, "Failed to validate API key", "")
		return
	}
	if response.StatusCode == http.StatusUnauthorized {
		writeError(w, http.StatusBadRequest, "Invalid API key", "")
		return
	}
	if !response.Successful() {
		writeUpstream(w, response)
		return
	}
	info, err := cursorapi.Decode[cursorapi.APIKeyInfo](response)
	if err != nil {
		p.logError("Failed to decode Cursor account response", err)
		writeError(w, http.StatusBadGateway, "Cursor returned an invalid account response", "")
		return
	}
	userID := userIDFromRequest(r)
	if err := p.keyStore.Set(userID, body.APIKey, info.UserEmail); err != nil {
		p.logError("Failed to store Cursor API key", err)
		writeError(w, http.StatusInternalServerError, "Failed to store API key", "")
		return
	}
	p.cache.invalidate(userID)
	p.hydration.invalidateUser(userID)
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "email": info.UserEmail})
}

func (p *Plugin) deleteKey(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromRequest(r)
	if err := p.keyStore.Delete(userID); err != nil {
		p.logError("Failed to delete Cursor API key", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete API key", "")
		return
	}
	p.cache.invalidate(userID)
	p.hydration.invalidateUser(userID)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Plugin) listAgents(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	query := make(url.Values)
	requestQuery := r.URL.Query()
	for _, key := range []string{"limit", "cursor", "includeArchived"} {
		for _, value := range requestQuery[key] {
			query.Add(key, value)
		}
	}
	response, err := client.ListAgents(r.Context(), query)
	if err != nil {
		p.logError(cursorAPIFailureMessage, err)
		writeError(w, http.StatusBadGateway, cursorAPIFailureMessage, "")
		return
	}
	if !response.Successful() {
		writeUpstream(w, response)
		return
	}
	list, err := cursorapi.Decode[cursorapi.ListAgentsResponse](response)
	if err != nil {
		p.logError("Failed to decode Cursor agents response", err)
		writeError(w, http.StatusBadGateway, "Cursor returned an invalid agents response", "")
		return
	}
	hydrated := cursorapi.HydratedListAgentsResponse{
		Items: p.hydrateAgents(
			r.Context(),
			userIDFromRequest(r),
			client,
			list.Items,
			true,
		),
		NextCursor: list.NextCursor,
	}
	copyUpstreamHeaders(w.Header(), response.Header)
	writeJSON(w, response.StatusCode, hydrated)
}

type createAgentRequest struct {
	Prompt       string `json:"prompt"`
	Repository   string `json:"repository"`
	Ref          string `json:"ref"`
	Model        string `json:"model"`
	AutoCreatePR *bool  `json:"autoCreatePr"`
}

func (p *Plugin) createAgent(w http.ResponseWriter, r *http.Request) {
	var body createAgentRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		return
	}
	if strings.TrimSpace(body.Prompt) == "" || strings.TrimSpace(body.Repository) == "" {
		writeError(w, http.StatusBadRequest, "prompt and repository are required", "")
		return
	}
	request := cursorapi.NewCreateAgentRequest(
		body.Prompt,
		body.Repository,
		body.Ref,
		body.Model,
		body.AutoCreatePR,
	)
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	response, err := client.CreateAgent(r.Context(), request)
	p.proxyResponse(w, response, err)
}

func (p *Plugin) getAgent(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	response, err := client.GetAgent(r.Context(), mux.Vars(r)["id"])
	p.proxyResponse(w, response, err)
}

func (p *Plugin) deleteAgent(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	response, err := client.DeleteAgent(r.Context(), mux.Vars(r)["id"])
	p.proxyNoContent(w, response, err)
}

func (p *Plugin) archiveAgent(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	response, err := client.ArchiveAgent(r.Context(), mux.Vars(r)["id"])
	p.proxyNoContent(w, response, err)
}

func (p *Plugin) unarchiveAgent(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	response, err := client.UnarchiveAgent(r.Context(), mux.Vars(r)["id"])
	p.proxyNoContent(w, response, err)
}

func (p *Plugin) listAgentMessages(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	response, err := client.GetConversation(r.Context(), mux.Vars(r)["id"])
	p.proxyResponse(w, response, err)
}

type followupRequest struct {
	Prompt string `json:"prompt"`
}

func (p *Plugin) addFollowup(w http.ResponseWriter, r *http.Request) {
	var body followupRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required", "")
		return
	}
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	request := cursorapi.CreateRunRequest{Prompt: cursorapi.Prompt{Text: body.Prompt}}
	response, err := client.CreateRun(r.Context(), mux.Vars(r)["id"], request)
	p.proxyResponse(w, response, err)
}

func (p *Plugin) getRun(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	response, err := client.GetRun(r.Context(), vars["id"], vars["runId"])
	p.proxyResponse(w, response, err)
}

func (p *Plugin) cancelRun(w http.ResponseWriter, r *http.Request) {
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	response, err := client.CancelRun(r.Context(), vars["id"], vars["runId"])
	p.proxyNoContent(w, response, err)
}

func (p *Plugin) streamRun(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		p.logError("Failed to start Cursor stream", errors.New("response writer does not support flushing"))
		writeError(w, http.StatusInternalServerError, "Streaming is not supported", "")
		return
	}
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	response, err := client.StreamRun(r.Context(), vars["id"], vars["runId"], r.Header.Get("Last-Event-ID"))
	if err != nil {
		p.logError("Failed to connect to Cursor stream", err)
		writeError(w, http.StatusBadGateway, "Failed to connect to Cursor stream", "")
		return
	}
	defer func() { _ = response.Body.Close() }()
	if !response.Successful() {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			p.logError("Failed to read Cursor stream response", readErr)
			writeError(w, http.StatusBadGateway, "Failed to read Cursor stream response", "")
			return
		}
		writeUpstream(w, cursorapi.Response{
			StatusCode: response.StatusCode,
			Header:     response.Header,
			Body:       body,
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if retention := response.Header.Get("X-Cursor-Stream-Retention-Seconds"); retention != "" {
		w.Header().Set("X-Cursor-Stream-Retention-Seconds", retention)
	}
	w.WriteHeader(response.StatusCode)

	reader := bufio.NewReader(response.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			if _, err := io.WriteString(w, line); err != nil {
				return
			}
			if line == "\n" || line == "\r\n" {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) && line != "" {
				flusher.Flush()
			}
			return
		}
	}
}

func (p *Plugin) listModels(w http.ResponseWriter, r *http.Request) {
	p.proxyCached(w, r, "models", func(client *cursorapi.Client) (cursorapi.Response, error) {
		return client.ListModels(r.Context())
	})
}

func (p *Plugin) listRepositories(w http.ResponseWriter, r *http.Request) {
	p.proxyCached(w, r, "repositories", func(client *cursorapi.Client) (cursorapi.Response, error) {
		return client.ListRepositories(r.Context())
	})
}

func (p *Plugin) proxyCached(
	w http.ResponseWriter,
	r *http.Request,
	cacheName string,
	fetch func(*cursorapi.Client) (cursorapi.Response, error),
) {
	userID := userIDFromRequest(r)
	if response, ok := p.cache.get(cacheName, userID); ok {
		writeUpstream(w, response)
		return
	}
	client, ok := p.clientForRequest(w, r)
	if !ok {
		return
	}
	response, err := fetch(client)
	if err != nil {
		p.logError(cursorAPIFailureMessage, err)
		writeError(w, http.StatusBadGateway, cursorAPIFailureMessage, "")
		return
	}
	if response.Successful() {
		p.cache.set(cacheName, userID, response)
	}
	writeUpstream(w, response)
}

func (p *Plugin) clientForRequest(w http.ResponseWriter, r *http.Request) (*cursorapi.Client, bool) {
	apiKey, _, err := p.keyStore.Get(userIDFromRequest(r))
	if errors.Is(err, errAPIKeyNotFound) {
		writeError(
			w,
			http.StatusForbidden,
			"Configure your Cursor API key in the Cursor plugin panel in Mattermost",
			"api_key_not_configured",
		)
		return nil, false
	}
	if err != nil {
		p.logError("Failed to load Cursor API key", err)
		writeError(w, http.StatusInternalServerError, "Failed to load API key", "")
		return nil, false
	}
	return p.cursorClient(apiKey), true
}

func (p *Plugin) proxyResponse(w http.ResponseWriter, response cursorapi.Response, err error) {
	if err != nil {
		p.logError(cursorAPIFailureMessage, err)
		writeError(w, http.StatusBadGateway, cursorAPIFailureMessage, "")
		return
	}
	writeUpstream(w, response)
}

func (p *Plugin) proxyNoContent(w http.ResponseWriter, response cursorapi.Response, err error) {
	if err != nil {
		p.logError(cursorAPIFailureMessage, err)
		writeError(w, http.StatusBadGateway, cursorAPIFailureMessage, "")
		return
	}
	if !response.Successful() {
		writeUpstream(w, response)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, destination any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body", "")
		return err
	}
	return nil
}

func writeUpstream(w http.ResponseWriter, response cursorapi.Response) {
	copyUpstreamHeaders(w.Header(), response.Header)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)
}

func copyUpstreamHeaders(destination, source http.Header) {
	for _, header := range []string{
		"Content-Type",
		"Retry-After",
		"X-RateLimit-Limit",
		"X-RateLimit-Remaining",
		"X-RateLimit-Reset",
	} {
		if value := source.Get(header); value != "" {
			destination.Set(header, value)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	response := map[string]string{"error": message}
	if code != "" {
		response["code"] = code
	}
	writeJSON(w, status, response)
}
