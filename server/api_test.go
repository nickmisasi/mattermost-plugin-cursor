package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursorapi"
)

func TestAuthenticationMiddleware(t *testing.T) {
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		withUserID bool
		wantStatus int
	}{
		{name: "missing user", wantStatus: http.StatusUnauthorized},
		{name: "authenticated", withUserID: true, wantStatus: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/key", nil)
			if test.withUserID {
				request.Header.Set("Mattermost-User-Id", testUserID)
			}
			recorder := httptest.NewRecorder()
			p.router.ServeHTTP(recorder, request)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		})
	}
}

func TestKeyStoreEncryptionRoundTrip(t *testing.T) {
	kv := newMemoryKV()
	store, err := NewKeyStore(kv)
	require.NoError(t, err)

	require.NoError(t, store.Set(testUserID, "secret-api-key", "developer@example.com"))
	stored, appErr := kv.KVGet(apiKeyPrefix + testUserID)
	require.Nil(t, appErr)
	assert.NotContains(t, string(stored), "secret-api-key")

	apiKey, email, err := store.Get(testUserID)
	require.NoError(t, err)
	assert.Equal(t, "secret-api-key", apiKey)
	assert.Equal(t, "developer@example.com", email)

	reloaded, err := NewKeyStore(kv)
	require.NoError(t, err)
	apiKey, email, err = reloaded.Get(testUserID)
	require.NoError(t, err)
	assert.Equal(t, "secret-api-key", apiKey)
	assert.Equal(t, "developer@example.com", email)
}

func TestKeyEndpoints(t *testing.T) {
	p, kv, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/me", r.URL.Path)
		assert.Equal(t, "Bearer valid-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"apiKeyName":"test","createdAt":"2026-01-01T00:00:00Z","userEmail":"dev@example.com"}`)
	}))

	put := authenticatedRequest(http.MethodPut, "/api/v1/key", strings.NewReader(`{"apiKey":"valid-key"}`))
	putRecorder := httptest.NewRecorder()
	p.router.ServeHTTP(putRecorder, put)
	require.Equal(t, http.StatusOK, putRecorder.Code)
	assert.JSONEq(t, `{"configured":true,"email":"dev@example.com"}`, putRecorder.Body.String())

	encrypted, appErr := kv.KVGet(apiKeyPrefix + testUserID)
	require.Nil(t, appErr)
	assert.NotContains(t, string(encrypted), "valid-key")

	getRecorder := httptest.NewRecorder()
	p.router.ServeHTTP(
		getRecorder,
		authenticatedRequest(http.MethodGet, "/api/v1/key", nil),
	)
	require.Equal(t, http.StatusOK, getRecorder.Code)
	assert.JSONEq(t, `{"configured":true,"email":"dev@example.com"}`, getRecorder.Body.String())

	deleteRecorder := httptest.NewRecorder()
	p.router.ServeHTTP(
		deleteRecorder,
		authenticatedRequest(http.MethodDelete, "/api/v1/key", nil),
	)
	assert.Equal(t, http.StatusNoContent, deleteRecorder.Code)

	getAfterDelete := httptest.NewRecorder()
	p.router.ServeHTTP(
		getAfterDelete,
		authenticatedRequest(http.MethodGet, "/api/v1/key", nil),
	)
	assert.JSONEq(t, `{"configured":false,"email":""}`, getAfterDelete.Body.String())
}

func TestPutKeyRejectsInvalidKey(t *testing.T) {
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"no"}}`)
	}))
	recorder := httptest.NewRecorder()
	p.router.ServeHTTP(
		recorder,
		authenticatedRequest(http.MethodPut, "/api/v1/key", strings.NewReader(`{"apiKey":"bad"}`)),
	)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.JSONEq(t, `{"error":"Invalid API key"}`, recorder.Body.String())
}

func TestCreateAgentBoundaryMapping(t *testing.T) {
	var received map[string]any
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/agents", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"agent":{"id":"bc-1"},"run":{"id":"run-1"}}`)
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	recorder := httptest.NewRecorder()
	body := `{
		"prompt":"fix the bug",
		"repository":"https://github.com/acme/repo",
		"ref":"main",
		"model":"composer-2",
		"autoCreatePr":true
	}`
	p.router.ServeHTTP(
		recorder,
		authenticatedRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body)),
	)

	require.Equal(t, http.StatusCreated, recorder.Code)
	encoded, err := json.Marshal(received)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"prompt":{"text":"fix the bug"},
		"repos":[{"url":"https://github.com/acme/repo","startingRef":"main"}],
		"model":{"id":"composer-2"},
		"autoCreatePR":true
	}`, string(encoded))
}

func TestFollowupBoundaryMapping(t *testing.T) {
	var received map[string]any
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/agents/bc-1/runs", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{
			"run":{
				"id":"run-1","agentId":"bc-1","status":"CREATING",
				"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"
			}
		}`)
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	recorder := httptest.NewRecorder()
	p.router.ServeHTTP(
		recorder,
		authenticatedRequest(
			http.MethodPost,
			"/api/v1/agents/bc-1/followup",
			strings.NewReader(`{"prompt":"also update tests"}`),
		),
	)
	require.Equal(t, http.StatusCreated, recorder.Code)
	encoded, err := json.Marshal(received)
	require.NoError(t, err)
	assert.JSONEq(t, `{"prompt":{"text":"also update tests"}}`, string(encoded))
}

func TestProxyPassthroughAndErrorMirroring(t *testing.T) {
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"items":[{"id":"model-1"}]}`)
		case "/v1/agents/bc-1":
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":"rate_limit_exceeded","message":"slow down"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantBody   string
		wantRetry  string
	}{
		{
			name:       "successful body is unchanged",
			target:     "/api/v1/models",
			wantStatus: http.StatusOK,
			wantBody:   `{"items":[{"id":"model-1"}]}`,
		},
		{
			name:       "upstream error is unchanged",
			target:     "/api/v1/agents/bc-1",
			wantStatus: http.StatusTooManyRequests,
			wantBody:   `{"error":{"code":"rate_limit_exceeded","message":"slow down"}}`,
			wantRetry:  "10",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			p.router.ServeHTTP(
				recorder,
				authenticatedRequest(http.MethodGet, test.target, nil),
			)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantBody, recorder.Body.String())
			assert.Equal(t, test.wantRetry, recorder.Header().Get("Retry-After"))
		})
	}
}

func TestProxyRequiresConfiguredAPIKey(t *testing.T) {
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("upstream should not be called")
	}))
	recorder := httptest.NewRecorder()
	p.router.ServeHTTP(
		recorder,
		authenticatedRequest(http.MethodGet, "/api/v1/agents", nil),
	)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.JSONEq(t, `{
		"error":"Configure your Cursor API key in the Cursor plugin panel in Mattermost",
		"code":"api_key_not_configured"
	}`, recorder.Body.String())
}

func TestListAgentsHydratesAndCachesItems(t *testing.T) {
	var listCalls atomic.Int32
	var agentACalls atomic.Int32
	var runACalls atomic.Int32
	var agentBCalls atomic.Int32
	var runBCalls atomic.Int32

	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/agents":
			call := listCalls.Add(1)
			assert.Equal(t, "false", r.URL.Query().Get("includeArchived"))
			updatedAt := "2026-01-01T00:01:00Z"
			if call >= 3 {
				updatedAt = "2026-01-01T00:02:00Z"
			}
			_, _ = io.WriteString(w, `{
				"items":[
					{
						"id":"bc-a","name":"A","status":"ACTIVE","env":{"type":"cloud"},
						"url":"https://cursor.com/agents/bc-a",
						"createdAt":"2026-01-01T00:00:00Z",
						"updatedAt":"`+updatedAt+`","latestRunId":"run-a"
					},
					{
						"id":"bc-b","name":"B","status":"ACTIVE","env":{"type":"cloud"},
						"url":"https://cursor.com/agents/bc-b",
						"createdAt":"2026-01-01T00:00:00Z",
						"updatedAt":"2026-01-01T00:01:00Z","latestRunId":"run-b"
					}
				],
				"nextCursor":"next-page"
			}`)
		case "/v1/agents/bc-a":
			agentACalls.Add(1)
			_, _ = io.WriteString(w, `{
				"id":"bc-a","name":"A","status":"ACTIVE","env":{"type":"cloud"},
				"url":"https://cursor.com/agents/bc-a",
				"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:01:00Z",
				"latestRunId":"run-a","repos":[{"url":"https://github.com/acme/a","startingRef":"main"}],
				"workOnCurrentBranch":false,"autoCreatePR":true
			}`)
		case "/v1/agents/bc-a/runs/run-a":
			call := runACalls.Add(1)
			branch := "cursor/first"
			runStatus := "RUNNING"
			if call > 1 {
				branch = "cursor/refreshed"
				runStatus = "FINISHED"
			}
			_, _ = io.WriteString(w, `{
				"id":"run-a","agentId":"bc-a","status":"`+runStatus+`",
				"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:01:00Z",
				"git":{"branches":[{"repoUrl":"github.com/acme/a","branch":"`+branch+`","prUrl":"https://github.com/acme/a/pull/1"}]}
			}`)
		case "/v1/agents/bc-b":
			agentBCalls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":"upstream_error","message":"agent failed"}}`)
		case "/v1/agents/bc-b/runs/run-b":
			runBCalls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":"upstream_error","message":"run failed"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	list := func() cursorapi.HydratedListAgentsResponse {
		t.Helper()
		recorder := httptest.NewRecorder()
		p.router.ServeHTTP(
			recorder,
			authenticatedRequest(
				http.MethodGet,
				"/api/v1/agents?limit=20&includeArchived=false",
				nil,
			),
		)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response cursorapi.HydratedListAgentsResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		return response
	}

	first := list()
	require.Len(t, first.Items, 2)
	assert.Equal(t, "next-page", first.NextCursor)
	assert.Equal(t, "https://github.com/acme/a", first.Items[0].Repos[0].URL)
	assert.Equal(t, "cursor/first", first.Items[0].Branch)
	assert.Equal(t, "https://github.com/acme/a/pull/1", first.Items[0].PRURL)
	assert.Equal(t, "RUNNING", first.Items[0].RunStatus)
	assert.Empty(t, first.Items[1].Repos)
	assert.Empty(t, first.Items[1].Branch)
	assert.Empty(t, first.Items[1].RunStatus)

	second := list()
	assert.Equal(t, "cursor/first", second.Items[0].Branch)
	assert.Equal(t, "RUNNING", second.Items[0].RunStatus)
	assert.Equal(t, int32(1), agentACalls.Load())
	assert.Equal(t, int32(1), runACalls.Load())
	assert.Equal(t, int32(2), agentBCalls.Load())
	assert.Equal(t, int32(2), runBCalls.Load())

	third := list()
	assert.Equal(t, "cursor/refreshed", third.Items[0].Branch)
	assert.Equal(t, "FINISHED", third.Items[0].RunStatus)
	assert.Equal(t, int32(1), agentACalls.Load())
	assert.Equal(t, int32(2), runACalls.Load())
}

func TestMessagesProxyUsesLegacyConversationEndpoint(t *testing.T) {
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/agents/bc-1/conversation", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"bc-1",
			"messages":[{"id":"message-1","type":"assistant_message","text":"Done"}]
		}`)
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	recorder := httptest.NewRecorder()
	p.router.ServeHTTP(
		recorder,
		authenticatedRequest(http.MethodGet, "/api/v1/agents/bc-1/messages", nil),
	)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{
		"id":"bc-1",
		"messages":[{"id":"message-1","type":"assistant_message","text":"Done"}]
	}`, recorder.Body.String())
}

func TestSSEProxy(t *testing.T) {
	var lastEventID string
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastEventID = r.Header.Get("Last-Event-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Cursor-Stream-Retention-Seconds", "300")
		_, _ = io.WriteString(w, "id: event-2\nevent: assistant\ndata: {\"text\":\"hello\"}\n\n")
		w.(http.Flusher).Flush()
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	request := authenticatedRequest(
		http.MethodGet,
		"/api/v1/agents/bc-1/runs/run-1/stream",
		nil,
	)
	request.Header.Set("Last-Event-ID", "event-1")
	recorder := httptest.NewRecorder()
	p.router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "event-1", lastEventID)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "300", recorder.Header().Get("X-Cursor-Stream-Retention-Seconds"))
	assert.True(t, recorder.Flushed)
	assert.Equal(t, "id: event-2\nevent: assistant\ndata: {\"text\":\"hello\"}\n\n", recorder.Body.String())
}

func TestRepositoriesResponseIsCachedPerUser(t *testing.T) {
	var calls atomic.Int32
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/repositories", r.URL.Path)
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"url":"https://github.com/acme/repo"}]}`)
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	for range 2 {
		recorder := httptest.NewRecorder()
		p.router.ServeHTTP(
			recorder,
			authenticatedRequest(http.MethodGet, "/api/v1/repositories", nil),
		)
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `{"items":[{"url":"https://github.com/acme/repo"}]}`, recorder.Body.String())
	}
	assert.Equal(t, int32(1), calls.Load())
}

func TestNoContentProxyEndpoints(t *testing.T) {
	p, _, _ := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	require.NoError(t, p.keyStore.Set(testUserID, "key", ""))

	tests := []struct {
		method string
		target string
	}{
		{method: http.MethodDelete, target: "/api/v1/agents/bc-1"},
		{method: http.MethodPost, target: "/api/v1/agents/bc-1/archive"},
		{method: http.MethodPost, target: "/api/v1/agents/bc-1/unarchive"},
		{method: http.MethodPost, target: "/api/v1/agents/bc-1/runs/run-1/cancel"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			p.router.ServeHTTP(recorder, authenticatedRequest(test.method, test.target, bytes.NewReader(nil)))
			assert.Equal(t, http.StatusNoContent, recorder.Code)
			assert.Empty(t, recorder.Body.String())
		})
	}
}
