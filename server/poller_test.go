package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/mock"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

func setupPollerPlugin(t *testing.T) (*Plugin, *plugintest.API, *mockCursorClient, *mockKVStore) {
	t.Helper()

	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Phase 4: WebSocket events and post prop updates for all status changes.
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	api.On("GetPostThread", mock.Anything).Return(&model.PostList{Posts: map[string]*model.Post{}}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(nil, nil).Maybe()

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

	return p, api, cursorClient, store
}

func TestPoller_NoActiveAgents(t *testing.T) {
	p, _, cursorClient, store := setupPollerPlugin(t)

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{}, nil)

	p.pollAgentStatuses()

	cursorClient.AssertNotCalled(t, "GetAgent")
}

func TestPoller_StatusUnchanged(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		TriggerPostID: "post-1",
		PostID:        "post-1",
		ChannelID:     "ch-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusRunning,
	}, nil)

	p.pollAgentStatuses()

	// No reactions changed, no posts created, no store updates.
	api.AssertNotCalled(t, "AddReaction")
	api.AssertNotCalled(t, "RemoveReaction")
	api.AssertNotCalled(t, "CreatePost")
	store.AssertNotCalled(t, "SaveAgent")
}

func TestPoller_CreatingToRunning(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "CREATING",
		TriggerPostID: "post-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusRunning,
	}, nil)

	// Running message posted in thread
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" && p.Message == ":gear: Agent is now running..."
	})).Return(&model.Post{Id: "msg-1"}, nil)

	// Status updated in store
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-1" && r.Status == "RUNNING"
	})).Return(nil)

	p.pollAgentStatuses()

	api.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestPoller_RunningToFinished(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		TriggerPostID: "trigger-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:      "agent-1",
		Status:  cursor.AgentStatusFinished,
		Summary: "Fixed the login bug",
		Target:  cursor.AgentTarget{PrURL: "https://github.com/org/repo/pull/42"},
	}, nil)

	// Remove hourglass
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)

	// Add checkmark
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "white_check_mark"
	})).Return(nil, nil)

	// Completion message with summary and PR link
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" &&
			p.UserId == "bot-user-id" &&
			containsAll(p.Message, "Agent finished", "Fixed the login bug", "View Pull Request", "pull/42")
	})).Return(&model.Post{Id: "msg-1"}, nil)

	// Status updated
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.Status == "FINISHED" && r.PrURL == "https://github.com/org/repo/pull/42"
	})).Return(nil)

	p.pollAgentStatuses()

	api.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestPoller_RunningToFailed(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		TriggerPostID: "trigger-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:      "agent-1",
		Status:  cursor.AgentStatusFailed,
		Summary: "Authentication error",
	}, nil)

	// Remove hourglass
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)

	// Add X
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "x"
	})).Return(nil, nil)

	// Failure message
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" &&
			containsAll(p.Message, "Agent failed", "Authentication error")
	})).Return(&model.Post{Id: "msg-1"}, nil)

	// Status updated
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.Status == "FAILED"
	})).Return(nil)

	p.pollAgentStatuses()

	api.AssertExpectations(t)
}

func TestPoller_RunningToStopped(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		TriggerPostID: "trigger-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusStopped,
	}, nil)

	// Remove hourglass
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)

	// Add no_entry_sign
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "no_entry_sign"
	})).Return(nil, nil)

	// Stopped message
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" &&
			containsAll(p.Message, "Agent was stopped")
	})).Return(&model.Post{Id: "msg-1"}, nil)

	// Status updated
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.Status == "STOPPED"
	})).Return(nil)

	p.pollAgentStatuses()

	api.AssertExpectations(t)
}

func TestPoller_CursorAPIError(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		TriggerPostID: "trigger-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(nil, fmt.Errorf("API timeout"))

	p.pollAgentStatuses()

	// No state changes, no reactions, no posts.
	api.AssertNotCalled(t, "AddReaction")
	api.AssertNotCalled(t, "RemoveReaction")
	api.AssertNotCalled(t, "CreatePost")
	store.AssertNotCalled(t, "SaveAgent")
}

func TestPoller_MultipleAgents(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	records := []*kvstore.AgentRecord{
		{
			CursorAgentID: "agent-1",
			Status:        "RUNNING",
			TriggerPostID: "trigger-1",
			PostID:        "root-1",
			ChannelID:     "ch-1",
		},
		{
			CursorAgentID: "agent-2",
			Status:        "CREATING",
			TriggerPostID: "trigger-2",
			PostID:        "root-2",
			ChannelID:     "ch-2",
		},
	}

	store.On("ListActiveAgents").Return(records, nil)

	// agent-1: unchanged
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusRunning,
	}, nil)

	// agent-2: CREATING -> RUNNING
	cursorClient.On("GetAgent", mock.Anything, "agent-2").Return(&cursor.Agent{
		ID:     "agent-2",
		Status: cursor.AgentStatusRunning,
	}, nil)

	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-2"
	})).Return(&model.Post{Id: "msg-2"}, nil)

	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-2" && r.Status == "RUNNING"
	})).Return(nil)

	p.pollAgentStatuses()

	// Only agent-2 should have had state changes.
	cursorClient.AssertNumberOfCalls(t, "GetAgent", 2)
	store.AssertNumberOfCalls(t, "SaveAgent", 1)
}

func TestPoller_NilCursorClient(t *testing.T) {
	p, _, _, store := setupPollerPlugin(t)
	p.cursorClient = nil

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)

	// Should not panic.
	p.pollAgentStatuses()

	store.AssertNotCalled(t, "SaveAgent")
}

// containsAll checks if all substrings are present in the string.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
