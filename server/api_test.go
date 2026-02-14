package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

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

	rr := doRequest(p, http.MethodGet, "/api/v1/agents", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentsListResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Len(t, resp.Agents, 2)
	assert.Equal(t, "agent-1", resp.Agents[0].ID)
	assert.Equal(t, "RUNNING", resp.Agents[0].Status)
	assert.Equal(t, "agent-2", resp.Agents[1].ID)
	assert.Equal(t, "https://github.com/org/repo2/pull/1", resp.Agents[1].PrURL)
}

func TestGetAgents_Empty(t *testing.T) {
	p, _, _, store := setupAPITestPlugin(t)

	store.On("GetAgentsByUser", "user-1").Return([]*kvstore.AgentRecord{}, nil)

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

	rr := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "user-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp AgentResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "agent-1", resp.ID)
	assert.Equal(t, "RUNNING", resp.Status)
	assert.Equal(t, "https://cursor.com/agents/agent-1", resp.CursorURL)
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
