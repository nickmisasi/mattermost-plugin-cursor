package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
)

// initRouter initializes the HTTP router for the plugin.
func (p *Plugin) initRouter() *mux.Router {
	router := mux.NewRouter()

	// GitHub webhook endpoint -- NO auth middleware (uses HMAC signature verification).
	router.HandleFunc("/api/v1/webhooks/github", p.handleGitHubWebhook).Methods(http.MethodPost)

	// All other API routes require a logged-in Mattermost user.
	authedRouter := router.PathPrefix("/api/v1").Subrouter()
	authedRouter.Use(p.MattermostAuthorizationRequired)

	// Dialog submission endpoint for /cursor settings.
	authedRouter.HandleFunc("/dialog/settings", p.handleSettingsDialogSubmission).Methods(http.MethodPost)

	// Phase 4: REST endpoints for the webapp frontend.
	authedRouter.HandleFunc("/agents", p.handleGetAgents).Methods(http.MethodGet)
	authedRouter.HandleFunc("/agents/{id}", p.handleGetAgent).Methods(http.MethodGet)
	authedRouter.HandleFunc("/agents/{id}/followup", p.handleAddFollowup).Methods(http.MethodPost)
	authedRouter.HandleFunc("/agents/{id}", p.handleCancelAgent).Methods(http.MethodDelete)

	// Admin-only routes.
	adminRouter := authedRouter.PathPrefix("/admin").Subrouter()
	adminRouter.Use(p.RequireSystemAdmin)
	adminRouter.HandleFunc("/health", p.handleHealthCheck).Methods(http.MethodGet)

	return router
}

func (p *Plugin) MattermostAuthorizationRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireSystemAdmin is middleware that rejects requests from non-admin users.
func (p *Plugin) RequireSystemAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		if !p.isSystemAdmin(userID) {
			http.Error(w, "Forbidden: system admin required", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HealthResponse is the JSON response from the health check endpoint.
type HealthResponse struct {
	Healthy          bool         `json:"healthy"`
	CursorAPI        HealthStatus `json:"cursor_api"`
	ActiveAgentCount int          `json:"active_agent_count"`
	Configuration    HealthStatus `json:"configuration"`
	PluginVersion    string       `json:"plugin_version"`
}

// HealthStatus represents the health of a single subsystem.
type HealthStatus struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func (p *Plugin) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()

	response := HealthResponse{
		PluginVersion: "0.1.0",
	}

	// 1. Validate configuration.
	if err := config.IsValid(); err != nil {
		response.Configuration = HealthStatus{OK: false, Message: err.Error()}
	} else {
		response.Configuration = HealthStatus{OK: true}
	}

	// 2. Test Cursor API connectivity.
	if config.CursorAPIKey == "" {
		response.CursorAPI = HealthStatus{OK: false, Message: "Cursor API key not configured"}
	} else {
		cursorClient := p.getCursorClient()
		if cursorClient == nil {
			response.CursorAPI = HealthStatus{OK: false, Message: "Cursor client not initialized"}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_, err := cursorClient.GetMe(ctx)
			if err != nil {
				response.CursorAPI = HealthStatus{OK: false, Message: fmt.Sprintf("Cursor API unreachable: %s", err.Error())}
			} else {
				response.CursorAPI = HealthStatus{OK: true}
			}
		}
	}

	// 3. Count active agents.
	if p.kvstore != nil {
		activeAgents, err := p.kvstore.ListActiveAgents()
		if err != nil {
			p.API.LogError("Failed to list active agents for health check", "error", err.Error())
			response.ActiveAgentCount = -1 // Indicates error.
		} else {
			response.ActiveAgentCount = len(activeAgents)
		}
	}

	// 4. Overall health: healthy only if config is valid AND Cursor API is reachable.
	response.Healthy = response.Configuration.OK && response.CursorAPI.OK

	// 5. Write response.
	w.Header().Set("Content-Type", "application/json")
	if !response.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		p.API.LogError("Failed to encode health response", "error", err.Error())
	}
}

// isSystemAdmin checks if the user is a system admin.
func (p *Plugin) isSystemAdmin(userID string) bool {
	if p.client == nil {
		return false
	}
	user, err := p.client.User.Get(userID)
	if err != nil {
		return false
	}
	return user.IsSystemAdmin()
}

// --- Phase 4: REST API handlers for webapp frontend ---

// AgentResponse is the JSON representation of an agent for the webapp.
type AgentResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	Prompt     string `json:"prompt"`
	PrURL      string `json:"pr_url"`
	CursorURL  string `json:"cursor_url"`
	ChannelID  string `json:"channel_id"`
	PostID     string `json:"post_id"`
	RootPostID string `json:"root_post_id"`
	Summary    string `json:"summary"`
	Model      string `json:"model"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// AgentsListResponse is the response from GET /api/v1/agents.
type AgentsListResponse struct {
	Agents []AgentResponse `json:"agents"`
}

// FollowupRequestBody is the request body for POST /api/v1/agents/{id}/followup.
type FollowupRequestBody struct {
	Message string `json:"message"`
}

// StatusOKResponse is a generic OK response.
type StatusOKResponse struct {
	Status string `json:"status"`
}

func (p *Plugin) handleGetAgents(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")

	agents, err := p.kvstore.GetAgentsByUser(userID)
	if err != nil {
		p.API.LogError("Failed to get agents by user", "userID", userID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	resp := AgentsListResponse{
		Agents: make([]AgentResponse, 0, len(agents)),
	}
	for _, a := range agents {
		resp.Agents = append(resp.Agents, AgentResponse{
			ID:         a.CursorAgentID,
			Status:     a.Status,
			Repository: a.Repository,
			Branch:     a.Branch,
			PrURL:      a.PrURL,
			CursorURL:  fmt.Sprintf("https://cursor.com/agents/%s", a.CursorAgentID),
			ChannelID:  a.ChannelID,
			PostID:     a.PostID,
			Prompt:     a.Prompt,
			Model:      a.Model,
			Summary:    a.Summary,
			CreatedAt:  a.CreatedAt,
			UpdatedAt:  a.UpdatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Plugin) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	// Optionally refresh status from Cursor API.
	cursorClient := p.getCursorClient()
	if cursorClient != nil && !cursor.AgentStatus(record.Status).IsTerminal() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if remoteAgent, apiErr := cursorClient.GetAgent(ctx, agentID); apiErr == nil {
			if string(remoteAgent.Status) != record.Status {
				record.Status = string(remoteAgent.Status)
				if remoteAgent.Target.PrURL != "" {
					record.PrURL = remoteAgent.Target.PrURL
				}
				if remoteAgent.Summary != "" {
					record.Summary = remoteAgent.Summary
				}
				record.UpdatedAt = time.Now().UnixMilli()
				_ = p.kvstore.SaveAgent(record)
			}
		}
	}

	resp := AgentResponse{
		ID:         record.CursorAgentID,
		Status:     record.Status,
		Repository: record.Repository,
		Branch:     record.Branch,
		PrURL:      record.PrURL,
		CursorURL:  fmt.Sprintf("https://cursor.com/agents/%s", record.CursorAgentID),
		ChannelID:  record.ChannelID,
		PostID:     record.PostID,
		Prompt:     record.Prompt,
		Model:      record.Model,
		Summary:    record.Summary,
		CreatedAt:  record.CreatedAt,
		UpdatedAt:  record.UpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Plugin) handleAddFollowup(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	var reqBody FollowupRequestBody
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if reqBody.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	if record.Status != string(cursor.AgentStatusRunning) {
		http.Error(w, "Agent is not in RUNNING state", http.StatusBadRequest)
		return
	}

	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		http.Error(w, "Cursor client not configured", http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, apiErr := cursorClient.AddFollowup(ctx, agentID, cursor.FollowupRequest{
		Prompt: cursor.Prompt{Text: reqBody.Message},
	})
	if apiErr != nil {
		p.API.LogError("Failed to add followup", "agentID", agentID, "error", apiErr.Error())
		http.Error(w, "Failed to send follow-up to Cursor API", http.StatusBadGateway)
		return
	}

	// Post a thread reply via bot.
	if record.PostID != "" {
		_, _ = p.API.CreatePost(&model.Post{
			UserId:    p.getBotUserID(),
			ChannelId: record.ChannelID,
			RootId:    record.PostID,
			Message:   fmt.Sprintf(":speech_balloon: Follow-up sent: %s", reqBody.Message),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(StatusOKResponse{Status: "ok"})
}

func (p *Plugin) handleCancelAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	status := cursor.AgentStatus(record.Status)
	if status.IsTerminal() {
		http.Error(w, fmt.Sprintf("Agent is already in %s state", record.Status), http.StatusBadRequest)
		return
	}

	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		http.Error(w, "Cursor client not configured", http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, apiErr := cursorClient.StopAgent(ctx, agentID); apiErr != nil {
		p.API.LogError("Failed to stop agent via Cursor API", "agentID", agentID, "error", apiErr.Error())
		http.Error(w, "Failed to stop agent via Cursor API", http.StatusBadGateway)
		return
	}

	// Update KV store.
	record.Status = string(cursor.AgentStatusStopped)
	if err := p.kvstore.SaveAgent(record); err != nil {
		p.API.LogError("Failed to update agent record", "agentID", agentID, "error", err.Error())
	}

	// Update thread.
	if record.TriggerPostID != "" {
		p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
		p.addReaction(record.TriggerPostID, "no_entry_sign")
	}
	if record.PostID != "" {
		_, _ = p.API.CreatePost(&model.Post{
			UserId:    p.getBotUserID(),
			ChannelId: record.ChannelID,
			RootId:    record.PostID,
			Message:   ":no_entry_sign: Agent was cancelled via the dashboard.",
		})
	}

	// Publish WebSocket event.
	p.publishAgentStatusChange(record)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(StatusOKResponse{Status: "ok"})
}
