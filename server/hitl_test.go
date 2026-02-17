package main

import (
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

func boolPtr(v bool) *bool { return &v }

// --- resolveHITLFlags tests ---

func TestResolveHITLFlags_GlobalDefaults(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		EnableContextReview: "true",
		EnablePlanLoop:      "true",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)

	parsed := &parser.ParsedMention{Prompt: "fix the bug"}
	skipReview, skipPlan := p.resolveHITLFlags(parsed, "user-1")
	assert.False(t, skipReview)
	assert.False(t, skipPlan)
}

func TestResolveHITLFlags_GlobalDisabled(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		EnableContextReview: "false",
		EnablePlanLoop:      "false",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)

	parsed := &parser.ParsedMention{Prompt: "fix the bug"}
	skipReview, skipPlan := p.resolveHITLFlags(parsed, "user-1")
	assert.True(t, skipReview)
	assert.True(t, skipPlan)
}

func TestResolveHITLFlags_UserOverridesGlobal(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		EnableContextReview: "true",
		EnablePlanLoop:      "true",
	}

	store.On("GetUserSettings", "user-1").Return(&kvstore.UserSettings{
		EnableContextReview: boolPtr(false),
		EnablePlanLoop:      boolPtr(false),
	}, nil)

	parsed := &parser.ParsedMention{Prompt: "fix the bug"}
	skipReview, skipPlan := p.resolveHITLFlags(parsed, "user-1")
	assert.True(t, skipReview)
	assert.True(t, skipPlan)
}

func TestResolveHITLFlags_MentionOverridesUser(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		EnableContextReview: "false",
	}

	store.On("GetUserSettings", "user-1").Return(&kvstore.UserSettings{
		EnableContextReview: boolPtr(false),
	}, nil)

	// SkipReview: ptr(false) means "don't skip" = enable review.
	parsed := &parser.ParsedMention{Prompt: "fix the bug", SkipReview: boolPtr(false)}
	skipReview, _ := p.resolveHITLFlags(parsed, "user-1")
	assert.False(t, skipReview)
}

func TestResolveHITLFlags_Direct(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		EnableContextReview: "true",
		EnablePlanLoop:      "true",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)

	parsed := &parser.ParsedMention{Prompt: "fix the bug", Direct: true}
	skipReview, skipPlan := p.resolveHITLFlags(parsed, "user-1")
	assert.True(t, skipReview)
	assert.True(t, skipPlan)
}

func TestResolveHITLFlags_NoReviewFlag(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		EnableContextReview: "true",
		EnablePlanLoop:      "true",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)

	parsed := &parser.ParsedMention{Prompt: "fix the bug", SkipReview: boolPtr(true)}
	skipReview, skipPlan := p.resolveHITLFlags(parsed, "user-1")
	assert.True(t, skipReview)
	assert.False(t, skipPlan)
}

func TestResolveHITLFlags_NoPlanFlag(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		EnableContextReview: "true",
		EnablePlanLoop:      "true",
	}

	store.On("GetUserSettings", "user-1").Return(nil, nil)

	parsed := &parser.ParsedMention{Prompt: "fix the bug", SkipPlan: boolPtr(true)}
	skipReview, skipPlan := p.resolveHITLFlags(parsed, "user-1")
	assert.False(t, skipReview)
	assert.True(t, skipPlan)
}

// --- startContextReview tests ---

func TestStartContextReview_CreatesWorkflowAndPostsAttachment(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	store := &mockKVStore{}
	cursorClient := &mockCursorClient{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.cursorClient = cursorClient
	p.kvstore = store
	p.botUserID = "bot-user-id"
	p.botUsername = "cursor"
	p.configuration = &configuration{
		EnableContextReview: "true",
		EnablePlanLoop:      "false",
	}

	// Mock GetConfig for getPluginURL.
	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

	// Mock GetUser for getUsername.
	api.On("GetUser", mock.AnythingOfType("string")).Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
	}
	parsed := &parser.ParsedMention{Prompt: "fix the login bug"}

	// SaveWorkflow called twice (initial save + update with context post ID).
	store.On("SaveWorkflow", mock.MatchedBy(func(wf *kvstore.HITLWorkflow) bool {
		return wf.Phase == kvstore.PhaseContextReview &&
			wf.UserID == "user-1" &&
			wf.ChannelID == "ch-1" &&
			wf.Repository == "org/repo" &&
			wf.EnrichedContext == "Enriched text"
	})).Return(nil)

	store.On("SetThreadWorkflow", "post-1", mock.AnythingOfType("string")).Return(nil)

	// CreatePost for the review attachment -- check it contains buttons.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.UserId == "bot-user-id" &&
			p.ChannelId == "ch-1" &&
			p.RootId == "post-1"
	})).Return(&model.Post{Id: "review-post-1"}, nil)

	// publishWorkflowPhaseChange is called after saving workflow.
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.startContextReview(post, parsed, "org/repo", "main", "auto", true, "Enriched text", nil, false)

	store.AssertCalled(t, "SaveWorkflow", mock.Anything)
	store.AssertCalled(t, "SetThreadWorkflow", "post-1", mock.AnythingOfType("string"))
	api.AssertCalled(t, "CreatePost", mock.Anything)
}

func TestStartContextReview_KVSaveFailure_PostsFallbackMessage(t *testing.T) {
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
	p.configuration = &configuration{
		EnableContextReview: "true",
	}

	post := &model.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
	}
	parsed := &parser.ParsedMention{Prompt: "fix the bug"}

	// SaveWorkflow returns error.
	store.On("SaveWorkflow", mock.Anything).Return(assert.AnError)

	// Expect fallback bot reply.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.Message == "Failed to start context review. Launching agent directly."
	})).Return(&model.Post{Id: "reply-1"}, nil)

	p.startContextReview(post, parsed, "org/repo", "main", "auto", true, "text", nil, false)

	api.AssertCalled(t, "CreatePost", mock.Anything)
}

// --- acceptContext tests ---

func TestAcceptContext_SkipPlan_LaunchesImplementer(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
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
	p.configuration = &configuration{}

	workflow := &kvstore.HITLWorkflow{
		ID:              "wf-1",
		UserID:          "user-1",
		ChannelID:       "ch-1",
		RootPostID:      "root-1",
		TriggerPostID:   "trigger-1",
		Repository:      "org/repo",
		Branch:          "main",
		Model:           "auto",
		AutoCreatePR:    true,
		OriginalPrompt:  "fix the bug",
		EnrichedContext: "Enriched context",
		SkipPlanLoop:    true,
	}

	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// LaunchAgent is called from launchImplementerFromWorkflow.
	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(&cursor.Agent{
		ID:     "agent-impl-1",
		Status: cursor.AgentStatusCreating,
	}, nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetThreadAgent", "root-1", "agent-impl-1").Return(nil)
	store.On("SetAgentWorkflow", "agent-impl-1", "wf-1").Return(nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.acceptContext(workflow)

	assert.Equal(t, kvstore.PhaseImplementing, workflow.Phase)
	assert.Equal(t, "Enriched context", workflow.ApprovedContext)
	cursorClient.AssertCalled(t, "LaunchAgent", mock.Anything, mock.Anything)
}

func TestAcceptContext_PlanEnabled_TransitionsToPlanning(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
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
	p.configuration = &configuration{}

	workflow := &kvstore.HITLWorkflow{
		ID:              "wf-1",
		UserID:          "user-1",
		ChannelID:       "ch-1",
		RootPostID:      "root-1",
		TriggerPostID:   "trigger-1",
		Repository:      "org/repo",
		Branch:          "main",
		Model:           "auto",
		AutoCreatePR:    true,
		OriginalPrompt:  "fix the bug",
		EnrichedContext: "Enriched context",
		SkipPlanLoop:    false, // Plan loop enabled.
	}

	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// startPlanLoop launches a planner agent.
	cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return req.Target != nil &&
			req.Target.AutoCreatePr == false &&
			req.Target.AutoBranch == false
	})).Return(&cursor.Agent{
		ID:     "planner-1",
		Status: cursor.AgentStatusCreating,
	}, nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "reply-1"}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetAgentWorkflow", "planner-1", "wf-1").Return(nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.acceptContext(workflow)

	// Phase should be planning (planner launched).
	assert.Equal(t, kvstore.PhasePlanning, workflow.Phase)
	assert.Equal(t, "planner-1", workflow.PlannerAgentID)
}

// --- rejectWorkflow tests ---

func TestRejectWorkflow_SetsPhaseAndReactions(t *testing.T) {
	p, api, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:            "wf-1",
		TriggerPostID: "trigger-1",
		Phase:         kvstore.PhaseContextReview,
	}

	store.On("SaveWorkflow", mock.MatchedBy(func(wf *kvstore.HITLWorkflow) bool {
		return wf.Phase == kvstore.PhaseRejected
	})).Return(nil)

	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "hourglass_flowing_sand"
	})).Return(nil)

	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "no_entry_sign"
	})).Return(nil, nil)

	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.rejectWorkflow(workflow)

	assert.Equal(t, kvstore.PhaseRejected, workflow.Phase)
	api.AssertExpectations(t)
}

// --- iterateContext tests ---

func TestIterateContext_ReEnrichesAndPostsNewReview(t *testing.T) {
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
	p.configuration = &configuration{}
	// bridgeClient is nil, so enrichment falls back.

	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

	// Mock GetUser for getUsername.
	api.On("GetUser", mock.AnythingOfType("string")).Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

	workflow := &kvstore.HITLWorkflow{
		ID:              "wf-1",
		UserID:          "user-1",
		ChannelID:       "ch-1",
		RootPostID:      "root-1",
		ContextPostID:   "old-review-post",
		Repository:      "org/repo",
		Branch:          "main",
		Model:           "auto",
		EnrichedContext: "Original enriched context",
		Phase:           kvstore.PhaseContextReview,
	}

	post := &model.Post{
		Id:        "feedback-post",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-1",
		Message:   "add more detail about auth",
	}

	// Acknowledgment post.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.Message == "Re-analyzing with your feedback..."
	})).Return(&model.Post{Id: "ack-1"}, nil).Once()

	// New review post.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" && p.UserId == "bot-user-id" && p.Message == ""
	})).Return(&model.Post{Id: "new-review-post"}, nil).Once()

	// Update old context post to superseded.
	api.On("GetPost", "old-review-post").Return(&model.Post{
		Id:    "old-review-post",
		Props: model.StringInterface{},
	}, nil)
	api.On("UpdatePost", mock.Anything).Return(nil, nil)

	store.On("SaveWorkflow", mock.Anything).Return(nil)

	p.iterateContext(workflow, "add more detail about auth", post)

	// Context should be updated (fallback since bridgeClient is nil).
	assert.Contains(t, workflow.EnrichedContext, "--- Additional Context ---")
	assert.Contains(t, workflow.EnrichedContext, "add more detail about auth")
	assert.Equal(t, "new-review-post", workflow.ContextPostID)
}

func TestIterateContext_BridgeFailure_AppendsRawFeedback(t *testing.T) {
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
	p.configuration = &configuration{}

	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

	// Mock GetUser for getUsername.
	api.On("GetUser", mock.AnythingOfType("string")).Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

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

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "post-1"}, nil)
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	post := &model.Post{Id: "post-2", UserId: "user-1", ChannelId: "ch-1", RootId: "root-1"}
	p.iterateContext(workflow, "add this detail", post)

	assert.Contains(t, workflow.EnrichedContext, "--- Additional Context ---\nadd this detail")
}

// --- handlePossibleWorkflowReply tests ---

func TestHandlePossibleWorkflowReply_ContextReviewPhase_Iterates(t *testing.T) {
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
	p.configuration = &configuration{}

	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

	// Mock GetUser for getUsername.
	api.On("GetUser", mock.AnythingOfType("string")).Return(&model.User{
		Id:       "user-1",
		Username: "testuser",
	}, nil).Maybe()

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
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "post-1"}, nil)
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	post := &model.Post{
		Id:        "reply-1",
		UserId:    "user-1",
		ChannelId: "ch-1",
		RootId:    "root-1",
		Message:   "refine the context",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.True(t, result)
}

func TestHandlePossibleWorkflowReply_NotWorkflowThread_ReturnsFalse(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)

	store.On("GetWorkflowByThread", "root-1").Return(nil, nil)

	post := &model.Post{
		Id:     "reply-1",
		UserId: "user-1",
		RootId: "root-1",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.False(t, result)
}

func TestHandlePossibleWorkflowReply_DifferentUser_ReturnsFalse(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:     "wf-1",
		UserID: "user-1",
		Phase:  kvstore.PhaseContextReview,
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)

	post := &model.Post{
		Id:     "reply-1",
		UserId: "user-2", // Different user.
		RootId: "root-1",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.False(t, result)
}

func TestHandlePossibleWorkflowReply_ImplementingPhase_ReturnsFalse(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:     "wf-1",
		UserID: "user-1",
		Phase:  kvstore.PhaseImplementing,
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)

	post := &model.Post{
		Id:     "reply-1",
		UserId: "user-1",
		RootId: "root-1",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.False(t, result)
}

// --- Phase 3: Plan loop tests ---

func TestExtractPlanFromConversation(t *testing.T) {
	tests := []struct {
		name     string
		conv     *cursor.Conversation
		expected string
	}{
		{
			name:     "nil conversation",
			conv:     nil,
			expected: "",
		},
		{
			name:     "empty messages",
			conv:     &cursor.Conversation{Messages: []cursor.Message{}},
			expected: "",
		},
		{
			name: "only user messages",
			conv: &cursor.Conversation{
				Messages: []cursor.Message{
					{Type: "user_message", Text: "fix the bug"},
				},
			},
			expected: "",
		},
		{
			name: "single assistant message is the plan",
			conv: &cursor.Conversation{
				Messages: []cursor.Message{
					{Type: "user_message", Text: "fix the bug"},
					{Type: "assistant_message", Text: "### Summary\nHere is the plan..."},
				},
			},
			expected: "### Summary\nHere is the plan...",
		},
		{
			name: "last assistant message is the plan (multiple assistant messages)",
			conv: &cursor.Conversation{
				Messages: []cursor.Message{
					{Type: "user_message", Text: "fix the bug"},
					{Type: "assistant_message", Text: "I'm investigating the codebase..."},
					{Type: "assistant_message", Text: "I found the relevant files..."},
					{Type: "assistant_message", Text: "### Summary\nFinal plan here."},
				},
			},
			expected: "### Summary\nFinal plan here.",
		},
		{
			name: "trims whitespace from plan",
			conv: &cursor.Conversation{
				Messages: []cursor.Message{
					{Type: "assistant_message", Text: "  \n  plan text  \n  "},
				},
			},
			expected: "plan text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPlanFromConversation(tt.conv)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildPlannerPrompt(t *testing.T) {
	p, _, _, _ := setupTestPlugin(t)

	t.Run("first iteration uses approved context", func(t *testing.T) {
		workflow := &kvstore.HITLWorkflow{
			ApprovedContext:    "Fix the login bug in the auth module",
			OriginalPrompt:     "fix the login bug",
			PlanIterationCount: 0,
		}
		prompt := p.buildPlannerPrompt(workflow)
		assert.Contains(t, prompt, "<system-instructions>")
		assert.Contains(t, prompt, "PLANNING MODE")
		assert.Contains(t, prompt, "<task>")
		assert.Contains(t, prompt, "Fix the login bug in the auth module")
		assert.NotContains(t, prompt, "<previous-plan>")
		assert.NotContains(t, prompt, "<user-feedback>")
	})

	t.Run("second iteration includes previous plan and feedback", func(t *testing.T) {
		workflow := &kvstore.HITLWorkflow{
			ApprovedContext:    "Fix the login bug",
			OriginalPrompt:     "fix the login bug",
			PlanIterationCount: 1,
			RetrievedPlan:      "### Summary\nOld plan...",
			PlanFeedback:       "Also handle the edge case where token is expired",
		}
		prompt := p.buildPlannerPrompt(workflow)
		assert.Contains(t, prompt, "<previous-plan>")
		assert.Contains(t, prompt, "Old plan...")
		assert.Contains(t, prompt, "<user-feedback>")
		assert.Contains(t, prompt, "token is expired")
		assert.Contains(t, prompt, "revise the plan")
	})

	t.Run("falls back to original prompt when no approved context", func(t *testing.T) {
		workflow := &kvstore.HITLWorkflow{
			OriginalPrompt:     "fix the login bug",
			PlanIterationCount: 0,
		}
		prompt := p.buildPlannerPrompt(workflow)
		assert.Contains(t, prompt, "fix the login bug")
	})

	t.Run("custom planner system prompt", func(t *testing.T) {
		p.configuration = &configuration{PlannerSystemPrompt: "Custom planner instructions"}
		workflow := &kvstore.HITLWorkflow{
			OriginalPrompt: "fix it",
		}
		prompt := p.buildPlannerPrompt(workflow)
		assert.Contains(t, prompt, "Custom planner instructions")
		assert.NotContains(t, prompt, "PLANNING MODE")
		// Reset.
		p.configuration = &configuration{}
	})
}

func TestLaunchPlannerAgent(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:                 "wf-1",
		UserID:             "user-1",
		ChannelID:          "ch-1",
		RootPostID:         "root-1",
		TriggerPostID:      "trigger-1",
		Repository:         "org/repo",
		Branch:             "main",
		Model:              "auto",
		ApprovedContext:    "Fix the bug",
		OriginalPrompt:     "fix the bug",
		PlanIterationCount: 0,
	}

	// LaunchAgent should be called with autoBranch=false, autoCreatePr=false.
	cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return req.Target != nil &&
			req.Target.AutoCreatePr == false &&
			req.Target.AutoBranch == false &&
			strings.Contains(req.Prompt.Text, "PLANNING MODE") &&
			strings.Contains(req.Prompt.Text, "Fix the bug") &&
			req.Source.Repository == "https://github.com/org/repo" &&
			req.Source.Ref == "main"
	})).Return(&cursor.Agent{
		ID:     "planner-agent-1",
		Status: cursor.AgentStatusCreating,
	}, nil)

	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "planner-agent-1" &&
			strings.Contains(r.Prompt, "[planner iteration 0]")
	})).Return(nil)

	store.On("SaveWorkflow", mock.MatchedBy(func(w *kvstore.HITLWorkflow) bool {
		return w.ID == "wf-1" &&
			w.PlannerAgentID == "planner-agent-1" &&
			w.Phase == kvstore.PhasePlanning
	})).Return(nil)

	store.On("SetAgentWorkflow", "planner-agent-1", "wf-1").Return(nil)

	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	err := p.launchPlannerAgent(workflow)
	assert.NoError(t, err)
	assert.Equal(t, "planner-agent-1", workflow.PlannerAgentID)
	assert.Equal(t, kvstore.PhasePlanning, workflow.Phase)

	cursorClient.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestLaunchPlannerAgent_NilClient(t *testing.T) {
	p, _, _, _ := setupTestPlugin(t)
	p.cursorClient = nil

	workflow := &kvstore.HITLWorkflow{
		ID:         "wf-1",
		Repository: "org/repo",
		Branch:     "main",
	}

	err := p.launchPlannerAgent(workflow)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestHandlePlannerFinished_Success(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	siteURL := "http://localhost:8065"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()

	workflow := &kvstore.HITLWorkflow{
		ID:             "wf-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		RootPostID:     "root-1",
		PlannerAgentID: "planner-1",
		Repository:     "org/repo",
		Branch:         "main",
		Model:          "auto",
		Phase:          kvstore.PhasePlanning,
	}

	// GetConversation returns a conversation with plan in last assistant message.
	cursorClient.On("GetConversation", mock.Anything, "planner-1").Return(&cursor.Conversation{
		Messages: []cursor.Message{
			{Type: "user_message", Text: "fix the bug"},
			{Type: "assistant_message", Text: "Investigating..."},
			{Type: "assistant_message", Text: "### Summary\nHere is the plan."},
		},
	}, nil)

	// SaveWorkflow called multiple times (plan, then plan post ID).
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// Post the plan review attachment.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" && p.UserId == "bot-user-id"
	})).Return(&model.Post{Id: "plan-review-post-1"}, nil)

	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.handlePlannerFinished(workflow, &cursor.Agent{
		ID:     "planner-1",
		Status: cursor.AgentStatusFinished,
	})

	assert.Equal(t, kvstore.PhasePlanReview, workflow.Phase)
	assert.Equal(t, "### Summary\nHere is the plan.", workflow.RetrievedPlan)
	assert.Equal(t, "plan-review-post-1", workflow.PlanPostID)
	cursorClient.AssertExpectations(t)
}

func TestHandlePlannerFinished_PendingFeedback_AutoIterates(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

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
		PlannerAgentID:  "planner-1",
		Repository:      "org/repo",
		Branch:          "main",
		Model:           "auto",
		Phase:           kvstore.PhasePlanning,
		PendingFeedback: "also handle edge case Y",
		ApprovedContext: "Fix the bug",
		OriginalPrompt:  "fix the bug",
	}

	// GetConversation returns a valid plan.
	cursorClient.On("GetConversation", mock.Anything, "planner-1").Return(&cursor.Conversation{
		Messages: []cursor.Message{
			{Type: "assistant_message", Text: "### Summary\nFirst plan."},
		},
	}, nil)

	// SaveWorkflow called for clearing pending feedback, then by iteratePlan.
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// stopAgentIfRunning for the old planner.
	store.On("GetAgent", "planner-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "planner-1",
		Status:        "FINISHED",
	}, nil).Maybe()

	// Post acknowledgment for pending feedback.
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "post-1"}, nil).Maybe()

	// New planner launched.
	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(&cursor.Agent{
		ID:     "planner-2",
		Status: cursor.AgentStatusCreating,
	}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetAgentWorkflow", "planner-2", "wf-1").Return(nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.handlePlannerFinished(workflow, &cursor.Agent{
		ID:     "planner-1",
		Status: cursor.AgentStatusFinished,
	})

	// Plan should be stored from conversation.
	assert.Equal(t, "### Summary\nFirst plan.", workflow.RetrievedPlan)
	// PendingFeedback should have been cleared.
	assert.Empty(t, workflow.PendingFeedback)
	// PlanFeedback should have been set to the pending feedback text.
	assert.Equal(t, "also handle edge case Y", workflow.PlanFeedback)
	// Should have iterated (new planner launched).
	assert.Equal(t, "planner-2", workflow.PlannerAgentID)
	assert.Equal(t, 1, workflow.PlanIterationCount)
}

func TestHandlePlannerFinished_AgentFailed(t *testing.T) {
	p, api, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:             "wf-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		RootPostID:     "root-1",
		PlannerAgentID: "planner-1",
		Phase:          kvstore.PhasePlanning,
	}

	// Error message posted.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "failed")
	})).Return(&model.Post{Id: "error-post"}, nil)

	store.On("SaveWorkflow", mock.Anything).Return(nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.handlePlannerFinished(workflow, &cursor.Agent{
		ID:     "planner-1",
		Status: cursor.AgentStatusFailed,
	})

	assert.Equal(t, kvstore.PhasePlanReview, workflow.Phase)
}

func TestHandlePlannerFinished_EmptyPlan(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:             "wf-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		RootPostID:     "root-1",
		PlannerAgentID: "planner-1",
		Phase:          kvstore.PhasePlanning,
	}

	// GetConversation returns conversation with no assistant messages.
	cursorClient.On("GetConversation", mock.Anything, "planner-1").Return(&cursor.Conversation{
		Messages: []cursor.Message{
			{Type: "user_message", Text: "fix the bug"},
		},
	}, nil)

	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "no plan")
	})).Return(&model.Post{Id: "warning-post"}, nil)

	store.On("SaveWorkflow", mock.Anything).Return(nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.handlePlannerFinished(workflow, &cursor.Agent{
		ID:     "planner-1",
		Status: cursor.AgentStatusFinished,
	})

	assert.Equal(t, kvstore.PhasePlanReview, workflow.Phase)
}

func TestAcceptPlan(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:                 "wf-1",
		UserID:             "user-1",
		ChannelID:          "ch-1",
		RootPostID:         "root-1",
		TriggerPostID:      "trigger-1",
		Repository:         "org/repo",
		Branch:             "main",
		Model:              "auto",
		AutoCreatePR:       true,
		Phase:              kvstore.PhasePlanReview,
		RetrievedPlan:      "### Summary\nThe plan.",
		PlanPostID:         "plan-post-1",
		ApprovedContext:    "Fix the bug",
		OriginalPrompt:     "fix the bug",
		PlanIterationCount: 0,
		PlannerAgentID:     "planner-1",
	}

	// Save workflow after approval.
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// Update plan review post (remove buttons).
	api.On("GetPost", "plan-post-1").Return(&model.Post{
		Id:    "plan-post-1",
		Props: model.StringInterface{},
	}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(nil, nil).Maybe()

	// Planner is already finished -- stopAgentIfRunning checks the record.
	store.On("GetAgent", "planner-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "planner-1",
		Status:        "FINISHED",
	}, nil).Maybe()

	// Launch implementer.
	cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return strings.Contains(req.Prompt.Text, "<approved-plan>") &&
			strings.Contains(req.Prompt.Text, "The plan.") &&
			req.Target.AutoCreatePr == true
	})).Return(&cursor.Agent{
		ID:     "impl-agent-1",
		Status: cursor.AgentStatusCreating,
	}, nil)

	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetThreadAgent", "root-1", "impl-agent-1").Return(nil)
	store.On("SetAgentWorkflow", "impl-agent-1", "wf-1").Return(nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "launch-post"}, nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.acceptPlan(workflow)

	assert.Equal(t, "### Summary\nThe plan.", workflow.ApprovedPlan)
	assert.Equal(t, "impl-agent-1", workflow.ImplementerAgentID)
	assert.Equal(t, kvstore.PhaseImplementing, workflow.Phase)

	cursorClient.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestIteratePlan(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:                 "wf-1",
		UserID:             "user-1",
		ChannelID:          "ch-1",
		RootPostID:         "root-1",
		TriggerPostID:      "trigger-1",
		Repository:         "org/repo",
		Branch:             "main",
		Model:              "auto",
		Phase:              kvstore.PhasePlanReview,
		RetrievedPlan:      "### Summary\nOld plan.",
		ApprovedContext:    "Fix the bug",
		OriginalPrompt:     "fix the bug",
		PlannerAgentID:     "planner-1",
		PlanIterationCount: 0,
	}

	// Stop current planner (already finished, returns nil).
	store.On("GetAgent", "planner-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "planner-1",
		Status:        "FINISHED",
	}, nil).Maybe()

	// Save workflow with feedback.
	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// Post acknowledgment.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "revising") || containsSubstring(p.Message, "feedback")
	})).Return(&model.Post{Id: "ack-post"}, nil).Maybe()

	// Launch new planner with feedback in prompt.
	cursorClient.On("LaunchAgent", mock.Anything, mock.MatchedBy(func(req cursor.LaunchAgentRequest) bool {
		return strings.Contains(req.Prompt.Text, "<previous-plan>") &&
			strings.Contains(req.Prompt.Text, "Old plan.") &&
			strings.Contains(req.Prompt.Text, "<user-feedback>") &&
			strings.Contains(req.Prompt.Text, "Add error handling")
	})).Return(&cursor.Agent{
		ID:     "planner-2",
		Status: cursor.AgentStatusCreating,
	}, nil)

	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetAgentWorkflow", "planner-2", "wf-1").Return(nil)

	// Any additional CreatePost calls.
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "post-any"}, nil).Maybe()
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	p.iteratePlan(workflow, "Add error handling for expired tokens")

	assert.Equal(t, 1, workflow.PlanIterationCount)
	assert.Equal(t, "Add error handling for expired tokens", workflow.PlanFeedback)
	assert.Equal(t, "planner-2", workflow.PlannerAgentID)

	cursorClient.AssertExpectations(t)
}

func TestHandlePossibleWorkflowReply_PlanReviewPhase_IteratesPlan(t *testing.T) {
	p, api, cursorClient, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:              "wf-1",
		UserID:          "user-1",
		ChannelID:       "ch-1",
		RootPostID:      "root-1",
		Repository:      "org/repo",
		Branch:          "main",
		Model:           "auto",
		Phase:           kvstore.PhasePlanReview,
		RetrievedPlan:   "### Old plan",
		ApprovedContext: "Fix the bug",
		OriginalPrompt:  "fix the bug",
		PlannerAgentID:  "planner-1",
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)

	// Stop current planner.
	store.On("GetAgent", "planner-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "planner-1",
		Status:        "FINISHED",
	}, nil).Maybe()

	store.On("SaveWorkflow", mock.Anything).Return(nil)

	// New planner launched.
	cursorClient.On("LaunchAgent", mock.Anything, mock.Anything).Return(&cursor.Agent{
		ID:     "planner-2",
		Status: cursor.AgentStatusCreating,
	}, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)
	store.On("SetAgentWorkflow", "planner-2", "wf-1").Return(nil)

	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "post-1"}, nil).Maybe()
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	post := &model.Post{
		Id:      "reply-1",
		UserId:  "user-1",
		RootId:  "root-1",
		Message: "add more tests",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.True(t, result)
	assert.Equal(t, "planner-2", workflow.PlannerAgentID)
}

func TestHandlePossibleWorkflowReply_PlanningPhase_QueuesFeedback(t *testing.T) {
	p, api, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:         "wf-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		RootPostID: "root-1",
		Phase:      kvstore.PhasePlanning,
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)
	store.On("SaveWorkflow", mock.Anything).Return(nil)
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "apply your feedback")
	})).Return(&model.Post{Id: "ack"}, nil)

	post := &model.Post{
		Id:      "reply-1",
		UserId:  "user-1",
		RootId:  "root-1",
		Message: "also handle edge case X",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.True(t, result)
	assert.Equal(t, "also handle edge case X", workflow.PendingFeedback)
}

func TestHandlePossibleWorkflowReply_PlanningPhase_AppendsFeedback(t *testing.T) {
	p, api, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:              "wf-1",
		UserID:          "user-1",
		ChannelID:       "ch-1",
		RootPostID:      "root-1",
		Phase:           kvstore.PhasePlanning,
		PendingFeedback: "first feedback",
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)
	store.On("SaveWorkflow", mock.Anything).Return(nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "ack"}, nil)

	post := &model.Post{
		Id:      "reply-2",
		UserId:  "user-1",
		RootId:  "root-1",
		Message: "second feedback",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.True(t, result)
	assert.Equal(t, "first feedback\n\nsecond feedback", workflow.PendingFeedback)
}

func TestHandlePossibleWorkflowReply_PlanningPhase_StripsBotMention(t *testing.T) {
	p, api, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:         "wf-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		RootPostID: "root-1",
		Phase:      kvstore.PhasePlanning,
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)
	store.On("SaveWorkflow", mock.Anything).Return(nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "ack"}, nil)

	post := &model.Post{
		Id:      "reply-3",
		UserId:  "user-1",
		RootId:  "root-1",
		Message: "@cursor also add error handling",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.True(t, result)
	assert.Equal(t, "also add error handling", workflow.PendingFeedback)
}

func TestHandlePossibleWorkflowReply_PlanningPhase_EmptyAfterStrip_Ignored(t *testing.T) {
	p, _, _, store := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:         "wf-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		RootPostID: "root-1",
		Phase:      kvstore.PhasePlanning,
	}

	store.On("GetWorkflowByThread", "root-1").Return(workflow, nil)

	post := &model.Post{
		Id:      "reply-4",
		UserId:  "user-1",
		RootId:  "root-1",
		Message: "@cursor",
	}

	result := p.handlePossibleWorkflowReply(post)
	assert.True(t, result)
	assert.Empty(t, workflow.PendingFeedback)
}

func TestStopAgentIfRunning_AlreadyTerminal(t *testing.T) {
	p, _, cursorClient, store := setupTestPlugin(t)

	store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		Status:        "FINISHED",
	}, nil)

	p.stopAgentIfRunning("agent-1")

	// StopAgent should NOT be called because agent is already finished.
	cursorClient.AssertNotCalled(t, "StopAgent")
}

func TestStopAgentIfRunning_EmptyID(t *testing.T) {
	p, _, cursorClient, _ := setupTestPlugin(t)

	p.stopAgentIfRunning("")

	cursorClient.AssertNotCalled(t, "StopAgent")
	cursorClient.AssertNotCalled(t, "GetAgent")
}

// --- publishWorkflowPhaseChange tests ---

func TestPublishWorkflowPhaseChange(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:                 "wf-1",
		UserID:             "user-1",
		Phase:              kvstore.PhasePlanning,
		PlannerAgentID:     "planner-1",
		ImplementerAgentID: "impl-1",
		PlanIterationCount: 2,
		UpdatedAt:          3000,
	}

	api.On("PublishWebSocketEvent",
		"workflow_phase_change",
		mock.MatchedBy(func(data map[string]any) bool {
			return data["workflow_id"] == "wf-1" &&
				data["phase"] == "planning" &&
				data["planner_agent_id"] == "planner-1" &&
				data["implementer_agent_id"] == "impl-1" &&
				data["plan_iteration_count"] == "2" &&
				data["updated_at"] == "3000"
		}),
		mock.MatchedBy(func(broadcast *model.WebsocketBroadcast) bool {
			return broadcast.UserId == "user-1"
		}),
	).Return()

	p.publishWorkflowPhaseChange(workflow)

	api.AssertExpectations(t)
}

func TestPublishWorkflowPhaseChange_ZeroValues(t *testing.T) {
	p, api, _, _ := setupTestPlugin(t)

	workflow := &kvstore.HITLWorkflow{
		ID:     "wf-2",
		UserID: "user-2",
		Phase:  kvstore.PhaseContextReview,
		// All agent IDs and counts are zero values.
	}

	api.On("PublishWebSocketEvent",
		"workflow_phase_change",
		mock.MatchedBy(func(data map[string]any) bool {
			return data["workflow_id"] == "wf-2" &&
				data["phase"] == "context_review" &&
				data["planner_agent_id"] == "" &&
				data["implementer_agent_id"] == "" &&
				data["plan_iteration_count"] == "0" &&
				data["updated_at"] == "0"
		}),
		mock.MatchedBy(func(broadcast *model.WebsocketBroadcast) bool {
			return broadcast.UserId == "user-2"
		}),
	).Return()

	p.publishWorkflowPhaseChange(workflow)

	api.AssertExpectations(t)
}
