package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/parser"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// createTestPNG generates a minimal valid PNG image with the given dimensions.
func createTestPNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

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

func (m *mockKVStore) GetAgentsByUser(userID string) ([]*kvstore.AgentRecord, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*kvstore.AgentRecord), args.Error(1)
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

// setupTestPlugin creates a Plugin with mocked dependencies for handler testing.
func setupTestPlugin(t *testing.T) (*Plugin, *plugintest.API, *mockCursorClient, *mockKVStore) {
	t.Helper()

	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	// ShouldProcessMessage calls GetUser to check if the poster is a bot.
	api.On("GetUser", "user-1").Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
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
		DefaultRepository:   "org/default-repo",
		DefaultBranch:       "main",
		DefaultModel:        "auto",
		AutoCreatePR:        true,
		EnableContextReview: false, // Default to false so existing tests pass unchanged.
		EnablePlanLoop:      false,
	}

	return p, api, cursorClient, store
}

func TestMessageHasBeenPosted_IgnoresBotPosts(t *testing.T) {
	p, _, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		UserId:    "bot-user-id",
		ChannelId: "ch-1",
		Message:   "@cursor fix something",
	}

	p.MessageHasBeenPosted(nil, post)

	// No store or API calls should be made.
	cursorClient.AssertNotCalled(t, "LaunchAgent")
	store.AssertNotCalled(t, "SaveAgent")
}

func TestMessageHasBeenPosted_EmptyPrompt_PostsHelp(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor",
	}

	// :eyes: added on mention detection.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// :eyes: removed on empty prompt error.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil)

	// Expect bot reply with help text.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "post-1" &&
			p.UserId == "bot-user-id" &&
			p.Message == "Please provide a prompt. Example: `@cursor fix the login bug`"
	})).Return(&model.Post{Id: "reply-1"}, nil)

	p.MessageHasBeenPosted(nil, post)

	api.AssertExpectations(t)
}

func TestMessageHasBeenPosted_LaunchesAgent(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor fix the login bug",
	}

	// Default resolution - user and channel settings
	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	// :eyes: added on mention detection.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// :eyes: removed when swapping to hourglass.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil)

	// Add hourglass reaction
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil, nil)

	// Launch agent - prompt is wrapped with system instructions
	cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return strings.Contains(req.Prompt.Text, "<system-instructions>") &&
			strings.Contains(req.Prompt.Text, "<task>") &&
			strings.Contains(req.Prompt.Text, "fix the login bug") &&
			req.Source.Repository == "https://github.com/org/default-repo" &&
			req.Source.Ref == "main"
	})).Return(&cursor.Agent{
		ID:     "agent-123",
		Status: cursor.AgentStatusCreating,
	}, nil)

	// Bot reply (now uses attachment, so check UserId and RootId instead of Message)
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "post-1" && p.UserId == "bot-user-id"
	})).Return(&model.Post{Id: "reply-1"}, nil)

	// Save agent record
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-123" &&
			r.TriggerPostID == "post-1" &&
			r.ChannelID == "ch-1" &&
			r.Repository == "org/default-repo" &&
			r.BotReplyPostID == "reply-1"
	})).Return(nil)

	// Set thread mapping
	store.On("SetThreadAgent", "post-1", "agent-123").Return(nil)

	// WebSocket event for agent created
	api.On("PublishWebSocketEvent", "agent_created", mock.Anything, mock.Anything).Return()

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertExpectations(t)
	store.AssertExpectations(t)
	api.AssertExpectations(t)
}

func TestMessageHasBeenPosted_NoRepo_PostsError(t *testing.T) {
	p, api, _, store := setupTestPlugin(t)

	// Override config to have no default repo.
	p.configuration = &configuration{
		DefaultBranch: "main",
		DefaultModel:  "auto",
	}

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor fix the bug",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	// :eyes: added on mention detection.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// :eyes: removed on repo error.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil)

	// Expect error reply about no repo.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.Message == "No repository specified. Set a default with `/cursor settings` or specify one: `@cursor in org/repo, fix the bug`"
	})).Return(&model.Post{Id: "reply-1"}, nil)

	p.MessageHasBeenPosted(nil, post)

	api.AssertExpectations(t)
}

func TestMessageHasBeenPosted_APIError_AddsX(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor fix the bug",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	// :eyes: added on mention detection.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// :eyes: removed when swapping to hourglass.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil)

	// Hourglass added
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil, nil)

	// Launch fails
	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(nil, &cursor.APIError{
		StatusCode: 500,
		Message:    "server error",
	})

	// Hourglass removed
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)

	// X added
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "x"
	})).Return(nil, nil)

	// Error reply
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "post-1"
	})).Return(&model.Post{Id: "reply-1"}, nil)

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertExpectations(t)
	api.AssertExpectations(t)
}

func TestMessageHasBeenPosted_FollowUp_RunningAgent(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-post-1",
		Message:   "also fix the tests",
	}

	// No HITL workflow in this thread.
	store.On("GetWorkflowByThread", "root-post-1").Return(nil, nil)

	// Thread -> agent mapping
	store.On("GetAgentIDByThread", "root-post-1").Return("agent-123", nil)
	store.On("GetAgent", "agent-123").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-123",
		Status:        "RUNNING",
		Repository:    "org/repo",
	}, nil)

	// Eyes reaction
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "reply-post-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// Follow-up API call
	cursorClient.On("AddFollowup", mock.Anything, "agent-123", mock.MatchedBy(func(req cursor.FollowupRequest) bool {
		return req.Prompt.Text == "also fix the tests"
	})).Return(&cursor.FollowupResponse{ID: "agent-123"}, nil)

	// Confirmation reply
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-1" && p.Message == ":speech_balloon: Follow-up sent to the running agent."
	})).Return(&model.Post{Id: "reply-2"}, nil)

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertExpectations(t)
	store.AssertExpectations(t)
	api.AssertExpectations(t)
}

func TestMessageHasBeenPosted_FollowUp_FinishedAgent_Ignored(t *testing.T) {
	p, _, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-post-1",
		Message:   "also fix the tests",
	}

	// No HITL workflow in this thread.
	store.On("GetWorkflowByThread", "root-post-1").Return(nil, nil)

	// Thread -> agent mapping (agent is FINISHED).
	store.On("GetAgentIDByThread", "root-post-1").Return("agent-123", nil)
	store.On("GetAgent", "agent-123").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-123",
		Status:        "FINISHED",
	}, nil)

	p.MessageHasBeenPosted(nil, post)

	// No follow-up, no agent launch.
	cursorClient.AssertNotCalled(t, "AddFollowup")
	cursorClient.AssertNotCalled(t, "LaunchAgent")
}

func TestMessageHasBeenPosted_MentionInThread_RunningAgent_SendsFollowUp(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-post-2",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-post-1",
		Message:   "@cursor also fix Y",
	}

	// No HITL workflow in this thread.
	store.On("GetWorkflowByThread", "root-post-1").Return(nil, nil)

	store.On("GetAgentIDByThread", "root-post-1").Return("agent-123", nil)
	store.On("GetAgent", "agent-123").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-123",
		Status:        "RUNNING",
		Repository:    "org/repo",
	}, nil)

	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "eyes"
	})).Return(nil, nil)

	cursorClient.On("AddFollowup", mock.Anything, "agent-123", mock.Anything).Return(&cursor.FollowupResponse{ID: "agent-123"}, nil)

	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.Message == ":speech_balloon: Follow-up sent to the running agent."
	})).Return(&model.Post{Id: "reply-3"}, nil)

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertNotCalled(t, "LaunchAgent")
	cursorClient.AssertExpectations(t)
}

func TestMessageHasBeenPosted_MentionInThread_FinishedAgent_LaunchesNew(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-post-3",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-post-1",
		Message:   "@cursor fix Y",
	}

	// No HITL workflow in this thread.
	store.On("GetWorkflowByThread", "root-post-1").Return(nil, nil)

	// First lookup: thread has agent, but it's finished.
	store.On("GetAgentIDByThread", "root-post-1").Return("agent-old", nil)
	store.On("GetAgent", "agent-old").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-old",
		Status:        "FINISHED",
		Repository:    "org/old-repo",
		Branch:        "develop",
	}, nil)

	// Default resolution
	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	// :eyes: added on mention detection.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "eyes"
	})).Return(nil, nil)

	// :eyes: removed when swapping to hourglass.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "eyes"
	})).Return(nil)

	// Hourglass reaction
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil, nil)

	// Thread enrichment: GetPostThread returns the thread, GetUser for display names.
	api.On("GetPostThread", "root-post-1").Return(&model.PostList{
		Order: []string{"root-post-1", "reply-post-3"},
		Posts: map[string]*model.Post{
			"root-post-1":  {Id: "root-post-1", UserId: "user-1", Message: "Bug: login is broken", CreateAt: 1000},
			"reply-post-3": {Id: "reply-post-3", UserId: "user-1", Message: "@cursor fix Y", CreateAt: 2000},
		},
	}, nil)

	// Launch uses repo/branch from old agent; prompt is enriched with thread context (fallback since bridgeClient is nil).
	cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return strings.Contains(req.Prompt.Text, "Thread Context") &&
			strings.Contains(req.Prompt.Text, "Bug: login is broken") &&
			req.Source.Repository == "https://github.com/org/old-repo" &&
			req.Source.Ref == "develop"
	})).Return(&cursor.Agent{
		ID:     "agent-new",
		Status: cursor.AgentStatusCreating,
	}, nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-4"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetThreadAgent", "root-post-1", "agent-new").Return(nil)
	api.On("PublishWebSocketEvent", "agent_created", mock.Anything, mock.Anything).Return()

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestMessageHasBeenPosted_ForceNew_InThread(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-force",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-post-1",
		Message:   "@cursor agent fix Y completely from scratch",
	}

	// Default resolution
	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	// :eyes: added on mention detection.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "eyes"
	})).Return(nil, nil)

	// :eyes: removed when swapping to hourglass.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "eyes"
	})).Return(nil)

	// ForceNew bypasses thread agent check and goes straight to launch.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil, nil)

	// Thread enrichment: GetPostThread returns the thread.
	api.On("GetPostThread", "root-post-1").Return(&model.PostList{
		Order: []string{"root-post-1", "reply-force"},
		Posts: map[string]*model.Post{
			"root-post-1": {Id: "root-post-1", UserId: "user-1", Message: "Original post", CreateAt: 1000},
			"reply-force": {Id: "reply-force", UserId: "user-1", Message: "@cursor agent fix Y completely from scratch", CreateAt: 2000},
		},
	}, nil)

	cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return strings.Contains(req.Prompt.Text, "Thread Context") &&
			strings.Contains(req.Prompt.Text, "Original post")
	})).Return(&cursor.Agent{
		ID:     "agent-forced",
		Status: cursor.AgentStatusCreating,
	}, nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-5"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetThreadAgent", "root-post-1", "agent-forced").Return(nil)
	api.On("PublishWebSocketEvent", "agent_created", mock.Anything, mock.Anything).Return()

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestDefaultResolution(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)

	// Global config: org/default-repo, main, auto, autoCreatePR=true
	// User settings: user/repo, develop, claude-sonnet
	// Channel settings: channel/repo, staging
	// Parsed mention: explicit-branch

	store.On("GetUserSettings", "user-1").Return(&kvstore.UserSettings{
		DefaultRepository: "user/repo",
		DefaultBranch:     "develop",
		DefaultModel:      "claude-sonnet",
	}, nil)
	store.On("GetChannelSettings", "ch-1").Return(&kvstore.ChannelSettings{
		DefaultRepository: "channel/repo",
		DefaultBranch:     "staging",
	}, nil)

	post := &model.Post{
		UserId:    "user-1",
		ChannelId: "ch-1",
	}

	parsed := &parser.ParsedMention{Prompt: "fix it", Branch: "explicit-branch"}

	repo, branch, modelName, autoCreatePR := p.resolveDefaults(post, parsed)
	// Channel overrides user, user overrides global.
	// Parsed overrides everything.
	assert.Equal(t, "channel/repo", repo)       // channel > user > global
	assert.Equal(t, "explicit-branch", branch)  // parsed > channel > user > global
	assert.Equal(t, "claude-sonnet", modelName) // user > global (channel doesn't set model)
	assert.True(t, autoCreatePR)                // global default (no override)
}

func TestContainsMention(t *testing.T) {
	assert.True(t, containsMention("hey @cursor fix it", "@cursor"))
	assert.True(t, containsMention("hey @Cursor fix it", "@cursor"))
	assert.True(t, containsMention("@CURSOR fix it", "@cursor"))
	assert.False(t, containsMention("hey fix it", "@cursor"))
	assert.False(t, containsMention("", "@cursor"))
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"fix the login bug", "cursor/fix-the-login-bug"},
		{"Fix Bug #123!", "cursor/fix-bug-123"},
		{"a very long prompt that exceeds fifty characters and should be truncated", "cursor/a-very-long-prompt-that-exceeds-fifty-characters-a"},
		{"---leading-and-trailing---", "cursor/leading-and-trailing"},
		{"UPPERCASE", "cursor/uppercase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeBranchName(tt.input))
		})
	}

	// Non-alpha prompts should produce a fallback name instead of "cursor/".
	t.Run("all non-alpha falls back to agent timestamp", func(t *testing.T) {
		result := sanitizeBranchName("!!!")
		assert.True(t, strings.HasPrefix(result, "cursor/agent-"), "expected fallback prefix, got: %s", result)
	})
}

func TestEnrichFromThread_NotAThread(t *testing.T) {
	p, _, _, _ := setupTestPlugin(t)

	// Root post (no RootId) should return nil.
	post := &model.Post{Id: "post-1", UserId: "user-1", ChannelId: "ch-1"}
	result := p.enrichFromThread(post)
	assert.Nil(t, result)
}

func TestEnrichFromThread_FallbackToRawText(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	// bridgeClient is nil, so enrichment should fall back to raw thread text.
	post := &model.Post{
		Id:        "reply-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-1",
		Message:   "@cursor fix this",
	}

	api.On("GetPostThread", "root-1").Return(&model.PostList{
		Order: []string{"root-1", "reply-1"},
		Posts: map[string]*model.Post{
			"root-1":  {Id: "root-1", UserId: "user-1", Message: "The login page has a bug", CreateAt: 1000},
			"reply-1": {Id: "reply-1", UserId: "user-1", Message: "@cursor fix this", CreateAt: 2000},
		},
	}, nil)

	result := p.enrichFromThread(post)
	assert.NotNil(t, result)
	assert.Contains(t, result.Prompt, "--- Thread Context ---")
	assert.Contains(t, result.Prompt, "The login page has a bug")
	assert.Contains(t, result.Prompt, "@cursor fix this")
	assert.Contains(t, result.Prompt, "--- End Thread Context ---")
	assert.Empty(t, result.Images)
}

func TestEnrichFromThread_WithImages(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-1",
		Message:   "@cursor fix this",
	}

	api.On("GetPostThread", "root-1").Return(&model.PostList{
		Order: []string{"root-1", "reply-1"},
		Posts: map[string]*model.Post{
			"root-1": {
				Id: "root-1", UserId: "user-1", Message: "Here is the screenshot",
				CreateAt: 1000, FileIds: model.StringArray{"file-1"},
			},
			"reply-1": {Id: "reply-1", UserId: "user-1", Message: "@cursor fix this", CreateAt: 2000},
		},
	}, nil)

	api.On("GetFileInfo", "file-1").Return(&model.FileInfo{
		Id:       "file-1",
		MimeType: "image/png",
		Size:     100,
	}, nil)

	testPNG := createTestPNG(100, 50)
	api.On("GetFile", "file-1").Return(testPNG, nil)

	result := p.enrichFromThread(post)
	assert.NotNil(t, result)
	assert.Len(t, result.Images, 1)
	assert.Equal(t, 100, result.Images[0].Dimension.Width)
	assert.Equal(t, 50, result.Images[0].Dimension.Height)
	assert.NotEmpty(t, result.Images[0].Data)
}

func TestEnrichFromThread_SkipsUndecodableImages(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-1",
		Message:   "@cursor fix this",
	}

	api.On("GetPostThread", "root-1").Return(&model.PostList{
		Order: []string{"root-1"},
		Posts: map[string]*model.Post{
			"root-1": {
				Id: "root-1", UserId: "user-1", Message: "Screenshot",
				CreateAt: 1000, FileIds: model.StringArray{"file-bad"},
			},
		},
	}, nil)

	api.On("GetFileInfo", "file-bad").Return(&model.FileInfo{
		Id:       "file-bad",
		MimeType: "image/png",
		Size:     100,
	}, nil)

	// Return data that is not a valid image.
	api.On("GetFile", "file-bad").Return([]byte("not-a-real-image"), nil)

	result := p.enrichFromThread(post)
	assert.NotNil(t, result)
	assert.Empty(t, result.Images, "images with undecodable data should be skipped")
}

func TestEnrichFromThread_SkipsNonImageFiles(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	post := &model.Post{
		Id:     "reply-1",
		UserId: "user-1",
		RootId: "root-1",
	}

	api.On("GetPostThread", "root-1").Return(&model.PostList{
		Order: []string{"root-1"},
		Posts: map[string]*model.Post{
			"root-1": {
				Id: "root-1", UserId: "user-1", Message: "See attached",
				CreateAt: 1000, FileIds: model.StringArray{"file-pdf"},
			},
		},
	}, nil)

	api.On("GetFileInfo", "file-pdf").Return(&model.FileInfo{
		Id:       "file-pdf",
		MimeType: "application/pdf",
		Size:     500,
	}, nil)

	result := p.enrichFromThread(post)
	assert.NotNil(t, result)
	assert.Empty(t, result.Images)
}

func TestEnrichFromThread_GetPostThreadFails(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	post := &model.Post{
		Id:     "reply-1",
		UserId: "user-1",
		RootId: "root-1",
	}

	api.On("GetPostThread", "root-1").Return(nil, model.NewAppError("test", "error", nil, "", 500))

	result := p.enrichFromThread(post)
	assert.Nil(t, result)
}

func TestFormatThread_ChronologicalOrder(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	// Posts are in reverse order in the Order slice.
	postList := &model.PostList{
		Order: []string{"post-2", "post-1"},
		Posts: map[string]*model.Post{
			"post-1": {Id: "post-1", UserId: "user-1", Message: "First message", CreateAt: 1000},
			"post-2": {Id: "post-2", UserId: "user-1", Message: "Second message", CreateAt: 2000},
		},
	}

	api.On("GetUser", "user-1").Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

	text, images := p.formatThread(postList)
	assert.Contains(t, text, "First message")
	assert.Contains(t, text, "Second message")
	// Verify chronological order: "First" should appear before "Second".
	firstIdx := strings.Index(text, "First message")
	secondIdx := strings.Index(text, "Second message")
	assert.True(t, firstIdx < secondIdx, "Posts should be in chronological order")
	assert.Empty(t, images)
}

// --- HITL gate tests ---

func TestMessageHasBeenPosted_ContextReviewEnabled_PostsReviewInsteadOfLaunching(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)
	p.configuration = &configuration{
		DefaultRepository:   "org/default-repo",
		DefaultBranch:       "main",
		DefaultModel:        "auto",
		AutoCreatePR:        true,
		EnableContextReview: true, // HITL enabled.
		EnablePlanLoop:      false,
	}

	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor fix the bug",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	// :eyes: added on mention detection.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// :eyes: removed, hourglass added.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "eyes"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "post-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil, nil)

	// SaveWorkflow (called twice: initial + update with context post).
	store.On("SaveWorkflow", mock.Anything).Return(nil)
	store.On("SetThreadWorkflow", "post-1", mock.AnythingOfType("string")).Return(nil)

	// CreatePost for the review attachment.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "post-1" && p.UserId == "bot-user-id"
	})).Return(&model.Post{Id: "review-post-1"}, nil)

	// publishWorkflowPhaseChange is called after saving the workflow.
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.MessageHasBeenPosted(nil, post)

	// LaunchAgent should NOT be called.
	cursorClient.AssertNotCalled(t, "LaunchAgent")
	// SaveWorkflow should be called.
	store.AssertCalled(t, "SaveWorkflow", mock.Anything)
}

func TestMessageHasBeenPosted_ContextReviewDisabled_LaunchesDirectly(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)
	// EnableContextReview is false by default from setupTestPlugin.

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor fix the bug",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	api.On("AddReaction", mock.Anything).Return(nil, nil)
	api.On("RemoveReaction", mock.Anything).Return(nil)

	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(&cursor.Agent{
		ID:     "agent-123",
		Status: cursor.AgentStatusCreating,
	}, nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetThreadAgent", "post-1", "agent-123").Return(nil)
	api.On("PublishWebSocketEvent", "agent_created", mock.Anything, mock.Anything).Return()

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestMessageHasBeenPosted_DirectFlag_SkipsBothHITL(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)
	p.configuration = &configuration{
		DefaultRepository:   "org/default-repo",
		DefaultBranch:       "main",
		DefaultModel:        "auto",
		AutoCreatePR:        true,
		EnableContextReview: true,
		EnablePlanLoop:      true,
	}

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor --direct fix the bug",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	api.On("AddReaction", mock.Anything).Return(nil, nil)
	api.On("RemoveReaction", mock.Anything).Return(nil)

	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(&cursor.Agent{
		ID:     "agent-123",
		Status: cursor.AgentStatusCreating,
	}, nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetThreadAgent", "post-1", "agent-123").Return(nil)
	api.On("PublishWebSocketEvent", "agent_created", mock.Anything, mock.Anything).Return()

	p.MessageHasBeenPosted(nil, post)

	cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestMessageHasBeenPosted_NoReviewFlag_SkipsContextReviewOnly(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)
	p.configuration = &configuration{
		DefaultRepository:   "org/default-repo",
		DefaultBranch:       "main",
		DefaultModel:        "auto",
		AutoCreatePR:        true,
		EnableContextReview: true,
		EnablePlanLoop:      false,
	}

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		Message:   "@cursor --no-review fix the bug",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)
	store.On("GetChannelSettings", "ch-1").Return(nil, nil)

	api.On("AddReaction", mock.Anything).Return(nil, nil)
	api.On("RemoveReaction", mock.Anything).Return(nil)

	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(&cursor.Agent{
		ID:     "agent-123",
		Status: cursor.AgentStatusCreating,
	}, nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetThreadAgent", "post-1", "agent-123").Return(nil)
	api.On("PublishWebSocketEvent", "agent_created", mock.Anything, mock.Anything).Return()

	p.MessageHasBeenPosted(nil, post)

	// LaunchAgent should be called (context review skipped).
	cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestHandlePossibleFollowUp_WorkflowReply_IteratesContext(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	store := &mockKVStore{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.kvstore = store
	p.botUserID = "bot-user-id"
	p.botUsername = "cursor"
	p.configuration = &configuration{}

	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

	workflow := &kvstore.HITLWorkflow{
		ID:              "wf-1",
		UserID:          "user-1",
		ChannelID:       "ch-1",
		RootPostID:      "root-1",
		Repository:      "org/repo",
		Branch:          "main",
		Model:           "auto",
		EnrichedContext: "Original context",
		Phase:           kvstore.PhaseContextReview,
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)

	// User is the initiator and provides ShouldProcessMessage dependencies.
	api.On("GetUser", "user-1").Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

	// iterateContext mocks.
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "post-1"}, nil)
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	post := &model.Post{
		Id:        "reply-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-1",
		Message:   "refine this",
	}

	p.handlePossibleFollowUp(post)

	// iterateContext should update EnrichedContext.
	assert.Contains(t, workflow.EnrichedContext, "refine this")
}

func TestHandlePossibleFollowUp_AgentFollowUp_StillWorks(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	post := &model.Post{
		Id:        "reply-post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-post-1",
		Message:   "also fix the tests",
	}

	// No HITL workflow.
	store.On("GetWorkflowByThread", "root-post-1").Return(nil, nil)

	// Thread -> agent mapping.
	store.On("GetAgentIDByThread", "root-post-1").Return("agent-123", nil)
	store.On("GetAgent", "agent-123").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-123",
		Status:        "RUNNING",
		Repository:    "org/repo",
	}, nil)

	// Eyes reaction.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "reply-post-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// Follow-up API call.
	cursorClient.On("AddFollowup", mock.Anything, "agent-123", mock.Anything).Return(&cursor.FollowupResponse{ID: "agent-123"}, nil)

	// Confirmation reply.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-1" && p.Message == ":speech_balloon: Follow-up sent to the running agent."
	})).Return(&model.Post{Id: "reply-2"}, nil)

	p.handlePossibleFollowUp(post)

	cursorClient.AssertCalled(t, "AddFollowup", mock.Anything, "agent-123", mock.Anything)
}
