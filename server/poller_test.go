package main

import (
	"fmt"
	"testing"
	"time"

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
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Phase 4: WebSocket events and post prop updates for all status changes.
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	api.On("GetPostThread", mock.Anything).Return(&model.PostList{Posts: map[string]*model.Post{}}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(nil, nil).Maybe()

	// GetPost for updateBotReplyWithAttachment calls.
	api.On("GetPost", mock.Anything).Return(&model.Post{
		Id:     "bot-reply-1",
		UserId: "bot-user-id",
		Props:  model.StringInterface{},
	}, nil).Maybe()

	// getUsername calls GetUser.
	api.On("GetUser", mock.AnythingOfType("string")).Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

	// getPluginURL calls GetConfig.
	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

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

func TestPoller_CleansStaleAgentsBeforePolling(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		TriggerPostID: "trigger-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
		CreatedAt:     time.Now().Add(-25 * time.Hour).UnixMilli(),
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-1" && r.Status == "STOPPED"
	})).Return(nil)

	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "no_entry_sign"
	})).Return(nil, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-1" &&
			post.ChannelId == "ch-1" &&
			post.UserId == "bot-user-id" &&
			containsSubstring(post.Message, "stopped")
	})).Return(&model.Post{Id: "msg-1"}, nil)

	p.pollAgentStatuses()

	// Stale cleanup should stop the record and skip API polling in this cycle.
	cursorClient.AssertNotCalled(t, "GetAgent")
	store.AssertCalled(t, "SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-1" && r.Status == "STOPPED"
	}))
	api.AssertExpectations(t)
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
	store.On("GetAgent", "agent-1").Return(record, nil)
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
	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusRunning,
	}, nil)

	// Short text notification posted in thread.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" &&
			p.UserId == "bot-user-id" &&
			containsSubstring(p.Message, "running")
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
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		BotReplyPostID: "bot-reply-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
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

	// Short text notification posted in thread with PR link.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" &&
			p.UserId == "bot-user-id" &&
			containsSubstring(p.Message, "finished") &&
			containsSubstring(p.Message, "View PR")
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
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		BotReplyPostID: "bot-reply-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
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

	// Short text notification posted in thread.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" &&
			p.UserId == "bot-user-id" &&
			containsSubstring(p.Message, "failed")
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
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		BotReplyPostID: "bot-reply-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
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

	// Short text notification posted in thread.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" &&
			p.UserId == "bot-user-id" &&
			containsSubstring(p.Message, "stopped")
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
	store.On("GetAgent", "agent-1").Return(records[0], nil)
	store.On("GetAgent", "agent-2").Return(records[1], nil)

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

func TestPoller_SkipsCancelledAgent(t *testing.T) {
	// Simulates the race: ListActiveAgents loaded the agent as RUNNING,
	// but the cancel handler has since set it to STOPPED in KV.
	p, api, cursorClient, store := setupPollerPlugin(t)

	staleRecord := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "RUNNING",
		TriggerPostID: "trigger-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
	}

	// The cancel handler already updated the record in KV to STOPPED.
	freshRecord := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "STOPPED",
		TriggerPostID: "trigger-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{staleRecord}, nil)
	store.On("GetAgent", "agent-1").Return(freshRecord, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusFinished, // Cursor says FINISHED, but we already cancelled locally
	}, nil)

	p.pollAgentStatuses()

	// The poller should skip this agent because the fresh record is terminal.
	api.AssertNotCalled(t, "AddReaction")
	api.AssertNotCalled(t, "RemoveReaction")
	api.AssertNotCalled(t, "CreatePost")
	store.AssertNotCalled(t, "SaveAgent")
}

// --- Phase 3: Workflow-aware poller routing tests ---

func TestPoller_PlannerFinished_RoutesToWorkflow(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID:  "planner-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		BotReplyPostID: "bot-reply-1",
		UserID:         "user-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "planner-1").Return(record, nil)
	cursorClient.On("GetAgent", mock.Anything, "planner-1").Return(&cursor.Agent{
		ID:     "planner-1",
		Status: cursor.AgentStatusFinished,
	}, nil)

	// The planner belongs to a workflow in the planning phase.
	store.On("GetWorkflowByAgent", "planner-1").Return("wf-1", nil)
	store.On("GetWorkflow", "wf-1").Return(&kvstore.HITLWorkflow{
		ID:             "wf-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		RootPostID:     "root-1",
		PlannerAgentID: "planner-1",
		Repository:     "org/repo",
		Branch:         "main",
		Model:          "auto",
		Phase:          kvstore.PhasePlanning,
	}, nil)

	// handlePlannerFinished will call GetConversation.
	cursorClient.On("GetConversation", mock.Anything, "planner-1").Return(&cursor.Conversation{
		Messages: []cursor.Message{
			{Type: "assistant_message", Text: "### Summary\nThe plan."},
		},
	}, nil)

	store.On("SaveWorkflow", mock.Anything).Return(nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "plan-review-post"}, nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	// SaveAgent for status update.
	store.On("SaveAgent", mock.Anything).Return(nil)

	p.pollAgentStatuses()

	// handleWorkflowAgentTerminal should return true for planners, so normal
	// handleAgentFinished (reactions, etc.) should NOT be called.
	api.AssertNotCalled(t, "RemoveReaction")
	api.AssertNotCalled(t, "AddReaction")

	// But the agent record should still be saved with updated status.
	store.AssertCalled(t, "SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "planner-1" && r.Status == "FINISHED"
	}))
}

func TestPoller_ImplementerFinished_NormalHandling(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID:  "impl-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		BotReplyPostID: "bot-reply-1",
		UserID:         "user-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "impl-1").Return(record, nil)
	cursorClient.On("GetAgent", mock.Anything, "impl-1").Return(&cursor.Agent{
		ID:      "impl-1",
		Status:  cursor.AgentStatusFinished,
		Summary: "Done",
		Target:  cursor.AgentTarget{PrURL: "https://github.com/org/repo/pull/99"},
	}, nil)

	// The implementer belongs to a workflow in the implementing phase.
	store.On("GetWorkflowByAgent", "impl-1").Return("wf-1", nil)
	store.On("GetWorkflow", "wf-1").Return(&kvstore.HITLWorkflow{
		ID:    "wf-1",
		Phase: kvstore.PhaseImplementing,
	}, nil)

	store.On("SaveWorkflow", mock.Anything).Return(nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	// Normal terminal handling should still run for implementers.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "white_check_mark"
	})).Return(nil, nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)

	p.pollAgentStatuses()

	// Normal finished handling should have run.
	api.AssertCalled(t, "RemoveReaction", mock.Anything)
	api.AssertCalled(t, "AddReaction", mock.Anything)
}

func TestPoller_NonWorkflowAgent_NormalHandling(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		BotReplyPostID: "bot-reply-1",
		UserID:         "user-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "agent-1").Return(record, nil)
	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:      "agent-1",
		Status:  cursor.AgentStatusFinished,
		Summary: "All done",
		Target:  cursor.AgentTarget{PrURL: "https://github.com/org/repo/pull/1"},
	}, nil)

	// Agent does NOT belong to any workflow.
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	// Normal finished handling.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "white_check_mark"
	})).Return(nil, nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.pollAgentStatuses()

	api.AssertCalled(t, "RemoveReaction", mock.Anything)
	api.AssertCalled(t, "AddReaction", mock.Anything)
}

// --- Review Loop poller tests ---

func TestPoller_FinishedWithPR_StartsReviewLoop(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	// Enable review loop.
	p.configuration.EnableAIReviewLoop = "true"
	p.configuration.GitHubPAT = "ghp_test"
	p.configuration.AIReviewerBots = "coderabbitai[bot]"

	ghMock := &mockGitHubClient{}
	p.githubClient = ghMock

	record := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		UserID:         "user-1",
		BotReplyPostID: "bot-reply-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:      "agent-1",
		Status:  cursor.AgentStatusFinished,
		Summary: "Fixed the bug",
		Target:  cursor.AgentTarget{PrURL: "https://github.com/org/repo/pull/42"},
	}, nil)

	// Normal finish handling mocks.
	api.On("RemoveReaction", mock.Anything).Return(nil)
	api.On("AddReaction", mock.Anything).Return(nil, nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)

	// Review loop mocks.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)
	store.On("SaveReviewLoop", mock.Anything).Return(nil)
	ghMock.On("MarkPRReadyForReview", mock.Anything, "org", "repo", 42).Return(nil)
	ghMock.On("RequestReviewers", mock.Anything, "org", "repo", 42, mock.Anything).Return(nil)

	p.pollAgentStatuses()

	// Verify review loop was started.
	store.AssertCalled(t, "SaveReviewLoop", mock.Anything)
	ghMock.AssertCalled(t, "RequestReviewers", mock.Anything, "org", "repo", 42, mock.Anything)
}

func TestPoller_FinishedWithPR_ReviewLoopDisabled(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	// Review loop NOT enabled.
	p.configuration.EnableAIReviewLoop = "false"

	record := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		UserID:         "user-1",
		BotReplyPostID: "bot-reply-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:      "agent-1",
		Status:  cursor.AgentStatusFinished,
		Summary: "Done",
		Target:  cursor.AgentTarget{PrURL: "https://github.com/org/repo/pull/42"},
	}, nil)

	api.On("RemoveReaction", mock.Anything).Return(nil)
	api.On("AddReaction", mock.Anything).Return(nil, nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)

	p.pollAgentStatuses()

	// Should NOT have attempted review loop.
	store.AssertNotCalled(t, "GetReviewLoopByPRURL")
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestPoller_FinishedNoPR_NoReviewLoop(t *testing.T) {
	p, api, cursorClient, store := setupPollerPlugin(t)

	p.configuration.EnableAIReviewLoop = "true"
	p.configuration.GitHubPAT = "ghp_test"

	record := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		Status:         "RUNNING",
		TriggerPostID:  "trigger-1",
		PostID:         "root-1",
		ChannelID:      "ch-1",
		UserID:         "user-1",
		BotReplyPostID: "bot-reply-1",
	}

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{record}, nil)
	store.On("GetAgent", "agent-1").Return(record, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:      "agent-1",
		Status:  cursor.AgentStatusFinished,
		Summary: "Done, no PR created",
		Target:  cursor.AgentTarget{PrURL: ""}, // No PR.
	}, nil)

	api.On("RemoveReaction", mock.Anything).Return(nil)
	api.On("AddReaction", mock.Anything).Return(nil, nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)

	p.pollAgentStatuses()

	// No PR URL => no review loop.
	store.AssertNotCalled(t, "GetReviewLoopByPRURL")
	store.AssertNotCalled(t, "SaveReviewLoop")
}
