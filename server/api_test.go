package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

func setupAPITestPlugin(t *testing.T) (*Plugin, *plugintest.API, *mockCursorClient, *mockKVStore) {
	t.Helper()

	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	// getUsername calls GetUser -- provide a default mock.
	api.On("GetUser", mock.AnythingOfType("string")).Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

	// sendEphemeralToActionUser calls GetPost -- provide a default mock.
	api.On("GetPost", mock.AnythingOfType("string")).Return(&model.Post{
		Id:     "post-1",
		RootId: "",
	}, nil).Maybe()

	cursorClient := &mockCursorClient{}
	store := &mockKVStore{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.cursorClient = cursorClient
	p.kvstore = store
	p.botUserID = "bot-user-id"
	p.botUsername = "cursor"
	p.configuration = &configuration{
		CursorAPIKey:      "test-key",
		DefaultRepository: "org/repo",
	}
	p.router = p.initRouter()

	return p, api, cursorClient, store
}

func doRequest(p *Plugin, method, path string, body any, userID string) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(data)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	if userID != "" {
		req.Header.Set("Mattermost-User-ID", userID)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	p.ServeHTTP(nil, rr, req)
	return rr
}

// --- Auth middleware tests ---

func TestAPI_Unauthorized(t *testing.T) {
	p, _, _, _ := setupAPITestPlugin(t)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// --- GET /api/v1/agents ---

func TestGetAgents_Success(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	records := []*kvstore.AgentRecord{
		{
			CursorAgentID: "agent-1",
			Status:        "RUNNING",
			Repository:    "org/repo",
			Branch:        "main",
			ChannelID:     "ch-1",
			PostID:        "post-1",
			UserID:        "user-1",
		},
		{
			CursorAgentID: "agent-2",
			Status:        "FINISHED",
			Repository:    "org/repo2",
			Branch:        "develop",
			ChannelID:     "ch-2",
			PostID:        "post-2",
			UserID:        "user-1",
			PrURL:         "https://github.com/org/repo2/pull/1",
		},
	}

	store.On("GetAgentsByUser", "user-1").Return(records, nil)
	store.On("GetWorkflowByAgent", mock.AnythingOfType("string")).Return("", nil)
	store.On("GetReviewLoopByAgent", mock.AnythingOfType("string")).Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Len(t, resp.Agents, 2)
	assert.Equal(t, "agent-1", resp.Agents[0].ID)
	assert.Equal(t, "RUNNING", resp.Agents[0].Status)
	assert.Equal(t, "post-1", resp.Agents[0].RootPostID)
	assert.Equal(t, "agent-2", resp.Agents[1].ID)
	assert.Equal(t, "https://github.com/org/repo2/pull/1", resp.Agents[1].PrURL)
	assert.Equal(t, "post-2", resp.Agents[1].RootPostID)
}

func TestGetAgents_Empty(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetAgentsByUser", "user-1").Return([]*kvstore.AgentRecord{}, nil)
	store.On("GetWorkflowByAgent", mock.AnythingOfType("string")).Return("", nil)
	store.On("GetReviewLoopByAgent", mock.AnythingOfType("string")).Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Len(t, resp.Agents, 0)
}

func TestGetAgents_StoreError(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetAgentsByUser", "user-1").Return(nil, fmt.Errorf("KV store error"))

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// --- GET /api/v1/agents/{id} ---

func TestGetAgent_Success(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		Repository:    "org/repo",
		Branch:        "main",
		ChannelID:     "ch-1",
		PostID:        "post-1",
		UserID:        "user-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusRunning,
	}, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "agent-1", resp.ID)
	assert.Equal(t, "RUNNING", resp.Status)
	assert.Equal(t, "https://cursor.com/agents/agent-1", resp.CursorURL)
	assert.Equal(t, "post-1", resp.RootPostID)
}

func TestGetAgent_NotFound(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetAgent", "agent-nonexistent").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-nonexistent", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetAgent_WrongUser(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		UserID:        "other-user",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetAgent_RefreshesStatusFromCursor(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		Repository:    "org/repo",
		UserID:        "user-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusFinished,
		Target: cursor.AgentTarget{PrURL: "https://github.com/org/repo/pull/99"},
	}, nil)
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.Status == "FINISHED" && r.PrURL == "https://github.com/org/repo/pull/99"
	})).Return(nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "FINISHED", resp.Status)
	assert.Equal(t, "https://github.com/org/repo/pull/99", resp.PrURL)
}

func TestGetAgent_TerminalStatus_SkipsRefresh(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		Repository:    "org/repo",
		UserID:        "user-1",
		PrURL:         "https://github.com/org/repo/pull/1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	// Cursor API should NOT be called for terminal agents.
	cursorClient.AssertNotCalled(t, "GetAgent")
}

// --- POST /api/v1/agents/{id}/followup ---

func TestAddFollowup_Success(t *testing.T) {
	p, api, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		UserID:        "user-1",
		ChannelID:     "ch-1",
		PostID:        "post-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("AddFollowup", mock.Anything, "agent-1", mock.MatchedBy(func(req cursor.FollowupRequest) bool {
		return req.Prompt.Text == "also fix the tests"
	})).Return(&cursor.FollowupResponse{ID: "agent-1"}, nil)
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "post-1" && p.UserId == "bot-user-id"
	})).Return(&model.Post{Id: "msg-1"}, nil)

	body := FollowupRequestBody{Message: "also fix the tests"}
	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/followup", body, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp StatusOKResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
}

func TestAddFollowup_EmptyMessage(t *testing.T) {
	p, _, _, _ := setupAPITestPlugin(t)

	body := FollowupRequestBody{Message: ""}
	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/followup", body, "user-1")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestAddFollowup_AgentNotRunning(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		UserID:        "user-1",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)

	body := FollowupRequestBody{Message: "fix more"}
	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/followup", body, "user-1")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestAddFollowup_AgentNotFound(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetAgent", "agent-1").Return(nil, nil)

	body := FollowupRequestBody{Message: "fix more"}
	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/followup", body, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestAddFollowup_CursorAPIError(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		UserID:        "user-1",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("AddFollowup", mock.Anything, "agent-1", mock.Anything).Return(nil, fmt.Errorf("API error"))

	body := FollowupRequestBody{Message: "fix more"}
	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/followup", body, "user-1")
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestAddFollowup_NoCursorClient(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)
	p.cursorClient = nil

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		UserID:        "user-1",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)

	body := FollowupRequestBody{Message: "fix more"}
	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/followup", body, "user-1")
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

// --- DELETE /api/v1/agents/{id} ---

func TestCancelAgent_Success(t *testing.T) {
	p, api, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		UserID:         "user-1",
		TriggerPostID:  "trigger-1",
		PostID:         "post-1",
		ChannelID:      "ch-1",
		BotReplyPostID: "bot-reply-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("StopAgent", mock.Anything, "agent-1").Return(&cursor.StopResponse{ID: "agent-1"}, nil)
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.Status == "STOPPED"
	})).Return(nil)

	// Reaction swaps
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "no_entry_sign"
	})).Return(nil, nil)

	// Cancel attachment posted in thread: grey color
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "post-1" &&
			p.UserId == "bot-user-id" &&
			hasAttachmentWithColor(p, "#8B8FA7")
	})).Return(&model.Post{Id: "msg-1"}, nil)

	// GetPost/UpdatePost for updateBotReplyWithAttachment
	api.On("GetPost", "bot-reply-1").Return(&model.Post{
		Id:     "bot-reply-1",
		UserId: "bot-user-id",
		Props:  model.StringInterface{},
	}, nil)
	api.On("UpdatePost", mock.Anything).Return(nil, nil)

	// Workflow cleanup (no associated workflow)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	// WebSocket event
	api.On("PublishWebSocketEvent", "agent_status_change", mock.Anything, mock.Anything).Return()

	rr := doRequest(p, http.MethodDelete, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp StatusOKResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)

	cursorClient.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestCancelAgent_AlreadyTerminal(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		UserID:        "user-1",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)

	rr := doRequest(p, http.MethodDelete, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCancelAgent_NotFound(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodDelete, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestCancelAgent_WrongUser(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		UserID:        "other-user",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)

	rr := doRequest(p, http.MethodDelete, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestCancelAgent_CursorAPIError(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		UserID:        "user-1",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("StopAgent", mock.Anything, "agent-1").Return(nil, fmt.Errorf("API timeout"))

	rr := doRequest(p, http.MethodDelete, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

// --- HITL response handler tests ---

func TestHandleHITLResponse_AcceptContext(t *testing.T) {
	p, api, cursorClient, store := setupAPITestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:              "wf-1",
		UserID:          "user-1",
		ChannelID:       "ch-1",
		RootPostID:      "root-1",
		TriggerPostID:   "trigger-1",
		ContextPostID:   "review-post-1",
		Phase:           kvstore.PhaseContextReview,
		Repository:      "org/repo",
		Branch:          "main",
		Model:           "auto",
		AutoCreatePR:    true,
		OriginalPrompt:  "fix the bug",
		EnrichedContext: "Enriched context text",
		SkipPlanLoop:    true,
	}

	store.On("GetWorkflow", "wf-1").Return(workflow, nil)
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// launchImplementerFromWorkflow mocks (called async via goroutine).
	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(&cursor.Agent{
		ID:     "agent-impl-1",
		Status: cursor.AgentStatusCreating,
	}, nil).Maybe()
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-1"}, nil).Maybe()
	store.On("SaveAgent", mock.Anything).Return(nil).Maybe()
	store.On("SetThreadAgent", "root-1", "agent-impl-1").Return(nil).Maybe()
	store.On("SetAgentWorkflow", "agent-impl-1", "wf-1").Return(nil).Maybe()
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	body := model.PostActionIntegrationRequest{
		UserId: "user-1",
		Context: map[string]any{
			"action":      "accept",
			"phase":       "context_review",
			"workflow_id": "wf-1",
		},
	}
	rr := doRequest(p, http.MethodPost, "/api/v1/actions/hitl-response", body, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp model.PostActionIntegrationResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	// Response should contain the accepted attachment via Update.
	assert.NotNil(t, resp.Update)
}

func TestHandleHITLResponse_RejectContext(t *testing.T) {
	p, api, _, store := setupAPITestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:            "wf-1",
		UserID:        "user-1",
		ContextPostID: "review-post-1",
		TriggerPostID: "trigger-1",
		Phase:         kvstore.PhaseContextReview,
	}

	store.On("GetWorkflow", "wf-1").Return(workflow, nil)
	store.On("SaveWorkflow", mock.MatchedBy(func(wf *kvstore.HITLWorkflow) bool {
		return wf.Phase == kvstore.PhaseRejected
	})).Return(nil).Maybe()

	// Reaction swaps (called async from rejectWorkflow goroutine).
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil).Maybe()
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "no_entry_sign"
	})).Return(nil, nil).Maybe()
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	body := model.PostActionIntegrationRequest{
		UserId: "user-1",
		Context: map[string]any{
			"action":      "reject",
			"phase":       "context_review",
			"workflow_id": "wf-1",
		},
	}
	rr := doRequest(p, http.MethodPost, "/api/v1/actions/hitl-response", body, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp model.PostActionIntegrationResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	// Response should contain the rejected attachment via Update.
	assert.NotNil(t, resp.Update)
}

func TestHandleHITLResponse_WrongUser(t *testing.T) {
	p, api, _, store := setupAPITestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:     "wf-1",
		UserID: "user-1",
		Phase:  kvstore.PhaseContextReview,
	}

	store.On("GetWorkflow", "wf-1").Return(workflow, nil)

	// sendEphemeralToActionUser sends an ephemeral post.
	api.On("SendEphemeralPost", "user-2", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "Only @testuser can approve or reject")
	})).Return(nil).Maybe()

	body := model.PostActionIntegrationRequest{
		UserId: "user-2",
		Context: map[string]any{
			"action":      "accept",
			"phase":       "context_review",
			"workflow_id": "wf-1",
		},
	}
	rr := doRequest(p, http.MethodPost, "/api/v1/actions/hitl-response", body, "user-2")
	assert.Equal(t, http.StatusOK, rr.Code)

	// Response should be a no-op (nil attachment, buttons unchanged for user).
	var resp model.PostActionIntegrationResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Nil(t, resp.Update)
}

func TestHandleHITLResponse_PhaseMismatch(t *testing.T) {
	p, api, _, store := setupAPITestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:         "wf-1",
		UserID:     "user-1",
		Phase:      kvstore.PhaseImplementing,
		Repository: "org/repo",
		Branch:     "main",
		Model:      "auto",
	}

	store.On("GetWorkflow", "wf-1").Return(workflow, nil)

	// sendEphemeralToActionUser called for already-resolved message.
	api.On("SendEphemeralPost", "user-1", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "already been resolved")
	})).Return(nil).Maybe()

	body := model.PostActionIntegrationRequest{
		UserId: "user-1",
		Context: map[string]any{
			"action":      "accept",
			"phase":       "context_review",
			"workflow_id": "wf-1",
		},
	}
	rr := doRequest(p, http.MethodPost, "/api/v1/actions/hitl-response", body, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	// Response should include an idempotent attachment showing the resolved state.
	var resp model.PostActionIntegrationResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.NotNil(t, resp.Update)
}

func TestHandleHITLResponse_WorkflowNotFound(t *testing.T) {
	p, api, _, store := setupAPITestPlugin(t)

	store.On("GetWorkflow", "wf-nonexistent").Return(nil, nil)

	// sendEphemeralToActionUser called with "no longer exists" message.
	api.On("SendEphemeralPost", "user-1", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "no longer exists")
	})).Return(nil).Maybe()

	body := model.PostActionIntegrationRequest{
		UserId: "user-1",
		Context: map[string]any{
			"action":      "accept",
			"phase":       "context_review",
			"workflow_id": "wf-nonexistent",
		},
	}
	rr := doRequest(p, http.MethodPost, "/api/v1/actions/hitl-response", body, "user-1")
	// Returns 200 with empty response (PostAction handlers always return 200).
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHandleHITLResponse_MissingContextFields(t *testing.T) {
	p, _, _, _ := setupAPITestPlugin(t)

	body := model.PostActionIntegrationRequest{
		Context: map[string]any{},
	}
	rr := doRequest(p, http.MethodPost, "/api/v1/actions/hitl-response", body, "user-1")
	// PostAction handlers always return 200 with an empty response on error.
	assert.Equal(t, http.StatusOK, rr.Code)
}

// --- GET /api/v1/workflows/{id} ---

func TestGetWorkflow_Success(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:                 "wf-1",
		UserID:             "user-1",
		ChannelID:          "ch-1",
		RootPostID:         "root-1",
		Phase:              kvstore.PhasePlanReview,
		Repository:         "org/repo",
		Branch:             "main",
		Model:              "auto",
		OriginalPrompt:     "fix the bug",
		EnrichedContext:    "Enriched context",
		ApprovedContext:    "Approved context",
		PlannerAgentID:     "planner-1",
		RetrievedPlan:      "### Plan\nDo stuff.",
		ApprovedPlan:       "",
		PlanIterationCount: 2,
		ImplementerAgentID: "",
		SkipContextReview:  false,
		SkipPlanLoop:       false,
		CreatedAt:          1000,
		UpdatedAt:          2000,
	}

	store.On("GetWorkflow", "wf-1").Return(workflow, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/workflows/wf-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp WorkflowResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "wf-1", resp.ID)
	assert.Equal(t, "user-1", resp.UserID)
	assert.Equal(t, "plan_review", resp.Phase)
	assert.Equal(t, "org/repo", resp.Repository)
	assert.Equal(t, "main", resp.Branch)
	assert.Equal(t, "auto", resp.Model)
	assert.Equal(t, "fix the bug", resp.OriginalPrompt)
	assert.Equal(t, "Enriched context", resp.EnrichedContext)
	assert.Equal(t, "Approved context", resp.ApprovedContext)
	assert.Equal(t, "planner-1", resp.PlannerAgentID)
	assert.Equal(t, "### Plan\nDo stuff.", resp.RetrievedPlan)
	assert.Equal(t, 2, resp.PlanIterationCount)
	assert.False(t, resp.SkipContextReview)
	assert.False(t, resp.SkipPlanLoop)
	assert.Equal(t, int64(1000), resp.CreatedAt)
	assert.Equal(t, int64(2000), resp.UpdatedAt)
}

func TestGetWorkflow_NotFound(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetWorkflow", "wf-nonexistent").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/workflows/wf-nonexistent", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetWorkflow_WrongUser(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:     "wf-1",
		UserID: "other-user",
		Phase:  kvstore.PhaseContextReview,
	}

	store.On("GetWorkflow", "wf-1").Return(workflow, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/workflows/wf-1", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetWorkflow_StoreError(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetWorkflow", "wf-error").Return(nil, fmt.Errorf("KV store error"))

	rr := doRequest(p, http.MethodGet, "/api/v1/workflows/wf-error", nil, "user-1")
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// --- GET /api/v1/agents -- workflow field inclusion ---

func TestGetAgents_IncludesWorkflowFields(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	records := []*kvstore.AgentRecord{
		{
			CursorAgentID: "agent-1",
			Status:        "RUNNING",
			Repository:    "org/repo",
			Branch:        "main",
			ChannelID:     "ch-1",
			PostID:        "post-1",
			UserID:        "user-1",
		},
	}

	store.On("GetAgentsByUser", "user-1").Return(records, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("wf-1", nil)
	store.On("GetWorkflow", "wf-1").Return(&kvstore.HITLWorkflow{
		ID:                 "wf-1",
		Phase:              kvstore.PhasePlanning,
		PlanIterationCount: 3,
	}, nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Agents, 1)
	assert.Equal(t, "wf-1", resp.Agents[0].WorkflowID)
	assert.Equal(t, "planning", resp.Agents[0].WorkflowPhase)
	assert.Equal(t, 3, resp.Agents[0].PlanIterationCount)
}

func TestGetAgents_NoWorkflowFields_WhenNoWorkflow(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	records := []*kvstore.AgentRecord{
		{
			CursorAgentID: "agent-1",
			Status:        "RUNNING",
			Repository:    "org/repo",
			Branch:        "main",
			ChannelID:     "ch-1",
			PostID:        "post-1",
			UserID:        "user-1",
		},
	}

	store.On("GetAgentsByUser", "user-1").Return(records, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Agents, 1)
	assert.Empty(t, resp.Agents[0].WorkflowID)
	assert.Empty(t, resp.Agents[0].WorkflowPhase)
	assert.Equal(t, 0, resp.Agents[0].PlanIterationCount)
}

// --- GET /api/v1/agents/{id} -- workflow field inclusion ---

func TestGetAgent_IncludesWorkflowFields(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		Repository:    "org/repo",
		Branch:        "main",
		ChannelID:     "ch-1",
		PostID:        "post-1",
		UserID:        "user-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	// Terminal status: no Cursor API refresh.
	_ = cursorClient

	store.On("GetWorkflowByAgent", "agent-1").Return("wf-1", nil)
	store.On("GetWorkflow", "wf-1").Return(&kvstore.HITLWorkflow{
		ID:                 "wf-1",
		Phase:              kvstore.PhaseImplementing,
		PlanIterationCount: 1,
	}, nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "wf-1", resp.WorkflowID)
	assert.Equal(t, "implementing", resp.WorkflowPhase)
	assert.Equal(t, 1, resp.PlanIterationCount)
}

// --- POST /api/v1/agents/{id}/archive ---

func TestArchiveAgent_Success(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		UserID:        "user-1",
		Repository:    "org/repo",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.Archived && r.Status == "FINISHED"
	})).Return(nil)

	// Workflow cleanup (no associated workflow)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/archive", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp StatusOKResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
}

func TestArchiveAgent_StopsRunningAgent(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		UserID:        "user-1",
		Repository:    "org/repo",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("StopAgent", mock.Anything, "agent-1").Return(&cursor.StopResponse{ID: "agent-1"}, nil)
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.Archived && r.Status == "STOPPED"
	})).Return(nil)

	// Workflow cleanup (no associated workflow)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/archive", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	cursorClient.AssertCalled(t, "StopAgent", mock.Anything, "agent-1")
}

func TestArchiveAgent_StopsRunningAgent_AlreadyDeletedInCloud(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
	}{
		{"409 agent deleted", 409, "Agent is deleted"},
		{"400 agent not running", 400, "Cloud Agent not running.: This Cloud Agent is no longer available."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, cursorClient, store := setupAPITestPlugin(t)

			record := &kvstore.AgentRecord{
				CursorAgentID: "agent-1",
				Status:        "RUNNING",
				UserID:        "user-1",
				Repository:    "org/repo",
			}

			store.On("GetAgent", "agent-1").Return(record, nil)
			cursorClient.On("StopAgent", mock.Anything, "agent-1").Return(nil, &cursor.APIError{StatusCode: tt.statusCode, Message: tt.message})
			store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
				return r.Archived && r.Status == "STOPPED"
			})).Return(nil)
			store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

			rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/archive", nil, "user-1")
			assert.Equal(t, http.StatusOK, rr.Code)
		})
	}
}

func TestArchiveAgent_NotFound(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/archive", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestArchiveAgent_WrongUser(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		UserID:        "other-user",
	}
	store.On("GetAgent", "agent-1").Return(record, nil)

	rr := doRequest(p, http.MethodPost, "/api/v1/agents/agent-1/archive", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// --- GET /api/v1/agents -- archived filter ---

func TestGetAgents_FiltersOutArchived(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	records := []*kvstore.AgentRecord{
		{
			CursorAgentID: "agent-1",
			Status:        "RUNNING",
			Repository:    "org/repo",
			UserID:        "user-1",
		},
		{
			CursorAgentID: "agent-2",
			Status:        "FINISHED",
			Repository:    "org/repo",
			UserID:        "user-1",
			Archived:      true,
		},
	}

	store.On("GetAgentsByUser", "user-1").Return(records, nil)
	store.On("GetWorkflowByAgent", mock.AnythingOfType("string")).Return("", nil)
	store.On("GetReviewLoopByAgent", mock.AnythingOfType("string")).Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Len(t, resp.Agents, 1)
	assert.Equal(t, "agent-1", resp.Agents[0].ID)
}

func TestGetAgents_ReturnsOnlyArchived(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	records := []*kvstore.AgentRecord{
		{
			CursorAgentID: "agent-1",
			Status:        "RUNNING",
			Repository:    "org/repo",
			UserID:        "user-1",
		},
		{
			CursorAgentID: "agent-2",
			Status:        "FINISHED",
			Repository:    "org/repo",
			UserID:        "user-1",
			Archived:      true,
		},
	}

	store.On("GetAgentsByUser", "user-1").Return(records, nil)
	store.On("GetWorkflowByAgent", mock.AnythingOfType("string")).Return("", nil)
	store.On("GetReviewLoopByAgent", mock.AnythingOfType("string")).Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents?archived=true", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Len(t, resp.Agents, 1)
	assert.Equal(t, "agent-2", resp.Agents[0].ID)
	assert.True(t, resp.Agents[0].Archived)
}

// --- GET /api/v1/review-loops/{id} ---

func TestGetReviewLoop_Success(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		WorkflowID:    "wf-1",
		UserID:        "user-1",
		ChannelID:     "ch-1",
		RootPostID:    "root-1",
		TriggerPostID: "trigger-1",
		PRURL:         "https://github.com/org/repo/pull/42",
		PRNumber:      42,
		Owner:         "org",
		Repo:          "repo",
		Repository:    "org/repo",
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     2,
		LastCommitSHA: "abc123",
		History: []kvstore.ReviewLoopEvent{
			{Phase: kvstore.ReviewPhaseRequestingReview, Timestamp: 1000, Detail: "Review requested"},
			{Phase: kvstore.ReviewPhaseAwaitingReview, Timestamp: 2000},
		},
		CreatedAt: 1000,
		UpdatedAt: 2000,
	}

	store.On("GetReviewLoop", "loop-1").Return(loop, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/review-loops/loop-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp ReviewLoopResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "loop-1", resp.ID)
	assert.Equal(t, "agent-1", resp.AgentRecordID)
	assert.Equal(t, "wf-1", resp.WorkflowID)
	assert.Equal(t, "user-1", resp.UserID)
	assert.Equal(t, kvstore.ReviewPhaseAwaitingReview, resp.Phase)
	assert.Equal(t, 2, resp.Iteration)
	assert.Equal(t, "abc123", resp.LastCommitSHA)
	assert.Len(t, resp.History, 2)
	assert.Equal(t, kvstore.ReviewPhaseRequestingReview, resp.History[0].Phase)
	assert.Equal(t, "Review requested", resp.History[0].Detail)
	assert.Equal(t, int64(1000), resp.History[0].Timestamp)
}

func TestGetReviewLoop_NotFound(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetReviewLoop", "loop-nonexistent").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/review-loops/loop-nonexistent", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetReviewLoop_WrongUser(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:     "loop-1",
		UserID: "other-user",
		Phase:  kvstore.ReviewPhaseAwaitingReview,
	}

	store.On("GetReviewLoop", "loop-1").Return(loop, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/review-loops/loop-1", nil, "user-1")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// --- GET /api/v1/agents -- review loop field inclusion ---

func TestGetAgents_IncludesReviewLoopFields(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	records := []*kvstore.AgentRecord{
		{
			CursorAgentID: "agent-1",
			Status:        "FINISHED",
			Repository:    "org/repo",
			Branch:        "main",
			ChannelID:     "ch-1",
			PostID:        "post-1",
			UserID:        "user-1",
		},
	}

	store.On("GetAgentsByUser", "user-1").Return(records, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(&kvstore.ReviewLoop{
		ID:        "loop-1",
		Phase:     kvstore.ReviewPhaseHumanReview,
		Iteration: 3,
	}, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Agents, 1)
	assert.Equal(t, "loop-1", resp.Agents[0].ReviewLoopID)
	assert.Equal(t, kvstore.ReviewPhaseHumanReview, resp.Agents[0].ReviewLoopPhase)
	assert.Equal(t, 3, resp.Agents[0].ReviewLoopIteration)
}

// --- GET /api/v1/agents/{id} -- GitHub freshness check ---

func TestHandleGetAgent_BackfillsPrURL(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	ghMock := &mockGitHubClient{}
	p.githubClient = ghMock
	// Disable review loop so the test focuses on PrURL backfill.
	p.configuration = &configuration{
		CursorAPIKey:       "test-key",
		DefaultRepository:  "org/repo",
		EnableAIReviewLoop: false,
	}

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		Repository:    "org/repo",
		Branch:        "main",
		TargetBranch:  "cursor/fix-login",
		UserID:        "user-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)

	// GitHub returns a matching PR.
	ghMock.On("GetPullRequestByBranch", mock.Anything, "org", "repo", "cursor/fix-login").Return(
		&github.PullRequest{
			HTMLURL: github.Ptr("https://github.com/org/repo/pull/42"),
		}, nil,
	)

	// SaveAgent called with backfilled PrURL.
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.PrURL == "https://github.com/org/repo/pull/42"
	})).Return(nil)

	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "https://github.com/org/repo/pull/42", resp.PrURL)

	// Verify SaveAgent was called.
	store.AssertCalled(t, "SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.PrURL == "https://github.com/org/repo/pull/42"
	}))
}

func TestHandleGetAgent_NoGitHubClient(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)
	// Ensure no GitHub client is set.
	p.githubClient = nil

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		Repository:    "org/repo",
		Branch:        "main",
		TargetBranch:  "cursor/fix-login",
		UserID:        "user-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)
	_ = cursorClient // Terminal status, no Cursor API refresh.

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	// PrURL should still be empty -- no GitHub client to query.
	assert.Empty(t, resp.PrURL)
}

func TestHandleGetAgent_NoPROnGitHub(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	ghMock := &mockGitHubClient{}
	p.githubClient = ghMock

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		Repository:    "org/repo",
		Branch:        "main",
		TargetBranch:  "cursor/fix-login",
		UserID:        "user-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)

	// GitHub returns no matching PR.
	ghMock.On("GetPullRequestByBranch", mock.Anything, "org", "repo", "cursor/fix-login").Return(
		nil, nil,
	)

	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(nil, nil)
	_ = cursorClient // Terminal status, no Cursor API refresh.

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	// PrURL should still be empty -- no PR found on GitHub.
	assert.Empty(t, resp.PrURL)

	// SaveAgent should NOT have been called (no change).
	store.AssertNotCalled(t, "SaveAgent", mock.Anything)
}

func TestHandleGetAgent_FreshParam(t *testing.T) {
	p, _, cursorClient, store := setupAPITestPlugin(t)

	ghMock := &mockGitHubClient{}
	p.githubClient = ghMock
	p.configuration = &configuration{
		CursorAPIKey:       "test-key",
		DefaultRepository:  "org/repo",
		EnableAIReviewLoop: true,
	}

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
		Repository:    "org/repo",
		Branch:        "main",
		PrURL:         "https://github.com/org/repo/pull/42",
		UserID:        "user-1",
	}

	store.On("GetAgent", "agent-1").Return(record, nil)
	_ = cursorClient // Terminal status, no Cursor API refresh.

	// ensureReviewLoop should be called due to ?fresh=true.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(&kvstore.ReviewLoop{
		ID:    "loop-1",
		Phase: kvstore.ReviewPhaseAwaitingReview,
	}, nil)

	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("GetReviewLoopByAgent", "agent-1").Return(&kvstore.ReviewLoop{
		ID:        "loop-1",
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 1,
	}, nil)

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1?fresh=true", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "https://github.com/org/repo/pull/42", resp.PrURL)
	assert.Equal(t, "loop-1", resp.ReviewLoopID)

	// ensureReviewLoop was called (via GetReviewLoopByPRURL).
	store.AssertCalled(t, "GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42")
}
