package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursorapi"
)

func TestMCPToolHandlersHappyPath(t *testing.T) {
	p := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer "+testServiceAccountAPIKey, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{
				"agent":{"id":"bc-created","name":"Created","status":"ACTIVE","url":"https://cursor.com/agents/bc-created"},
				"run":{"id":"run-created","status":"CREATING"}
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents/bc-get":
			_, _ = io.WriteString(w, `{
				"id":"bc-get","name":"Get agent","status":"ACTIVE",
				"env":{"type":"cloud"},"url":"https://cursor.com/agents/bc-get",
				"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:01:00Z",
				"latestRunId":"run-latest","repos":[{"url":"https://github.com/acme/repo"}],
				"workOnCurrentBranch":false,"autoCreatePR":true
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents/bc-get/runs/run-latest":
			_, _ = io.WriteString(w, `{
				"id":"run-latest","status":"FINISHED","result":"Implemented the change",
				"git":{"branches":[{"branch":"cursor/change","prUrl":"https://github.com/acme/repo/pull/1"}]}
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents":
			_, _ = io.WriteString(w, `{"items":[{
				"id":"bc-list","name":"Listed","status":"ACTIVE",
				"env":{"type":"cloud"},"url":"https://cursor.com/agents/bc-list",
				"createdAt":"2026-01-02T03:04:05Z","updatedAt":"2026-01-02T03:05:05Z"
			}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents/bc-list":
			_, _ = io.WriteString(w, `{
				"id":"bc-list","name":"Listed","status":"ACTIVE",
				"env":{"type":"cloud"},"url":"https://cursor.com/agents/bc-list",
				"createdAt":"2026-01-02T03:04:05Z","updatedAt":"2026-01-02T03:05:05Z",
				"repos":[{"url":"https://github.com/acme/repo"}],
				"workOnCurrentBranch":false,"autoCreatePR":false
			}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents/bc-follow/runs":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"run":{"id":"run-follow","status":"CREATING"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v0/agents/bc-conversation/conversation":
			_, _ = io.WriteString(w, `{"id":"bc-conversation","messages":[
				{"type":"user_message","text":"Please fix it"},
				{"type":"assistant_message","text":"Done"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "create_agent",
			run: func(t *testing.T) {
				result, output, err := p.mcpCreateAgent(ctx, nil, createAgentInput{
					Prompt:     "make a change",
					Repository: "https://github.com/acme/repo",
				})
				require.NoError(t, err)
				assert.Nil(t, result)
				assert.Equal(t, createAgentOutput{
					AgentID: "bc-created",
					Name:    "Created",
					Status:  "CREATING",
					URL:     "https://cursor.com/agents/bc-created",
				}, output)
			},
		},
		{
			name: "get_agent",
			run: func(t *testing.T) {
				result, output, err := p.mcpGetAgent(ctx, nil, getAgentInput{AgentID: "bc-get"})
				require.NoError(t, err)
				assert.Nil(t, result)
				assert.Equal(t, getAgentOutput{
					AgentID: "bc-get",
					Name:    "Get agent",
					Status:  "FINISHED",
					Summary: "Implemented the change",
					Branch:  "cursor/change",
					PRURL:   "https://github.com/acme/repo/pull/1",
					URL:     "https://cursor.com/agents/bc-get",
				}, output)
			},
		},
		{
			name: "list_agents",
			run: func(t *testing.T) {
				limit := 5
				result, output, err := p.mcpListAgents(ctx, nil, listAgentsInput{Limit: &limit})
				require.NoError(t, err)
				assert.Nil(t, result)
				require.Len(t, output.Agents, 1)
				assert.Equal(t, listedAgent{
					AgentID:    "bc-list",
					Name:       "Listed",
					Status:     "ACTIVE",
					Repository: "https://github.com/acme/repo",
					CreatedAt:  "2026-01-02T03:04:05Z",
				}, output.Agents[0])
			},
		},
		{
			name: "add_followup",
			run: func(t *testing.T) {
				result, output, err := p.mcpAddFollowup(ctx, nil, addFollowupInput{
					AgentID: "bc-follow",
					Prompt:  "also update tests",
				})
				require.NoError(t, err)
				assert.Nil(t, result)
				assert.Equal(t, addFollowupOutput{RunID: "run-follow", Status: "CREATING"}, output)
			},
		},
		{
			name: "get_agent_conversation",
			run: func(t *testing.T) {
				result, output, err := p.mcpGetAgentConversation(
					ctx,
					nil,
					getAgentConversationInput{AgentID: "bc-conversation"},
				)
				require.NoError(t, err)
				assert.Nil(t, result)
				assert.Equal(t, getAgentConversationOutput{
					Messages: []conversationMessage{
						{Type: "user_message", Text: "Please fix it"},
						{Type: "assistant_message", Text: "Done"},
					},
				}, output)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func TestMCPToolHandlersMissingKey(t *testing.T) {
	p := newTestPlugin(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("upstream should not be called without the service account API key")
	}))
	config := *p.getConfiguration()
	config.ServiceAccountAPIKey = ""
	p.setConfiguration(&config)
	ctx := context.Background()

	tests := []struct {
		name string
		call func() *mcp.CallToolResult
	}{
		{
			name: "create_agent",
			call: func() *mcp.CallToolResult {
				result, _, _ := p.mcpCreateAgent(ctx, nil, createAgentInput{})
				return result
			},
		},
		{
			name: "get_agent",
			call: func() *mcp.CallToolResult {
				result, _, _ := p.mcpGetAgent(ctx, nil, getAgentInput{})
				return result
			},
		},
		{
			name: "list_agents",
			call: func() *mcp.CallToolResult {
				result, _, _ := p.mcpListAgents(ctx, nil, listAgentsInput{})
				return result
			},
		},
		{
			name: "add_followup",
			call: func() *mcp.CallToolResult {
				result, _, _ := p.mcpAddFollowup(ctx, nil, addFollowupInput{})
				return result
			},
		},
		{
			name: "get_agent_conversation",
			call: func() *mcp.CallToolResult {
				result, _, _ := p.mcpGetAgentConversation(ctx, nil, getAgentConversationInput{})
				return result
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.call()
			require.NotNil(t, result)
			assert.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			assert.Contains(
				t,
				text.Text,
				"A Mattermost administrator must configure the Cursor Service Account API Key",
			)
		})
	}
}

func TestMCPUsesRotatedServiceAccountKey(t *testing.T) {
	const rotatedKey = "rotated-service-account-key"
	p := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer "+rotatedKey, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))

	_, initialIdentity, failure := p.mcpClient()
	require.Nil(t, failure)
	config := *p.getConfiguration()
	config.ServiceAccountAPIKey = rotatedKey
	p.setConfiguration(&config)

	_, rotatedIdentity, failure := p.mcpClient()
	require.Nil(t, failure)
	assert.NotEqual(t, initialIdentity, rotatedIdentity)

	result, output, err := p.mcpListAgents(context.Background(), nil, listAgentsInput{})
	require.NoError(t, err)
	assert.Nil(t, result)
	assert.Empty(t, output.Agents)
}

func TestMCPGetAgentFallsBackToAgentStatus(t *testing.T) {
	p := newTestPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/agents/bc-idle", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"bc-idle","name":"Idle","status":"ACTIVE","env":{"type":"cloud"},
			"url":"https://cursor.com/agents/bc-idle",
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z",
			"repos":[],"workOnCurrentBranch":false,"autoCreatePR":false
		}`)
	}))

	result, output, err := p.mcpGetAgent(
		context.Background(),
		nil,
		getAgentInput{AgentID: "bc-idle"},
	)
	require.NoError(t, err)
	assert.Nil(t, result)
	assert.Equal(t, "ACTIVE", output.Status)
}

func TestConversationTruncationKeepsNewestContent(t *testing.T) {
	messages := []cursorapi.Message{
		{Type: "old", Text: strings.Repeat("a", 10)},
		{Type: "new", Text: strings.Repeat("b", 10)},
	}
	output := truncateMessages(messages, 12)
	require.True(t, output.Truncated)
	require.Len(t, output.Messages, 2)
	assert.Equal(t, "aa", output.Messages[0].Text)
	assert.Equal(t, strings.Repeat("b", 10), output.Messages[1].Text)

	output = truncateMessages([]cursorapi.Message{{Type: "new", Text: "Aé日"}}, 4)
	require.Len(t, output.Messages, 1)
	assert.Equal(t, "日", output.Messages[0].Text)
	assert.True(t, utf8.ValidString(output.Messages[0].Text))
}
