package cursor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchAgent(t *testing.T) {
	expected := Agent{
		ID:     "agent-123",
		Name:   "test-agent",
		Status: AgentStatusCreating,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v0/agents", r.URL.Path)
		assert.Contains(t, r.Header.Get("Authorization"), "Basic")
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req LaunchAgentRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "Fix the login bug", req.Prompt.Text)
		assert.Equal(t, "https://github.com/org/repo", req.Source.Repository)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	agent, err := client.LaunchAgent(context.Background(), LaunchAgentRequest{
		Prompt: Prompt{Text: "Fix the login bug"},
		Source: Source{Repository: "https://github.com/org/repo", Ref: "main"},
		Target: &Target{AutoCreatePr: true},
	})

	require.NoError(t, err)
	assert.Equal(t, expected.ID, agent.ID)
	assert.Equal(t, AgentStatusCreating, agent.Status)
}

func TestGetAgent(t *testing.T) {
	expected := Agent{
		ID:     "agent-123",
		Status: AgentStatusRunning,
		Target: AgentTarget{PrURL: "https://github.com/org/repo/pull/42"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v0/agents/agent-123", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	agent, err := client.GetAgent(context.Background(), "agent-123")

	require.NoError(t, err)
	assert.Equal(t, AgentStatusRunning, agent.Status)
	assert.Equal(t, "https://github.com/org/repo/pull/42", agent.Target.PrURL)
}

func TestListAgents(t *testing.T) {
	expected := ListAgentsResponse{
		Agents:     []Agent{{ID: "a1"}, {ID: "a2"}},
		NextCursor: "cursor-abc",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "20", r.URL.Query().Get("limit"))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	resp, err := client.ListAgents(context.Background(), 20, "")

	require.NoError(t, err)
	assert.Len(t, resp.Agents, 2)
	assert.Equal(t, "cursor-abc", resp.NextCursor)
}

func TestListAgentsWithCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "page2", r.URL.Query().Get("cursor"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ListAgentsResponse{Agents: []Agent{{ID: "a3"}}})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	resp, err := client.ListAgents(context.Background(), 10, "page2")

	require.NoError(t, err)
	assert.Len(t, resp.Agents, 1)
	assert.Equal(t, "a3", resp.Agents[0].ID)
}

func TestAddFollowup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v0/agents/agent-123/followup", r.URL.Path)

		var req FollowupRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "Also fix the tests", req.Prompt.Text)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(FollowupResponse{ID: "agent-123"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	resp, err := client.AddFollowup(context.Background(), "agent-123", FollowupRequest{
		Prompt: Prompt{Text: "Also fix the tests"},
	})

	require.NoError(t, err)
	assert.Equal(t, "agent-123", resp.ID)
}

func TestGetConversation(t *testing.T) {
	expected := Conversation{
		ID: "conv-1",
		Messages: []Message{
			{ID: "m1", Type: "user_message", Text: "Fix the bug"},
			{ID: "m2", Type: "assistant_message", Text: "Working on it..."},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/agents/agent-123/conversation", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	conv, err := client.GetConversation(context.Background(), "agent-123")

	require.NoError(t, err)
	assert.Len(t, conv.Messages, 2)
	assert.Equal(t, "user_message", conv.Messages[0].Type)
}

func TestStopAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v0/agents/agent-123/stop", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(StopResponse{ID: "agent-123"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	resp, err := client.StopAgent(context.Background(), "agent-123")

	require.NoError(t, err)
	assert.Equal(t, "agent-123", resp.ID)
}

func TestDeleteAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v0/agents/agent-123", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(DeleteResponse{ID: "agent-123"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	resp, err := client.DeleteAgent(context.Background(), "agent-123")

	require.NoError(t, err)
	assert.Equal(t, "agent-123", resp.ID)
}

func TestListModels(t *testing.T) {
	expected := ListModelsResponse{Models: []string{"auto", "claude-sonnet", "gpt-4o"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/models", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	resp, err := client.ListModels(context.Background())

	require.NoError(t, err)
	assert.Contains(t, resp.Models, "auto")
}

func TestGetMe(t *testing.T) {
	expected := APIKeyInfo{
		APIKeyName: "my-key",
		UserEmail:  "user@example.com",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/me", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-api-key", server.URL)
	info, err := client.GetMe(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "user@example.com", info.UserEmail)
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Invalid API key"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("bad-key", server.URL)
	_, err := client.GetMe(context.Background())

	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 401, apiErr.StatusCode)
	assert.Equal(t, "Invalid API key", apiErr.Message)
}

func TestAPIErrorNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Agent not found"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-key", server.URL)
	_, err := client.GetAgent(context.Background(), "nonexistent")

	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 404, apiErr.StatusCode)
	assert.Equal(t, "Agent not found", apiErr.Message)
}

func TestAPIErrorForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"message": "Forbidden"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-key", server.URL)
	_, err := client.GetAgent(context.Background(), "agent-123")

	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 403, apiErr.StatusCode)
}

func TestNoRetryOn400(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Bad request"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-key", server.URL)
	_, err := client.GetMe(context.Background())

	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "should not retry on 400")
}

func TestRetryOn429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"message":"rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ListModelsResponse{Models: []string{"auto"}})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-key", server.URL)
	resp, err := client.ListModels(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts)) // 2 retries + 1 success
	assert.Contains(t, resp.Models, "auto")
}

func TestRetryOn500(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"internal error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(APIKeyInfo{UserEmail: "test@example.com"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-key", server.URL)
	info, err := client.GetMe(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
	assert.Equal(t, "test@example.com", info.UserEmail)
}

func TestRetryExhausted(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"rate limited"}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-key", server.URL)
	_, err := client.GetMe(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed after")
	// Initial attempt + maxRetries = 4 attempts total
	assert.Equal(t, int32(4), atomic.LoadInt32(&attempts))
}

func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block forever -- context should cancel first
		select {}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := NewClientWithBaseURL("test-key", server.URL)
	_, err := client.GetMe(ctx)

	require.Error(t, err)
}

func TestAgentStatusIsTerminal(t *testing.T) {
	assert.True(t, AgentStatusFinished.IsTerminal())
	assert.True(t, AgentStatusFailed.IsTerminal())
	assert.True(t, AgentStatusStopped.IsTerminal())
	assert.False(t, AgentStatusCreating.IsTerminal())
	assert.False(t, AgentStatusRunning.IsTerminal())
}

func TestBasicAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		assert.True(t, ok, "should have basic auth")
		assert.Equal(t, "my-api-key", username)
		assert.Equal(t, "", password)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(APIKeyInfo{UserEmail: "test@example.com"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("my-api-key", server.URL)
	_, err := client.GetMe(context.Background())
	require.NoError(t, err)
}

func TestAPIErrorMessage(t *testing.T) {
	apiErr := &APIError{StatusCode: 401, Message: "Invalid API key"}
	assert.Equal(t, "cursor API error (HTTP 401): Invalid API key", apiErr.Error())
}

func TestNonJSONErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("plain text error"))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-key", server.URL)
	_, err := client.GetMe(context.Background())

	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Equal(t, "plain text error", apiErr.Message)
}
