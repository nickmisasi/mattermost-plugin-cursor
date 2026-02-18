package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/ghclient"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// mockGitHubClient implements ghclient.Client for testing.
type mockGitHubClient struct {
	mock.Mock
}

// Verify interface compliance.
var _ ghclient.Client = (*mockGitHubClient)(nil)

func (m *mockGitHubClient) RequestReviewers(ctx context.Context, owner, repo string, prNumber int, reviewers github.ReviewersRequest) error {
	return m.Called(ctx, owner, repo, prNumber, reviewers).Error(0)
}

func (m *mockGitHubClient) CreateComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.IssueComment, error) {
	args := m.Called(ctx, owner, repo, prNumber, body)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*github.IssueComment), args.Error(1)
}

func (m *mockGitHubClient) ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error) {
	args := m.Called(ctx, owner, repo, prNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*github.PullRequestReview), args.Error(1)
}

func (m *mockGitHubClient) MarkPRReadyForReview(ctx context.Context, owner, repo string, prNumber int) error {
	return m.Called(ctx, owner, repo, prNumber).Error(0)
}

func (m *mockGitHubClient) ListReviewComments(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestComment, error) {
	args := m.Called(ctx, owner, repo, prNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*github.PullRequestComment), args.Error(1)
}

func (m *mockGitHubClient) GetPullRequestByBranch(ctx context.Context, owner, repo, branch string) (*github.PullRequest, error) {
	args := m.Called(ctx, owner, repo, branch)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*github.PullRequest), args.Error(1)
}

func setupReviewLoopTestPlugin(t *testing.T) (*Plugin, *mockPluginAPI, *mockKVStore, *mockGitHubClient) {
	t.Helper()
	p, api, _, store := setupTestPlugin(t)

	// Add broader LogError mock for review loop functions that pass 7+ args.
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Add LogWarn mock for non-fatal warnings (reviewer request failures, etc.).
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Add LogInfo mock.
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Default mock for publishReviewLoopChange WebSocket events.
	api.On("PublishWebSocketEvent", "review_loop_changed", mock.Anything, mock.Anything).Return().Maybe()

	ghMock := &mockGitHubClient{}
	p.githubClient = ghMock
	p.configuration = &configuration{
		CursorAPIKey:        "test-key",
		EnableAIReviewLoop:  true,
		MaxReviewIterations: 5,
		AIReviewerBots:      "coderabbitai[bot],copilot-pull-request-reviewer",
		GitHubPAT:           "ghp_test",
	}
	return p, api, store, ghMock
}

// mockInlineStatusUpdate sets up mocks for updateReviewLoopInlineStatus:
// GetAgent -> GetPost -> UpdatePost.
func mockInlineStatusUpdate(store *mockKVStore, api *mockPluginAPI, agentID string, record *kvstore.AgentRecord) {
	store.On("GetAgent", agentID).Return(record, nil).Maybe()
	if record != nil && record.BotReplyPostID != "" {
		api.On("GetPost", record.BotReplyPostID).Return(&model.Post{
			Id:        record.BotReplyPostID,
			ChannelId: record.ChannelID,
		}, nil).Maybe()
		api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil).Maybe()
	}
}

func TestStartReviewLoop(t *testing.T) {
	p, api, store, ghMock := setupReviewLoopTestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		PostID:         "root-1",
		TriggerPostID:  "trigger-1",
		BotReplyPostID: "reply-1",
		PrURL:          "https://github.com/org/repo/pull/42",
		Repository:     "org/repo",
	}

	// No existing review loop.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)

	// No HITL workflow.
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	// Save review loop (called twice: initial + awaiting_review transition).
	store.On("SaveReviewLoop", mock.MatchedBy(func(loop *kvstore.ReviewLoop) bool {
		return loop.Owner == "org" &&
			loop.Repo == "repo" &&
			loop.PRNumber == 42 &&
			loop.AgentRecordID == "agent-1"
	})).Return(nil)

	// Mark PR ready for review.
	ghMock.On("MarkPRReadyForReview", mock.Anything, "org", "repo", 42).Return(nil)

	// Request reviewers.
	ghMock.On("RequestReviewers", mock.Anything, "org", "repo", 42, mock.Anything).Return(nil)

	// Inline status update (replaces old thread notification).
	mockInlineStatusUpdate(store, api, "agent-1", record)

	// eyes reaction.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	err := p.startReviewLoop(record)
	require.NoError(t, err)
	store.AssertExpectations(t)
	ghMock.AssertExpectations(t)
}

func TestStartReviewLoop_AlreadyExists(t *testing.T) {
	p, _, store, ghMock := setupReviewLoopTestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PrURL:         "https://github.com/org/repo/pull/42",
	}

	// Existing review loop found.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(&kvstore.ReviewLoop{
		ID: "existing-loop",
	}, nil)

	err := p.startReviewLoop(record)
	require.NoError(t, err)
	// Should NOT have called SaveReviewLoop or RequestReviewers.
	store.AssertNotCalled(t, "SaveReviewLoop")
	ghMock.AssertNotCalled(t, "RequestReviewers")
}

func TestStartReviewLoop_InvalidPRURL(t *testing.T) {
	p, _, _, _ := setupReviewLoopTestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PrURL:         "not-a-valid-url",
	}

	err := p.startReviewLoop(record)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse PR URL")
}

func TestStartReviewLoop_RequestReviewersFails(t *testing.T) {
	p, api, store, ghMock := setupReviewLoopTestPlugin(t)

	record := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		PostID:         "root-1",
		TriggerPostID:  "trigger-1",
		BotReplyPostID: "reply-1",
		PrURL:          "https://github.com/org/repo/pull/42",
	}

	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)

	// Save for initial creation and awaiting_review transition.
	store.On("SaveReviewLoop", mock.Anything).Return(nil)

	// Mark PR ready for review.
	ghMock.On("MarkPRReadyForReview", mock.Anything, "org", "repo", 42).Return(nil)

	// RequestReviewers fails -- non-fatal, bots auto-detect PRs.
	ghMock.On("RequestReviewers", mock.Anything, "org", "repo", 42, mock.Anything).Return(fmt.Errorf("404 not found"))

	// Inline status update (loop transitions to awaiting_review).
	mockInlineStatusUpdate(store, api, "agent-1", record)

	// eyes reaction on trigger post.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	err := p.startReviewLoop(record)
	require.NoError(t, err) // Non-fatal: reviewer request failure doesn't fail the loop.
}

func TestHandleAIReview_CodeRabbitApproved(t *testing.T) {
	p, api, store, _ := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     1,
		TriggerPostID: "trigger-1",
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
	}

	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
		Repository:     "org/repo",
	}

	review := ghReview{
		State: "commented",
		Body:  "## Summary\n\nActionable comments posted: 0\n\nAll good!",
	}
	review.User.Login = "coderabbitai[bot]"

	pr := ghPullRequest{}

	// Save loop with approved phase.
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseApproved
	})).Return(nil).Once()

	// Save loop with human_review phase (from transitionToHumanReview).
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseHumanReview
	})).Return(nil).Once()

	// Inline status update (called twice: approved + human_review).
	mockInlineStatusUpdate(store, api, "agent-1", agentRecord)

	// Completion post for approved.
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-1" && hasAttachmentWithTitle(post, "CodeRabbit approved the PR!")
	})).Return(&model.Post{Id: "notif-1"}, nil)

	// Reaction swap.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "eyes"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "white_check_mark"
	})).Return(nil, nil)

	err := p.handleAIReview(loop, review, pr)
	require.NoError(t, err)
	store.AssertExpectations(t)
}

func TestHandleAIReview_CodeRabbitApprovedViaState(t *testing.T) {
	p, api, store, _ := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     2,
		TriggerPostID: "trigger-1",
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
	}

	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}

	review := ghReview{
		State: "approved", // Primary signal.
		Body:  "Looks good!",
	}
	review.User.Login = "coderabbitai[bot]"

	pr := ghPullRequest{}

	store.On("SaveReviewLoop", mock.Anything).Return(nil)
	mockInlineStatusUpdate(store, api, "agent-1", agentRecord)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "notif-1"}, nil)
	api.On("RemoveReaction", mock.Anything).Return(nil)
	api.On("AddReaction", mock.Anything).Return(nil, nil)

	err := p.handleAIReview(loop, review, pr)
	require.NoError(t, err)

	// Verify phase transitioned to approved (first save) then human_review (second save).
	store.AssertNumberOfCalls(t, "SaveReviewLoop", 2)
}

func TestHandleAIReview_FeedbackReceived(t *testing.T) {
	p, api, store, ghMock := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     1,
		TriggerPostID: "trigger-1",
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
		PRURL:         "https://github.com/org/repo/pull/42",
	}

	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}

	review := ghReview{
		State: "commented",
		Body:  "## Summary\n\nActionable comments posted: 3",
	}
	review.User.Login = "coderabbitai[bot]"

	pr := ghPullRequest{}

	// collectReviewFeedback calls.
	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("main.go"),
			Line:     github.Ptr(42),
			Body:     github.Ptr("Potential nil pointer here"),
			CommitID: github.Ptr("abc123"),
		},
	}, nil)
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)

	// CreateComment posts the @cursor fix.
	ghMock.On("CreateComment", mock.Anything, "org", "repo", 42, mock.MatchedBy(func(body string) bool {
		return strings.Contains(body, "@cursor") && strings.Contains(body, "nil pointer")
	})).Return(&github.IssueComment{}, nil)

	// SaveReviewLoop for cursor_fixing transition.
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseCursorFixing && l.Iteration == 2
	})).Return(nil)

	// Inline status update (replaces old thread notification).
	mockInlineStatusUpdate(store, api, "agent-1", agentRecord)

	err := p.handleAIReview(loop, review, pr)
	require.NoError(t, err)
	ghMock.AssertExpectations(t)
}

func TestHandleAIReview_MaxIterations(t *testing.T) {
	p, api, store, _ := setupReviewLoopTestPlugin(t)
	p.configuration.MaxReviewIterations = 3

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     3, // At max.
		TriggerPostID: "trigger-1",
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
	}

	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}

	review := ghReview{
		State: "commented",
		Body:  "Actionable comments posted: 2",
	}
	review.User.Login = "coderabbitai[bot]"

	pr := ghPullRequest{}

	// Save loop with max_iterations phase.
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseMaxIterations
	})).Return(nil)

	// Inline status update + completion post.
	mockInlineStatusUpdate(store, api, "agent-1", agentRecord)
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-1" && hasAttachmentWithTitle(post, "AI review loop reached the maximum")
	})).Return(&model.Post{Id: "notif-1"}, nil)

	// Reaction swap.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "eyes"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.EmojiName == "warning"
	})).Return(nil, nil)

	err := p.handleAIReview(loop, review, pr)
	require.NoError(t, err)
	store.AssertExpectations(t)
}

func TestHandleAIReview_NonCodeRabbitBot(t *testing.T) {
	p, _, store, _ := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:    "loop-1",
		Phase: kvstore.ReviewPhaseAwaitingReview,
	}

	review := ghReview{
		State: "commented",
		Body:  "Some feedback",
	}
	review.User.Login = "copilot-pull-request-reviewer"

	pr := ghPullRequest{}

	err := p.handleAIReview(loop, review, pr)
	require.NoError(t, err)

	// Should NOT save or change phase.
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestHandlePRSynchronize(t *testing.T) {
	p, api, store, _ := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseCursorFixing,
		Iteration:     2,
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
	}

	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}

	pr := ghPullRequest{}
	pr.Head.SHA = "newsha123"

	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseAwaitingReview &&
			l.LastCommitSHA == "newsha123"
	})).Return(nil)

	// Inline status update (replaces old thread notification).
	mockInlineStatusUpdate(store, api, "agent-1", agentRecord)

	err := p.handlePRSynchronize(loop, pr)
	require.NoError(t, err)
	store.AssertExpectations(t)
}

func TestCollectReviewFeedback(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:       "loop-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 42,
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("main.go"),
			Line:     github.Ptr(10),
			Body:     github.Ptr("Missing error check"),
			CommitID: github.Ptr("abc123"),
		},
		{
			User:     &github.User{Login: github.Ptr("human-reviewer")},
			Path:     github.Ptr("main.go"),
			Line:     github.Ptr(20),
			Body:     github.Ptr("This is fine"),
			CommitID: github.Ptr("abc123"),
		},
		{
			User:     &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
			Path:     github.Ptr("util.go"),
			Line:     github.Ptr(5),
			Body:     github.Ptr("Consider using errors.Is()"),
			CommitID: github.Ptr("abc123"),
		},
	}, nil)

	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	// Should include coderabbitai and copilot comments, but NOT human-reviewer.
	assert.Contains(t, feedback, "main.go:10")
	assert.Contains(t, feedback, "Missing error check")
	assert.Contains(t, feedback, "util.go:5")
	assert.Contains(t, feedback, "errors.Is()")
	assert.NotContains(t, feedback, "This is fine")
}

func TestCollectReviewFeedback_FiltersByCommitSHA(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		LastCommitSHA: "newsha456",
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("old.go"),
			Line:     github.Ptr(1),
			Body:     github.Ptr("Stale comment on old commit"),
			CommitID: github.Ptr("oldsha123"),
		},
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("new.go"),
			Line:     github.Ptr(5),
			Body:     github.Ptr("New issue found"),
			CommitID: github.Ptr("newsha456"),
		},
	}, nil)

	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	// Should only include the comment on the latest commit.
	assert.NotContains(t, feedback, "Stale comment")
	assert.Contains(t, feedback, "New issue found")
}

func TestIsAIReviewerBot(t *testing.T) {
	p, _, _, _ := setupReviewLoopTestPlugin(t)

	assert.True(t, p.isAIReviewerBot("coderabbitai[bot]"))
	assert.True(t, p.isAIReviewerBot("CODERABBITAI[BOT]")) // case-insensitive
	assert.True(t, p.isAIReviewerBot("copilot-pull-request-reviewer"))
	assert.False(t, p.isAIReviewerBot("human-reviewer"))
	assert.False(t, p.isAIReviewerBot(""))
}

func TestPublishReviewLoopChange(t *testing.T) {
	p, api, _, _ := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     2,
		PRURL:         "https://github.com/org/repo/pull/42",
		UserID:        "user-1",
		UpdatedAt:     1234567890,
	}

	api.On("PublishWebSocketEvent",
		"review_loop_changed",
		mock.MatchedBy(func(data map[string]any) bool {
			return data["review_loop_id"] == "loop-1" &&
				data["agent_record_id"] == "agent-1" &&
				data["phase"] == kvstore.ReviewPhaseAwaitingReview &&
				data["iteration"] == "2" &&
				data["pr_url"] == "https://github.com/org/repo/pull/42" &&
				data["updated_at"] == "1234567890"
		}),
		mock.MatchedBy(func(broadcast *model.WebsocketBroadcast) bool {
			return broadcast.UserId == "user-1"
		}),
	).Return()

	p.publishReviewLoopChange(loop)
	api.AssertExpectations(t)
}

func TestHandleHumanReviewApproval(t *testing.T) {
	p, api, store, _ := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseHumanReview,
		Iteration:     2,
		TriggerPostID: "trigger-1",
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
		PRURL:         "https://github.com/org/repo/pull/42",
	}

	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}

	// SaveReviewLoop for complete transition.
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseComplete
	})).Return(nil)

	// Inline status update.
	mockInlineStatusUpdate(store, api, "agent-1", agentRecord)

	// Completion post.
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-1" && hasAttachmentWithTitle(post, "approved")
	})).Return(&model.Post{Id: "notif-1"}, nil)

	// Rocket reaction.
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "rocket"
	})).Return(nil, nil)

	// WebSocket event.
	api.On("PublishWebSocketEvent", "review_loop_changed", mock.Anything, mock.Anything).Return()

	err := p.handleHumanReviewApproval(loop, "testuser")
	require.NoError(t, err)

	// Verify phase was set to complete.
	assert.Equal(t, kvstore.ReviewPhaseComplete, loop.Phase)

	// Verify history has a new event with complete phase and reviewer name.
	require.NotEmpty(t, loop.History)
	lastEvent := loop.History[len(loop.History)-1]
	assert.Equal(t, kvstore.ReviewPhaseComplete, lastEvent.Phase)
	assert.Contains(t, lastEvent.Detail, "testuser")

	store.AssertExpectations(t)
	api.AssertExpectations(t)
}

// --- ensureReviewLoop tests ---

func TestEnsureReviewLoop_ExistingLoop(t *testing.T) {
	p, _, store, _ := setupReviewLoopTestPlugin(t)

	existingLoop := &kvstore.ReviewLoop{
		ID:    "existing-loop-1",
		PRURL: "https://github.com/org/repo/pull/42",
		Phase: kvstore.ReviewPhaseAwaitingReview,
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(existingLoop, nil)

	result := p.ensureReviewLoop("https://github.com/org/repo/pull/42")
	require.NotNil(t, result)
	assert.Equal(t, "existing-loop-1", result.ID)

	// Should NOT have tried to look up an agent or start a new loop.
	store.AssertNotCalled(t, "GetAgentByPRURL")
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestEnsureReviewLoop_BootstrapsFromAgent(t *testing.T) {
	p, api, store, ghMock := setupReviewLoopTestPlugin(t)

	agent := &kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		UserID:         "user-1",
		ChannelID:      "ch-1",
		PostID:         "root-1",
		TriggerPostID:  "trigger-1",
		BotReplyPostID: "reply-1",
		PrURL:          "https://github.com/org/repo/pull/42",
		Status:         "FINISHED",
		Repository:     "org/repo",
	}

	// Call 1: ensureReviewLoop checks for existing loop -> nil.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil).Once()

	// Agent lookup by PR URL.
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(agent, nil)

	// Call 2: startReviewLoop idempotency check -> nil.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil).Once()

	// startReviewLoop mocks.
	store.On("GetWorkflowByAgent", "agent-1").Return("", nil)
	store.On("SaveReviewLoop", mock.MatchedBy(func(loop *kvstore.ReviewLoop) bool {
		return loop.AgentRecordID == "agent-1" && loop.Owner == "org" && loop.Repo == "repo"
	})).Return(nil)
	ghMock.On("MarkPRReadyForReview", mock.Anything, "org", "repo", 42).Return(nil)
	ghMock.On("RequestReviewers", mock.Anything, "org", "repo", 42, mock.Anything).Return(nil)
	mockInlineStatusUpdate(store, api, "agent-1", agent)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-1" && r.EmojiName == "eyes"
	})).Return(nil, nil)

	// Call 3: ensureReviewLoop refetches the freshly-created loop.
	createdLoop := &kvstore.ReviewLoop{
		ID:            "new-loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		PRURL:         "https://github.com/org/repo/pull/42",
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(createdLoop, nil).Once()

	result := p.ensureReviewLoop("https://github.com/org/repo/pull/42")
	require.NotNil(t, result)
	assert.Equal(t, "new-loop-1", result.ID)
	assert.Equal(t, kvstore.ReviewPhaseAwaitingReview, result.Phase)

	store.AssertExpectations(t)
	ghMock.AssertExpectations(t)
}

func TestEnsureReviewLoop_SkipsRunningAgent(t *testing.T) {
	p, _, store, _ := setupReviewLoopTestPlugin(t)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PrURL:         "https://github.com/org/repo/pull/42",
		Status:        "RUNNING", // Not terminal.
	}

	// No existing loop.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(agent, nil)

	result := p.ensureReviewLoop("https://github.com/org/repo/pull/42")
	assert.Nil(t, result)

	// Should NOT have called startReviewLoop (SaveReviewLoop).
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestEnsureReviewLoop_DisabledConfig(t *testing.T) {
	p, _, store, _ := setupReviewLoopTestPlugin(t)
	p.configuration.EnableAIReviewLoop = false // Disabled.

	// No existing loop.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)

	result := p.ensureReviewLoop("https://github.com/org/repo/pull/42")
	assert.Nil(t, result)

	// Should NOT have tried to look up an agent.
	store.AssertNotCalled(t, "GetAgentByPRURL")
}

func TestEnsureReviewLoop_NoGitHubClient(t *testing.T) {
	p, _, store, _ := setupReviewLoopTestPlugin(t)
	p.githubClient = nil // No GitHub client.

	// No existing loop.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)

	result := p.ensureReviewLoop("https://github.com/org/repo/pull/42")
	assert.Nil(t, result)

	// Should NOT have tried to look up an agent.
	store.AssertNotCalled(t, "GetAgentByPRURL")
}

func TestEnsureReviewLoop_NoAgentFound(t *testing.T) {
	p, _, store, _ := setupReviewLoopTestPlugin(t)

	// No existing loop.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)

	result := p.ensureReviewLoop("https://github.com/org/repo/pull/42")
	assert.Nil(t, result)

	// Should NOT have called startReviewLoop.
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestEnsureReviewLoop_Idempotent(t *testing.T) {
	p, _, store, _ := setupReviewLoopTestPlugin(t)

	existingLoop := &kvstore.ReviewLoop{
		ID:    "loop-1",
		PRURL: "https://github.com/org/repo/pull/42",
		Phase: kvstore.ReviewPhaseCursorFixing,
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(existingLoop, nil)

	// Call twice -- should return the same loop both times without side effects.
	result1 := p.ensureReviewLoop("https://github.com/org/repo/pull/42")
	result2 := p.ensureReviewLoop("https://github.com/org/repo/pull/42")

	assert.Equal(t, result1.ID, result2.ID)
	store.AssertNotCalled(t, "GetAgentByPRURL")
	store.AssertNotCalled(t, "SaveReviewLoop")
}
