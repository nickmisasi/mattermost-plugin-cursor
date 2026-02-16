package command

import (
	"context"
	"fmt"
	"strings"
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

// mockCursorClient implements cursor.Client for testing.
type mockCursorClient struct {
	mock.Mock
}

func (m *mockCursorClient) LaunchAgent(ctx context.Context, req cursor.LaunchAgentRequest) (*cursor.Agent, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.Agent), args.Error(1)
}

func (m *mockCursorClient) GetAgent(ctx context.Context, id string) (*cursor.Agent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.Agent), args.Error(1)
}

func (m *mockCursorClient) ListAgents(ctx context.Context, limit int, cur string) (*cursor.ListAgentsResponse, error) {
	args := m.Called(ctx, limit, cur)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.ListAgentsResponse), args.Error(1)
}

func (m *mockCursorClient) AddFollowup(ctx context.Context, id string, req cursor.FollowupRequest) (*cursor.FollowupResponse, error) {
	args := m.Called(ctx, id, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.FollowupResponse), args.Error(1)
}

func (m *mockCursorClient) GetConversation(ctx context.Context, id string) (*cursor.Conversation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.Conversation), args.Error(1)
}

func (m *mockCursorClient) StopAgent(ctx context.Context, id string) (*cursor.StopResponse, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.StopResponse), args.Error(1)
}

func (m *mockCursorClient) DeleteAgent(ctx context.Context, id string) (*cursor.DeleteResponse, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.DeleteResponse), args.Error(1)
}

func (m *mockCursorClient) ListModels(ctx context.Context) (*cursor.ListModelsResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.ListModelsResponse), args.Error(1)
}

func (m *mockCursorClient) GetMe(ctx context.Context) (*cursor.APIKeyInfo, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cursor.APIKeyInfo), args.Error(1)
}

// mockKVStore implements kvstore.KVStore for testing.
type mockKVStore struct {
	mock.Mock
}

func (m *mockKVStore) GetAgent(cursorAgentID string) (*kvstore.AgentRecord, error) {
	args := m.Called(cursorAgentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.AgentRecord), args.Error(1)
}

func (m *mockKVStore) SaveAgent(record *kvstore.AgentRecord) error {
	return m.Called(record).Error(0)
}

func (m *mockKVStore) DeleteAgent(cursorAgentID string) error {
	return m.Called(cursorAgentID).Error(0)
}

func (m *mockKVStore) ListActiveAgents() ([]*kvstore.AgentRecord, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*kvstore.AgentRecord), args.Error(1)
}

func (m *mockKVStore) GetAgentsByUser(userID string) ([]*kvstore.AgentRecord, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*kvstore.AgentRecord), args.Error(1)
}

func (m *mockKVStore) GetAgentIDByThread(rootPostID string) (string, error) {
	args := m.Called(rootPostID)
	return args.String(0), args.Error(1)
}

func (m *mockKVStore) SetThreadAgent(rootPostID string, cursorAgentID string) error {
	return m.Called(rootPostID, cursorAgentID).Error(0)
}

func (m *mockKVStore) DeleteThreadAgent(rootPostID string) error {
	return m.Called(rootPostID).Error(0)
}

func (m *mockKVStore) GetChannelSettings(channelID string) (*kvstore.ChannelSettings, error) {
	args := m.Called(channelID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.ChannelSettings), args.Error(1)
}

func (m *mockKVStore) SaveChannelSettings(channelID string, settings *kvstore.ChannelSettings) error {
	return m.Called(channelID, settings).Error(0)
}

func (m *mockKVStore) GetUserSettings(userID string) (*kvstore.UserSettings, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.UserSettings), args.Error(1)
}

func (m *mockKVStore) SaveUserSettings(userID string, settings *kvstore.UserSettings) error {
	return m.Called(userID, settings).Error(0)
}

func (m *mockKVStore) GetAgentByPRURL(prURL string) (*kvstore.AgentRecord, error) {
	args := m.Called(prURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.AgentRecord), args.Error(1)
}

func (m *mockKVStore) GetAgentByBranch(branchName string) (*kvstore.AgentRecord, error) {
	args := m.Called(branchName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.AgentRecord), args.Error(1)
}

func (m *mockKVStore) HasDeliveryBeenProcessed(deliveryID string) (bool, error) {
	args := m.Called(deliveryID)
	return args.Bool(0), args.Error(1)
}

func (m *mockKVStore) MarkDeliveryProcessed(deliveryID string) error {
	return m.Called(deliveryID).Error(0)
}

func (m *mockKVStore) GetWorkflow(workflowID string) (*kvstore.HITLWorkflow, error) {
	args := m.Called(workflowID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.HITLWorkflow), args.Error(1)
}

func (m *mockKVStore) SaveWorkflow(workflow *kvstore.HITLWorkflow) error {
	return m.Called(workflow).Error(0)
}

func (m *mockKVStore) DeleteWorkflow(workflowID string) error {
	return m.Called(workflowID).Error(0)
}

func (m *mockKVStore) GetWorkflowByThread(rootPostID string) (*kvstore.HITLWorkflow, error) {
	args := m.Called(rootPostID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.HITLWorkflow), args.Error(1)
}

func (m *mockKVStore) GetWorkflowByAgent(cursorAgentID string) (string, error) {
	args := m.Called(cursorAgentID)
	return args.String(0), args.Error(1)
}

func (m *mockKVStore) SetThreadWorkflow(rootPostID string, workflowID string) error {
	return m.Called(rootPostID, workflowID).Error(0)
}

func (m *mockKVStore) SetAgentWorkflow(cursorAgentID string, workflowID string) error {
	return m.Called(cursorAgentID, workflowID).Error(0)
}

func (m *mockKVStore) DeleteAgentWorkflow(cursorAgentID string) error {
	return m.Called(cursorAgentID).Error(0)
}

func (m *mockKVStore) GetReviewLoop(reviewLoopID string) (*kvstore.ReviewLoop, error) {
	args := m.Called(reviewLoopID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.ReviewLoop), args.Error(1)
}

func (m *mockKVStore) SaveReviewLoop(loop *kvstore.ReviewLoop) error {
	return m.Called(loop).Error(0)
}

func (m *mockKVStore) DeleteReviewLoop(reviewLoopID string) error {
	return m.Called(reviewLoopID).Error(0)
}

func (m *mockKVStore) GetReviewLoopByPRURL(prURL string) (*kvstore.ReviewLoop, error) {
	args := m.Called(prURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.ReviewLoop), args.Error(1)
}

func (m *mockKVStore) GetReviewLoopByAgent(agentRecordID string) (*kvstore.ReviewLoop, error) {
	args := m.Called(agentRecordID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*kvstore.ReviewLoop), args.Error(1)
}

type testEnv struct {
	handler      Command
	api          *plugintest.API
	cursorClient *mockCursorClient
	store        *mockKVStore
}

func setupTest(t *testing.T) *testEnv {
	t.Helper()

	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("RegisterCommand", mock.AnythingOfType("*model.Command")).Return(nil)

	client := pluginapi.NewClient(api, nil)
	cc := &mockCursorClient{}
	s := &mockKVStore{}

	handler := NewHandler(Dependencies{
		Client:         client,
		CursorClientFn: func() cursor.Client { return cc },
		Store:          s,
		BotUserID:      "bot-user-id",
		SiteURL:        "http://localhost:8065",
		PluginID:       "com.mattermost.plugin-cursor",
	})

	return &testEnv{
		handler:      handler,
		api:          api,
		cursorClient: cc,
		store:        s,
	}
}

func TestHelp_NoArgs(t *testing.T) {
	env := setupTest(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, model.CommandResponseTypeEphemeral, resp.ResponseType)
	assert.Contains(t, resp.Text, "Cursor Background Agents - Help")
	assert.Contains(t, resp.Text, "/cursor list")
}

func TestHelp_Explicit(t *testing.T) {
	env := setupTest(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor help",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, model.CommandResponseTypeEphemeral, resp.ResponseType)
	assert.Contains(t, resp.Text, "Cursor Background Agents - Help")
}

func TestList_NoAgents(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetAgentsByUser", "user-1").Return([]*kvstore.AgentRecord{}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor list",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "You have no agents")
}

func TestList_WithAgents(t *testing.T) {
	env := setupTest(t)

	agents := []*kvstore.AgentRecord{
		{CursorAgentID: "agent-111111111", Status: "RUNNING", Repository: "org/repo1", UserID: "user-1"},
		{CursorAgentID: "agent-222222222", Status: "FINISHED", Repository: "org/repo2", UserID: "user-1"},
	}

	env.store.On("GetAgentsByUser", "user-1").Return(agents, nil)
	env.cursorClient.On("ListAgents", mock.Anything, 100, "").Return(&cursor.ListAgentsResponse{
		Agents: []cursor.Agent{
			{ID: "agent-111111111", Status: cursor.AgentStatusRunning},
			{ID: "agent-222222222", Status: cursor.AgentStatusFinished},
		},
	}, nil)
	// No workflow associations.
	env.store.On("GetWorkflowByAgent", mock.Anything).Return("", nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor list",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Your Cursor Agents")
	assert.Contains(t, resp.Text, "agent-11")
	assert.Contains(t, resp.Text, "agent-22")
	assert.Contains(t, resp.Text, "org/repo1")
	assert.Contains(t, resp.Text, "org/repo2")
	assert.Contains(t, resp.Text, "RUNNING")
	assert.Contains(t, resp.Text, "FINISHED")
}

func TestList_SyncsRemoteStatus(t *testing.T) {
	env := setupTest(t)

	agents := []*kvstore.AgentRecord{
		{CursorAgentID: "agent-sync", Status: "RUNNING", Repository: "org/repo", UserID: "user-1"},
	}

	env.store.On("GetAgentsByUser", "user-1").Return(agents, nil)
	env.cursorClient.On("ListAgents", mock.Anything, 100, "").Return(&cursor.ListAgentsResponse{
		Agents: []cursor.Agent{
			{ID: "agent-sync", Status: cursor.AgentStatusFinished},
		},
	}, nil)
	env.store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-sync" && r.Status == "FINISHED"
	})).Return(nil)
	env.store.On("GetWorkflowByAgent", "agent-sync").Return("", nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor list",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "FINISHED")
	env.store.AssertCalled(t, "SaveAgent", mock.Anything)
}

func TestStatus_MissingID(t *testing.T) {
	env := setupTest(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor status",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Usage:")
}

func TestStatus_ValidAgent(t *testing.T) {
	env := setupTest(t)

	env.cursorClient.On("GetAgent", mock.Anything, "abc123").Return(&cursor.Agent{
		ID:     "abc123",
		Status: cursor.AgentStatusRunning,
		Source: cursor.Source{
			Repository: "https://github.com/org/repo",
			Ref:        "main",
		},
		Target: cursor.AgentTarget{
			BranchName: "cursor/fix-bug",
		},
	}, nil)
	env.store.On("GetAgent", "abc123").Return(&kvstore.AgentRecord{
		CursorAgentID: "abc123",
		PostID:        "post-1",
		ChannelID:     "ch-1",
	}, nil)
	env.store.On("GetWorkflowByAgent", "abc123").Return("", nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor status abc123",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Agent Details: `abc123`")
	assert.Contains(t, resp.Text, "RUNNING")
	assert.Contains(t, resp.Text, "org/repo")
	assert.Contains(t, resp.Text, "cursor/fix-bug")
	assert.Contains(t, resp.Text, "Go to thread")
}

func TestStatus_APIError(t *testing.T) {
	env := setupTest(t)

	env.cursorClient.On("GetAgent", mock.Anything, "bad").Return(nil, &cursor.APIError{
		StatusCode: 404,
		Message:    "not found",
	})

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor status bad",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Failed to fetch agent")
}

func TestCancel_MissingID(t *testing.T) {
	env := setupTest(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Usage:")
}

func TestCancel_NotOwner(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "abc").Return(nil, nil)
	env.store.On("GetAgent", "abc").Return(&kvstore.AgentRecord{
		CursorAgentID: "abc",
		UserID:        "other-user",
		Status:        "RUNNING",
	}, nil)
	env.store.On("GetWorkflowByAgent", "abc").Return("", nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel abc",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "only cancel your own agents")
}

func TestCancel_AlreadyFinished(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "abc").Return(nil, nil)
	env.store.On("GetAgent", "abc").Return(&kvstore.AgentRecord{
		CursorAgentID: "abc",
		UserID:        "user-1",
		Status:        "FINISHED",
	}, nil)
	env.store.On("GetWorkflowByAgent", "abc").Return("", nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel abc",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "already FINISHED")
}

func TestCancel_Success(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "abc").Return(nil, nil)
	env.store.On("GetWorkflowByAgent", "abc").Return("", nil)
	env.store.On("GetAgent", "abc").Return(&kvstore.AgentRecord{
		CursorAgentID: "abc",
		UserID:        "user-1",
		Status:        "RUNNING",
		PostID:        "post-1",
		TriggerPostID: "trigger-1",
		ChannelID:     "ch-1",
	}, nil)

	env.cursorClient.On("StopAgent", mock.Anything, "abc").Return(&cursor.StopResponse{ID: "abc"}, nil)
	env.store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "abc" && r.Status == "STOPPED"
	})).Return(nil)

	// Bot posts cancel message in thread
	env.api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "post-1" && p.UserId == "bot-user-id"
	})).Return(&model.Post{Id: "cancel-reply"}, nil)

	// Reaction swaps
	env.api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)
	env.api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "no_entry_sign"
	})).Return(&model.Reaction{}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel abc",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "has been cancelled")
	env.cursorClient.AssertCalled(t, "StopAgent", mock.Anything, "abc")
	env.store.AssertCalled(t, "SaveAgent", mock.Anything)
}

func TestSettings_OpensDialog(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetChannelSettings", "ch-1").Return(&kvstore.ChannelSettings{
		DefaultRepository: "org/existing-repo",
		DefaultBranch:     "develop",
	}, nil)
	env.store.On("GetUserSettings", "user-1").Return(&kvstore.UserSettings{
		DefaultRepository: "user/repo",
		DefaultModel:      "claude-sonnet",
	}, nil)

	env.api.On("OpenInteractiveDialog", mock.MatchedBy(func(d model.OpenDialogRequest) bool {
		return d.Dialog.Title == "Cursor Settings" &&
			d.Dialog.State == "ch-1|user-1" &&
			d.URL == "http://localhost:8065/plugins/com.mattermost.plugin-cursor/api/v1/dialog/settings"
	})).Return(nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command:   "/cursor settings",
		ChannelId: "ch-1",
		UserId:    "user-1",
		TriggerId: "trigger-abc",
	})

	require.NoError(t, err)
	assert.Equal(t, "", resp.Text) // No ephemeral text on success
	env.api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
}

func TestModels_Success(t *testing.T) {
	env := setupTest(t)

	env.cursorClient.On("ListModels", mock.Anything).Return(&cursor.ListModelsResponse{
		Models: []string{"auto", "claude-sonnet-4", "gpt-4o"},
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor models",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Available Cursor Models")
	assert.Contains(t, resp.Text, "auto")
	assert.Contains(t, resp.Text, "claude-sonnet-4")
	assert.Contains(t, resp.Text, "gpt-4o")
}

func TestModels_APIError(t *testing.T) {
	env := setupTest(t)

	env.cursorClient.On("ListModels", mock.Anything).Return(nil, fmt.Errorf("connection refused"))

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor models",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Failed to fetch models")
}

func TestModels_Empty(t *testing.T) {
	env := setupTest(t)

	env.cursorClient.On("ListModels", mock.Anything).Return(&cursor.ListModelsResponse{
		Models: []string{},
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor models",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "No models available")
}

func TestLaunch_NoPrompt(t *testing.T) {
	env := setupTest(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor ",
	})

	require.NoError(t, err)
	// Empty after trimming falls through to help
	assert.Contains(t, resp.Text, "Cursor Background Agents - Help")
}

func TestLaunch_NoRepo(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetChannelSettings", "ch-1").Return(nil, nil)
	env.store.On("GetUserSettings", "user-1").Return(nil, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command:   "/cursor fix bug",
		ChannelId: "ch-1",
		UserId:    "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "No repository specified")
}

func TestLaunch_Success(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetChannelSettings", "ch-1").Return(&kvstore.ChannelSettings{
		DefaultRepository: "org/repo",
	}, nil)
	env.store.On("GetUserSettings", "user-1").Return(nil, nil)

	env.cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return req.Prompt.Text == "fix bug" &&
			req.Source.Repository == "https://github.com/org/repo" &&
			req.Source.Ref == "main"
	})).Return(&cursor.Agent{
		ID:     "new-agent",
		Status: cursor.AgentStatusCreating,
	}, nil)

	// CreatePost for the bot post (pluginapi sets post.Id in place)
	env.api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		// Simulate the server setting the Id
		p.Id = "bot-post-1"
		return p.UserId == "bot-user-id" && p.ChannelId == "ch-1"
	})).Return(&model.Post{Id: "bot-post-1"}, nil)

	// Add hourglass reaction
	env.api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(&model.Reaction{}, nil)

	// Save agent record (includes BotReplyPostID)
	env.store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "new-agent" &&
			r.ChannelID == "ch-1" &&
			r.UserID == "user-1" &&
			r.Repository == "org/repo" &&
			r.BotReplyPostID == "bot-post-1"
	})).Return(nil)

	// Set thread mapping
	env.store.On("SetThreadAgent", mock.Anything, "new-agent").Return(nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command:   "/cursor fix bug",
		ChannelId: "ch-1",
		UserId:    "user-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "", resp.Text) // No ephemeral text on success
	env.cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestLaunch_WithInlineOptions(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetChannelSettings", "ch-1").Return(nil, nil)
	env.store.On("GetUserSettings", "user-1").Return(nil, nil)

	env.cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return req.Source.Repository == "https://github.com/custom/repo" &&
			req.Source.Ref == "dev"
	})).Return(&cursor.Agent{
		ID:     "agent-opts",
		Status: cursor.AgentStatusCreating,
	}, nil)

	env.api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		p.Id = "bot-post-opts"
		return true
	})).Return(&model.Post{Id: "bot-post-opts"}, nil)
	env.api.On("AddReaction", mock.Anything).Return(&model.Reaction{}, nil)
	env.store.On("SaveAgent", mock.Anything).Return(nil)
	env.store.On("SetThreadAgent", mock.Anything, "agent-opts").Return(nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command:   "/cursor repo=custom/repo branch=dev fix bug",
		ChannelId: "ch-1",
		UserId:    "user-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "", resp.Text)
	env.cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestUnknownFallsToLaunch(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetChannelSettings", "ch-1").Return(&kvstore.ChannelSettings{
		DefaultRepository: "org/repo",
	}, nil)
	env.store.On("GetUserSettings", "user-1").Return(nil, nil)

	env.cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return req.Prompt.Text == "fix the login bug"
	})).Return(&cursor.Agent{
		ID:     "agent-unknown",
		Status: cursor.AgentStatusCreating,
	}, nil)

	env.api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		p.Id = "bot-post-unknown"
		return true
	})).Return(&model.Post{Id: "bot-post-unknown"}, nil)
	env.api.On("AddReaction", mock.Anything).Return(&model.Reaction{}, nil)
	env.store.On("SaveAgent", mock.Anything).Return(nil)
	env.store.On("SetThreadAgent", mock.Anything, "agent-unknown").Return(nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command:   "/cursor fix the login bug",
		ChannelId: "ch-1",
		UserId:    "user-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "", resp.Text)
	env.cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestCancel_NotFound(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "nonexistent").Return(nil, nil)
	env.store.On("GetAgent", "nonexistent").Return(nil, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel nonexistent",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "not found")
}

func TestCancel_StopAPIError(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "abc").Return(nil, nil)
	env.store.On("GetWorkflowByAgent", "abc").Return("", nil)
	env.store.On("GetAgent", "abc").Return(&kvstore.AgentRecord{
		CursorAgentID: "abc",
		UserID:        "user-1",
		Status:        "RUNNING",
	}, nil)

	env.cursorClient.On("StopAgent", mock.Anything, "abc").Return(nil, fmt.Errorf("API down"))

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel abc",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Failed to cancel agent")
}

func TestCancel_AlreadyFailed(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "abc").Return(nil, nil)
	env.store.On("GetWorkflowByAgent", "abc").Return("", nil)
	env.store.On("GetAgent", "abc").Return(&kvstore.AgentRecord{
		CursorAgentID: "abc",
		UserID:        "user-1",
		Status:        "FAILED",
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel abc",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "already FAILED")
}

func TestCancel_AlreadyStopped(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "abc").Return(nil, nil)
	env.store.On("GetWorkflowByAgent", "abc").Return("", nil)
	env.store.On("GetAgent", "abc").Return(&kvstore.AgentRecord{
		CursorAgentID: "abc",
		UserID:        "user-1",
		Status:        "STOPPED",
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel abc",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "already STOPPED")
}

func TestLaunch_APIError(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetChannelSettings", "ch-1").Return(&kvstore.ChannelSettings{
		DefaultRepository: "org/repo",
	}, nil)
	env.store.On("GetUserSettings", "user-1").Return(nil, nil)

	env.cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("server error"))

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command:   "/cursor fix something",
		ChannelId: "ch-1",
		UserId:    "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Failed to launch agent")
}

func TestList_StoreError(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetAgentsByUser", "user-1").Return(nil, fmt.Errorf("store error"))

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor list",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Failed to retrieve agents")
}

func TestStatusToEmoji(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{"CREATING", ":arrows_counterclockwise:"},
		{"RUNNING", ":hourglass:"},
		{"FINISHED", ":white_check_mark:"},
		{"FAILED", ":x:"},
		{"STOPPED", ":no_entry_sign:"},
		{"unknown", ":grey_question:"},
		{"", ":grey_question:"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			assert.Equal(t, tt.expected, statusToEmoji(tt.status))
		})
	}
}

func TestCoalesce(t *testing.T) {
	assert.Equal(t, "first", coalesce("first", "second", "third"))
	assert.Equal(t, "second", coalesce("", "second", "third"))
	assert.Equal(t, "third", coalesce("", "", "third"))
	assert.Equal(t, "", coalesce("", "", ""))
	assert.Equal(t, "", coalesce())
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"fix the login bug", "fix-the-login-bug"},
		{"Fix Bug #123!", "fix-bug-123"},
		{"a very long prompt that exceeds fifty characters and should be truncated", "a-very-long-prompt-that-exceeds-fifty-characters-a"},
		{"---leading-and-trailing---", "leading-and-trailing"},
		{"UPPERCASE text", "uppercase-text"},
		{"hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeBranchName(tt.input))
		})
	}

	// Non-alpha prompts should produce a fallback name instead of empty string.
	t.Run("all non-alpha falls back to agent timestamp", func(t *testing.T) {
		result := sanitizeBranchName("!!!")
		assert.True(t, strings.HasPrefix(result, "agent-"), "expected fallback prefix, got: %s", result)
	})
}

func TestEphemeralResponse(t *testing.T) {
	resp := ephemeralResponse("test message")
	assert.Equal(t, model.CommandResponseTypeEphemeral, resp.ResponseType)
	assert.Equal(t, "test message", resp.Text)
}

func setupTestNilClient(t *testing.T) *testEnv {
	t.Helper()

	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("RegisterCommand", mock.AnythingOfType("*model.Command")).Return(nil)

	client := pluginapi.NewClient(api, nil)
	s := &mockKVStore{}

	handler := NewHandler(Dependencies{
		Client:         client,
		CursorClientFn: func() cursor.Client { return nil },
		Store:          s,
		BotUserID:      "bot-user-id",
		SiteURL:        "http://localhost:8065",
		PluginID:       "com.mattermost.plugin-cursor",
	})

	return &testEnv{
		handler:      handler,
		api:          api,
		cursorClient: nil,
		store:        s,
	}
}

func TestNilCursorClient_Models(t *testing.T) {
	env := setupTestNilClient(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor models",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Cursor API key is not configured")
}

func TestNilCursorClient_List(t *testing.T) {
	env := setupTestNilClient(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor list",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Cursor API key is not configured")
}

func TestNilCursorClient_Status(t *testing.T) {
	env := setupTestNilClient(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor status abc123",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Cursor API key is not configured")
}

func TestNilCursorClient_Cancel(t *testing.T) {
	env := setupTestNilClient(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel abc123",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Cursor API key is not configured")
}

func TestNilCursorClient_Launch(t *testing.T) {
	env := setupTestNilClient(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command:   "/cursor fix a bug",
		ChannelId: "ch-1",
		UserId:    "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Cursor API key is not configured")
}

func TestDispatch_AllSubcommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		check   func(*testing.T, *testEnv, *model.CommandResponse)
	}{
		{
			name:    "help with no args",
			command: "/cursor",
			check: func(t *testing.T, env *testEnv, resp *model.CommandResponse) {
				assert.Contains(t, resp.Text, "Help")
			},
		},
		{
			name:    "explicit help",
			command: "/cursor help",
			check: func(t *testing.T, env *testEnv, resp *model.CommandResponse) {
				assert.Contains(t, resp.Text, "Help")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupTest(t)
			resp, err := env.handler.Handle(&model.CommandArgs{Command: tt.command})
			require.NoError(t, err)
			tt.check(t, env, resp)
		})
	}
}

// --- HITL command tests ---

func TestHelp_ContainsHITLFlags(t *testing.T) {
	env := setupTest(t)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor help",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "--direct")
	assert.Contains(t, resp.Text, "--no-review")
	assert.Contains(t, resp.Text, "--no-plan")
	assert.Contains(t, resp.Text, "HITL")
	assert.Contains(t, resp.Text, "workflowID")
}

func TestStatus_ShowsWorkflowPhase(t *testing.T) {
	env := setupTest(t)

	env.cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusFinished,
		Source: cursor.Source{
			Repository: "https://github.com/org/repo",
			Ref:        "main",
		},
		Target: cursor.AgentTarget{
			BranchName: "cursor/fix-bug",
		},
	}, nil)
	env.store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PostID:        "post-1",
		ChannelID:     "ch-1",
	}, nil)
	env.store.On("GetWorkflowByAgent", "agent-1").Return("workflow-1", nil)
	env.store.On("GetWorkflow", "workflow-1").Return(&kvstore.HITLWorkflow{
		ID:                 "workflow-1",
		Phase:              kvstore.PhasePlanReview,
		PlanIterationCount: 2,
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor status agent-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "plan_review")
	assert.Contains(t, resp.Text, "v3") // iteration 2 -> "Plan v3"
	assert.Contains(t, resp.Text, "HITL Workflow")
}

func TestStatus_NoWorkflow(t *testing.T) {
	env := setupTest(t)

	env.cursorClient.On("GetAgent", mock.Anything, "agent-1").Return(&cursor.Agent{
		ID:     "agent-1",
		Status: cursor.AgentStatusRunning,
		Source: cursor.Source{
			Repository: "https://github.com/org/repo",
			Ref:        "main",
		},
		Target: cursor.AgentTarget{
			BranchName: "cursor/fix-bug",
		},
	}, nil)
	env.store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PostID:        "post-1",
		ChannelID:     "ch-1",
	}, nil)
	env.store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor status agent-1",
	})

	require.NoError(t, err)
	assert.NotContains(t, resp.Text, "HITL Workflow")
}

func TestCancel_CancelsWorkflowByWorkflowID(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "workflow-1").Return(&kvstore.HITLWorkflow{
		ID:             "workflow-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		RootPostID:     "root-1",
		TriggerPostID:  "trigger-1",
		Phase:          kvstore.PhasePlanning,
		PlannerAgentID: "planner-1",
	}, nil)
	env.store.On("GetAgent", "planner-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "planner-1",
		Status:        "RUNNING",
	}, nil)
	env.cursorClient.On("StopAgent", mock.Anything, "planner-1").Return(&cursor.StopResponse{}, nil)
	env.store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "planner-1" && r.Status == "STOPPED"
	})).Return(nil)
	env.store.On("SaveWorkflow", mock.MatchedBy(func(w *kvstore.HITLWorkflow) bool {
		return w.Phase == kvstore.PhaseRejected
	})).Return(nil)

	env.api.On("RemoveReaction", mock.Anything).Return(nil)
	env.api.On("AddReaction", mock.Anything).Return(&model.Reaction{}, nil)
	env.api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel workflow-1",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "cancelled")
	env.cursorClient.AssertCalled(t, "StopAgent", mock.Anything, "planner-1")
}

func TestCancel_CancelsWorkflowByAgentID(t *testing.T) {
	env := setupTest(t)

	// Not a workflow ID.
	env.store.On("GetWorkflow", "planner-1").Return(nil, nil)

	env.store.On("GetAgent", "planner-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "planner-1",
		UserID:        "user-1",
		Status:        "RUNNING",
	}, nil)
	env.store.On("GetWorkflowByAgent", "planner-1").Return("workflow-1", nil)
	env.store.On("GetWorkflow", "workflow-1").Return(&kvstore.HITLWorkflow{
		ID:             "workflow-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		RootPostID:     "root-1",
		TriggerPostID:  "trigger-1",
		Phase:          kvstore.PhasePlanning,
		PlannerAgentID: "planner-1",
	}, nil)

	env.store.On("GetAgent", "planner-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "planner-1",
		Status:        "RUNNING",
	}, nil)
	env.cursorClient.On("StopAgent", mock.Anything, "planner-1").Return(&cursor.StopResponse{}, nil)
	env.store.On("SaveAgent", mock.Anything).Return(nil)
	env.store.On("SaveWorkflow", mock.Anything).Return(nil)

	env.api.On("RemoveReaction", mock.Anything).Return(nil)
	env.api.On("AddReaction", mock.Anything).Return(&model.Reaction{}, nil)
	env.api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel planner-1",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "cancelled")
}

func TestCancel_WorkflowAlreadyComplete(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "workflow-1").Return(&kvstore.HITLWorkflow{
		ID:     "workflow-1",
		UserID: "user-1",
		Phase:  kvstore.PhaseComplete,
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel workflow-1",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "already complete")
}

func TestCancel_WorkflowNotOwner(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "workflow-1").Return(&kvstore.HITLWorkflow{
		ID:     "workflow-1",
		UserID: "other-user",
		Phase:  kvstore.PhasePlanning,
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel workflow-1",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "only cancel your own")
}

func TestCancel_WorkflowInContextReview_NoAgents(t *testing.T) {
	env := setupTest(t)

	env.store.On("GetWorkflow", "workflow-1").Return(&kvstore.HITLWorkflow{
		ID:            "workflow-1",
		UserID:        "user-1",
		ChannelID:     "ch-1",
		RootPostID:    "root-1",
		TriggerPostID: "trigger-1",
		Phase:         kvstore.PhaseContextReview,
		// No planner or implementer agents yet.
	}, nil)
	env.store.On("SaveWorkflow", mock.MatchedBy(func(w *kvstore.HITLWorkflow) bool {
		return w.Phase == kvstore.PhaseRejected
	})).Return(nil)

	env.api.On("RemoveReaction", mock.Anything).Return(nil)
	env.api.On("AddReaction", mock.Anything).Return(&model.Reaction{}, nil)
	env.api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "msg-1"}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor cancel workflow-1",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "cancelled")
	env.cursorClient.AssertNotCalled(t, "StopAgent")
}

func TestList_ShowsWorkflowPhase(t *testing.T) {
	env := setupTest(t)

	agents := []*kvstore.AgentRecord{
		{CursorAgentID: "agent-111111111", Status: "RUNNING", Repository: "org/repo1", UserID: "user-1"},
	}

	env.store.On("GetAgentsByUser", "user-1").Return(agents, nil)
	env.cursorClient.On("ListAgents", mock.Anything, 100, "").Return(&cursor.ListAgentsResponse{
		Agents: []cursor.Agent{
			{ID: "agent-111111111", Status: cursor.AgentStatusRunning},
		},
	}, nil)
	env.store.On("GetWorkflowByAgent", "agent-111111111").Return("workflow-1", nil)
	env.store.On("GetWorkflow", "workflow-1").Return(&kvstore.HITLWorkflow{
		ID:    "workflow-1",
		Phase: kvstore.PhasePlanning,
	}, nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor list",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Phase")
	assert.Contains(t, resp.Text, "planning")
}

func TestList_NoWorkflow_EmptyPhase(t *testing.T) {
	env := setupTest(t)

	agents := []*kvstore.AgentRecord{
		{CursorAgentID: "agent-222222222", Status: "RUNNING", Repository: "org/repo", UserID: "user-1"},
	}

	env.store.On("GetAgentsByUser", "user-1").Return(agents, nil)
	env.cursorClient.On("ListAgents", mock.Anything, 100, "").Return(&cursor.ListAgentsResponse{
		Agents: []cursor.Agent{
			{ID: "agent-222222222", Status: cursor.AgentStatusRunning},
		},
	}, nil)
	env.store.On("GetWorkflowByAgent", "agent-222222222").Return("", nil)

	resp, err := env.handler.Handle(&model.CommandArgs{
		Command: "/cursor list",
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.Text, "Phase")
	assert.Contains(t, resp.Text, "RUNNING")
}

func TestPhaseToEmoji(t *testing.T) {
	tests := []struct {
		phase    string
		expected string
	}{
		{kvstore.PhaseContextReview, ":eyes:"},
		{kvstore.PhasePlanning, ":hourglass:"},
		{kvstore.PhasePlanReview, ":clipboard:"},
		{kvstore.PhaseImplementing, ":gear:"},
		{kvstore.PhaseRejected, ":no_entry_sign:"},
		{kvstore.PhaseComplete, ":white_check_mark:"},
		{"unknown", ":grey_question:"},
		{"", ":grey_question:"},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			assert.Equal(t, tt.expected, phaseToEmoji(tt.phase))
		})
	}
}

func TestSafeUserEnableContextReview(t *testing.T) {
	assert.Equal(t, "", safeUserEnableContextReview(nil))

	assert.Equal(t, "", safeUserEnableContextReview(&kvstore.UserSettings{}))

	bTrue := true
	assert.Equal(t, "true", safeUserEnableContextReview(&kvstore.UserSettings{EnableContextReview: &bTrue}))

	bFalse := false
	assert.Equal(t, "false", safeUserEnableContextReview(&kvstore.UserSettings{EnableContextReview: &bFalse}))
}

func TestSafeUserEnablePlanLoop(t *testing.T) {
	assert.Equal(t, "", safeUserEnablePlanLoop(nil))

	assert.Equal(t, "", safeUserEnablePlanLoop(&kvstore.UserSettings{}))

	bTrue := true
	assert.Equal(t, "true", safeUserEnablePlanLoop(&kvstore.UserSettings{EnablePlanLoop: &bTrue}))

	bFalse := false
	assert.Equal(t, "false", safeUserEnablePlanLoop(&kvstore.UserSettings{EnablePlanLoop: &bFalse}))
}
