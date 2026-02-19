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

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
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

func (m *mockGitHubClient) ListIssueComments(ctx context.Context, owner, repo string, issueNumber int) ([]*github.IssueComment, error) {
	args := m.Called(ctx, owner, repo, issueNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*github.IssueComment), args.Error(1)
}

func (m *mockGitHubClient) ReplyToReviewComment(ctx context.Context, owner, repo string, prNumber int, commentID int64, body string) (*github.PullRequestComment, error) {
	args := m.Called(ctx, owner, repo, prNumber, commentID, body)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*github.PullRequestComment), args.Error(1)
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
	api.On(
		"LogError",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Maybe()

	// Add LogWarn mock for non-fatal warnings (reviewer request failures, etc.).
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On(
		"LogWarn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Add LogInfo mock.
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Support candidate-drop debug logs emitted by review feedback bundle tests.
	api.On(
		"LogDebug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything,
	).Maybe()

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

func collectDroppedCandidateLogs(api *mockPluginAPI) []map[string]any {
	logs := []map[string]any{}
	for _, call := range api.Calls {
		if call.Method != "LogDebug" || len(call.Arguments) == 0 {
			continue
		}
		message, ok := call.Arguments.Get(0).(string)
		if !ok || message != "Review feedback candidate dropped" {
			continue
		}

		fields := map[string]any{}
		for i := 1; i+1 < len(call.Arguments); i += 2 {
			key, ok := call.Arguments.Get(i).(string)
			if !ok {
				continue
			}
			fields[key] = call.Arguments.Get(i + 1)
		}
		logs = append(logs, fields)
	}
	return logs
}

func hasDroppedCandidateLog(logs []map[string]any, dropReason string, route reviewerExtractionRoute) bool {
	for _, fields := range logs {
		if fmt.Sprint(fields["drop_reason"]) == dropReason &&
			fmt.Sprint(fields["extraction_route"]) == string(route) {
			return true
		}
	}
	return false
}

func assertDroppedCandidateLogRequiredFields(t *testing.T, fields map[string]any) {
	t.Helper()

	requiredFields := []string{
		"review_loop_id",
		"agent_record_id",
		"phase",
		"iteration",
		"pr_url",
		"extraction_route",
		"drop_reason",
		"candidate_source_type",
		"candidate_source_id",
		"candidate_reviewer_login",
		"candidate_reviewer_type",
		"candidate_path",
		"candidate_line",
		"candidate_commit_sha",
		"candidate_raw_text_len",
		"candidate_normalized_text_len",
	}
	for _, field := range requiredFields {
		assert.Containsf(t, fields, field, "missing required dropped-candidate log field: %s", field)
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
	cursorMock := p.cursorClient.(*mockCursorClient)

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
	pr.Head.Ref = "cursor/fix-review-loop"
	pr.Head.SHA = "abc123"

	// collectReviewFeedback calls.
	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("main.go"),
			Line:     github.Ptr(42),
			Body:     github.Ptr("Prompt for AI Agents\nPotential nil pointer here"),
			CommitID: github.Ptr("abc123"),
		},
	}, nil)
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)

	cursorMock.On("AddFollowup", mock.Anything, "agent-1", mock.MatchedBy(func(req cursor.FollowupRequest) bool {
		return strings.Contains(req.Prompt.Text, "Potential nil pointer here") &&
			strings.Contains(req.Prompt.Text, "do not create a new pull request") &&
			strings.Contains(req.Prompt.Text, "pull_request_url: https://github.com/org/repo/pull/42")
	})).Return(&cursor.FollowupResponse{ID: "agent-1"}, nil)

	// SaveReviewLoop for cursor_fixing transition.
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseCursorFixing && l.Iteration == 2
	})).Return(nil)

	// Inline status update (replaces old thread notification).
	mockInlineStatusUpdate(store, api, "agent-1", agentRecord)

	err := p.handleAIReview(loop, review, pr)
	require.NoError(t, err)
	require.NotEmpty(t, loop.Findings)
	require.NotEmpty(t, loop.History)
	assert.NotZero(t, loop.LastFeedbackDispatchAt)
	assert.Equal(t, "abc123", loop.LastFeedbackDispatchSHA)
	assert.NotEmpty(t, loop.LastFeedbackDigest)
	assert.Equal(t, kvstore.ReviewPhaseCursorFixing, loop.History[len(loop.History)-1].Phase)
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "direct follow-up dispatched")
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "1 new, 0 repeated, 0 dismissed")
	ghMock.AssertExpectations(t)
	cursorMock.AssertExpectations(t)
}

func TestDispatchReviewFeedback_IdempotentSkip(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)
	cursorMock := p.cursorClient.(*mockCursorClient)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-idempotent",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     2,
		PRURL:         "https://github.com/org/repo/pull/42",
		LastCommitSHA: "sha-1",
	}

	pr := ghPullRequest{}
	pr.Head.Ref = "cursor/fix-branch"
	pr.Head.SHA = "sha-1"

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("server/api.go"),
			Line:     github.Ptr(14),
			Body:     github.Ptr("Prompt for AI Agents\nAdd a nil guard before dereferencing."),
			CommitID: github.Ptr("sha-1"),
		},
	}, nil).Twice()
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil).Twice()
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil).Twice()

	cursorMock.On("AddFollowup", mock.Anything, "agent-1", mock.Anything).
		Return(&cursor.FollowupResponse{ID: "agent-1"}, nil).Once()

	firstOutcome, err := p.dispatchReviewFeedback(loop, pr)
	require.NoError(t, err)
	require.True(t, firstOutcome.Dispatched)
	assert.Equal(t, reviewDispatchModeDirect, firstOutcome.Mode)
	assert.Equal(t, 1, firstOutcome.Counts.New)
	assert.Equal(t, 0, firstOutcome.Counts.Repeated)
	assert.Equal(t, 0, firstOutcome.Counts.Dismissed)
	require.NotEmpty(t, loop.LastFeedbackDigest)
	assert.Equal(t, "sha-1", loop.LastFeedbackDispatchSHA)

	secondOutcome, err := p.dispatchReviewFeedback(loop, pr)
	require.NoError(t, err)
	require.True(t, secondOutcome.Skipped)
	assert.Equal(t, reviewDispatchModeSkippedIdempotent, secondOutcome.Mode)
	assert.Equal(t, 0, secondOutcome.Counts.New)
	assert.Equal(t, 1, secondOutcome.Counts.Repeated)
	assert.Equal(t, 0, secondOutcome.Counts.Dismissed)
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "Skipped duplicate")
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "0 new, 1 repeated, 0 dismissed")

	cursorMock.AssertNumberOfCalls(t, "AddFollowup", 1)
	ghMock.AssertNotCalled(t, "CreateComment", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestDispatchReviewFeedback_DifferentSHASameDigestDispatchesAgain(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)
	cursorMock := p.cursorClient.(*mockCursorClient)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-sha-replay",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     2,
		PRURL:         "https://github.com/org/repo/pull/42",
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User: &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path: github.Ptr("server/api.go"),
			Line: github.Ptr(44),
			Body: github.Ptr("Prompt for AI Agents\nAdd a nil guard before dereferencing the request body."),
		},
	}, nil).Twice()
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil).Twice()
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil).Twice()

	cursorMock.On("AddFollowup", mock.Anything, "agent-1", mock.Anything).
		Return(&cursor.FollowupResponse{ID: "agent-1"}, nil).Twice()

	pr1 := ghPullRequest{}
	pr1.Head.Ref = "cursor/fix-review-loop"
	pr1.Head.SHA = "sha-1"

	firstOutcome, err := p.dispatchReviewFeedback(loop, pr1)
	require.NoError(t, err)
	require.True(t, firstOutcome.Dispatched)
	require.False(t, firstOutcome.Skipped)
	assert.Equal(t, reviewDispatchModeDirect, firstOutcome.Mode)
	assert.Equal(t, "sha-1", loop.LastFeedbackDispatchSHA)
	require.NotEmpty(t, loop.LastFeedbackDigest)
	firstDigest := loop.LastFeedbackDigest

	pr2 := ghPullRequest{}
	pr2.Head.Ref = "cursor/fix-review-loop"
	pr2.Head.SHA = "sha-2"

	secondOutcome, err := p.dispatchReviewFeedback(loop, pr2)
	require.NoError(t, err)
	require.True(t, secondOutcome.Dispatched)
	require.False(t, secondOutcome.Skipped)
	assert.Equal(t, reviewDispatchModeDirect, secondOutcome.Mode)
	assert.Equal(t, "sha-2", loop.LastFeedbackDispatchSHA)
	// Same findings should keep the digest stable while SHA changes.
	assert.Equal(t, firstDigest, loop.LastFeedbackDigest)

	cursorMock.AssertNumberOfCalls(t, "AddFollowup", 2)
	ghMock.AssertNotCalled(t, "CreateComment", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestDispatchReviewFeedback_FailsFastOnDirectFailure(t *testing.T) {
	p, api, _, ghMock := setupReviewLoopTestPlugin(t)
	cursorMock := p.cursorClient.(*mockCursorClient)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-direct-fail",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseHumanReview,
		Iteration:     3,
		PRURL:         "https://github.com/org/repo/pull/42",
		LastCommitSHA: "sha-fallback",
	}

	pr := ghPullRequest{}
	pr.Head.Ref = "cursor/fix-branch"
	pr.Head.SHA = "sha-fallback"

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("humandev")},
			Path:     github.Ptr("server/webhook.go"),
			Line:     github.Ptr(70),
			Body:     github.Ptr("Please add a nil check around payload parsing."),
			CommitID: github.Ptr("sha-fallback"),
		},
	}, nil)
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)

	cursorMock.On("AddFollowup", mock.Anything, "agent-1", mock.Anything).
		Return(nil, fmt.Errorf("agent is not running")).Once()

	outcome, err := p.dispatchReviewFeedback(loop, pr)
	require.NoError(t, err)
	require.True(t, outcome.Failed)
	assert.Equal(t, reviewDispatchModeFailed, outcome.Mode)
	assert.Equal(t, 1, outcome.Counts.New)
	assert.Equal(t, 0, outcome.Counts.Repeated)
	assert.Equal(t, 0, outcome.Counts.Dismissed)
	assert.Zero(t, loop.LastFeedbackDispatchAt)
	assert.Empty(t, loop.LastFeedbackDispatchSHA)
	assert.Empty(t, loop.LastFeedbackDigest)
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "manual intervention")
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "1 new, 0 repeated, 0 dismissed")
	api.AssertCalled(t, "LogError",
		"Review feedback dispatch decision",
		"review_loop_id", "loop-direct-fail",
		"dispatch_mode", reviewDispatchModeFailed,
		"decision_reason", reviewDispatchReasonAddFollowupError,
		"iteration", 3,
		"dispatch_sha", "sha-fallback",
		"dispatch_digest", mock.Anything,
		"new_count", 1,
		"repeated_count", 0,
		"dismissed_count", 0,
		"dispatchable_count", 1,
		"error_primary", "agent is not running",
	)

	cursorMock.AssertExpectations(t)
	ghMock.AssertNotCalled(t, "CreateComment", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	ghMock.AssertExpectations(t)
}

func TestDispatchReviewFeedback_FailsWhenCursorClientMissing(t *testing.T) {
	p, api, _, ghMock := setupReviewLoopTestPlugin(t)
	p.cursorClient = nil

	loop := &kvstore.ReviewLoop{
		ID:            "loop-no-cursor-client",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseHumanReview,
		Iteration:     1,
		PRURL:         "https://github.com/org/repo/pull/42",
		LastCommitSHA: "sha-fail",
	}

	pr := ghPullRequest{}
	pr.Head.SHA = "sha-fail"

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("humandev")},
			Path:     github.Ptr("server/reviewloop.go"),
			Line:     github.Ptr(120),
			Body:     github.Ptr("Please simplify this control flow."),
			CommitID: github.Ptr("sha-fail"),
		},
	}, nil)
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)

	outcome, err := p.dispatchReviewFeedback(loop, pr)
	require.NoError(t, err)
	require.True(t, outcome.Failed)
	assert.Equal(t, reviewDispatchModeFailed, outcome.Mode)
	assert.Equal(t, 1, outcome.Counts.New)
	assert.Equal(t, 0, outcome.Counts.Repeated)
	assert.Equal(t, 0, outcome.Counts.Dismissed)
	assert.Zero(t, loop.LastFeedbackDispatchAt)
	assert.Empty(t, loop.LastFeedbackDigest)
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "manual intervention")
	assert.Contains(t, loop.History[len(loop.History)-1].Detail, "1 new, 0 repeated, 0 dismissed")
	api.AssertCalled(t, "LogError",
		"Review feedback dispatch decision",
		"review_loop_id", "loop-no-cursor-client",
		"dispatch_mode", reviewDispatchModeFailed,
		"decision_reason", reviewDispatchReasonCursorClientNil,
		"iteration", 1,
		"dispatch_sha", "sha-fail",
		"dispatch_digest", mock.Anything,
		"new_count", 1,
		"repeated_count", 0,
		"dismissed_count", 0,
		"dispatchable_count", 1,
		"error_primary", "cursor client is not configured",
	)

	ghMock.AssertNotCalled(t, "CreateComment", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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

func TestCollectReviewFeedbackPipeline_AwaitingReview_IncludesAIBotSources(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:       "loop-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 42,
		Phase:    kvstore.ReviewPhaseAwaitingReview,
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("main.go"),
			Line:     github.Ptr(10),
			Body:     github.Ptr("Prompt for AI Agents\nMissing error check"),
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

	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{
		{
			User: &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Body: github.Ptr("Actionable comments posted: 1\n\nPrompt for all review comments with AI agents\n```txt\nUse context.WithTimeout in API requests.\n```\n\nmeta"),
		},
	}, nil)
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{
		{
			User: &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
			Body: github.Ptr("Add unit tests for this edge case."),
		},
		{
			User: &github.User{Login: github.Ptr("human-reviewer")},
			Body: github.Ptr("Human-only note"),
		},
	}, nil)

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	// Should include AI bot findings from inline and CodeRabbit review-body sources.
	assert.Contains(t, feedback, "main.go:10")
	assert.Contains(t, feedback, "Missing error check")
	assert.Contains(t, feedback, "util.go:5")
	assert.Contains(t, feedback, "errors.Is()")
	assert.Contains(t, feedback, "context.WithTimeout")
	assert.NotContains(t, feedback, "Add unit tests for this edge case")

	// Human review comments are ignored in awaiting_review.
	assert.NotContains(t, feedback, "This is fine")
	assert.NotContains(t, feedback, "Human-only note")
}

func TestCollectReviewFeedbackPipeline_FiltersByCommitSHA(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		LastCommitSHA: "newsha456",
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("old.go"),
			Line:     github.Ptr(1),
			Body:     github.Ptr("Prompt for AI Agents\nStale comment on old commit"),
			CommitID: github.Ptr("oldsha123"),
		},
		{
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("new.go"),
			Line:     github.Ptr(5),
			Body:     github.Ptr("Prompt for AI Agents\nNew issue found"),
			CommitID: github.Ptr("newsha456"),
		},
	}, nil)

	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	// Should only include the comment on the latest commit.
	assert.NotContains(t, feedback, "Stale comment")
	assert.Contains(t, feedback, "New issue found")
}

func TestCollectReviewFeedbackPipeline_HumanReview_ExcludesAIBots(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:       "loop-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 42,
		Phase:    kvstore.ReviewPhaseHumanReview,
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User: &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Body: github.Ptr("AI finding should be ignored in human phase"),
			Path: github.Ptr("ai.go"),
			Line: github.Ptr(1),
		},
		{
			User: &github.User{Login: github.Ptr("human-reviewer")},
			Body: github.Ptr("Please add a nil guard."),
			Path: github.Ptr("human.go"),
			Line: github.Ptr(7),
		},
	}, nil)

	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{
		{
			User: &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Body: github.Ptr("Actionable comments posted: 2"),
		},
		{
			User: &github.User{Login: github.Ptr("human-reviewer")},
			Body: github.Ptr("Also add tests for this path."),
		},
	}, nil)

	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{
		{
			User: &github.User{Login: github.Ptr("human-reviewer")},
			Body: github.Ptr("Confirm behavior for empty input."),
		},
		{
			User: &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
			Body: github.Ptr("AI issue comment should be ignored here."),
		},
	}, nil)

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)
	assert.Contains(t, feedback, "human.go:7")
	assert.Contains(t, feedback, "Please add a nil guard")
	assert.NotContains(t, feedback, "Also add tests for this path")
	assert.NotContains(t, feedback, "Confirm behavior for empty input")

	assert.NotContains(t, feedback, "AI finding should be ignored")
	assert.NotContains(t, feedback, "AI issue comment should be ignored")
}

func TestCollectReviewFeedbackPipeline_HumanReview_SkipsCursorRelayIssueComments(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)

	loop := &kvstore.ReviewLoop{
		ID:       "loop-1",
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 42,
		Phase:    kvstore.ReviewPhaseHumanReview,
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{}, nil)
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{
		{
			User: &github.User{Login: github.Ptr("human-reviewer")},
			Body: github.Ptr("@cursor Please address the following review feedback:\n\n1. **server/api.go:9** - stale relay item"),
		},
		{
			User: &github.User{Login: github.Ptr("human-reviewer")},
			Body: github.Ptr("Please add a timeout on this request path."),
		},
	}, nil)

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	assert.Empty(t, feedback)
	assert.NotContains(t, feedback, "stale relay item")
	assert.NotContains(t, feedback, "Please add a timeout on this request path")
	assert.NotContains(t, feedback, "@cursor Please address")
}

func TestCollectReviewFeedbackBundle_LogsDroppedCandidatesWithReasonAndRoute(t *testing.T) {
	cases := []struct {
		name           string
		expectedRoute  reviewerExtractionRoute
		expectedReason string
		setupGitHub    func(ghMock *mockGitHubClient)
	}{
		{
			name:           "coderabbit missing prompt markers",
			expectedRoute:  reviewerExtractionRouteCodeRabbit,
			expectedReason: reviewerExtractionDropReasonCodeRabbitMarkersMissing,
			setupGitHub: func(ghMock *mockGitHubClient) {
				ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
					{
						ID:       github.Ptr(int64(101)),
						User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
						Path:     github.Ptr("server/api.go"),
						Line:     github.Ptr(11),
						Body:     github.Ptr("Please add retry handling."),
						CommitID: github.Ptr("sha-101"),
					},
				}, nil)
				ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
				ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)
			},
		},
		{
			name:           "non-coderabbit non-inline source dropped",
			expectedRoute:  reviewerExtractionRouteNonCodeRabbit,
			expectedReason: reviewerExtractionDropReasonNonCodeRabbitNonInlineSource,
			setupGitHub: func(ghMock *mockGitHubClient) {
				ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{}, nil)
				ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{
					{
						ID:   github.Ptr(int64(201)),
						User: &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
						Body: github.Ptr("Please add integration tests for timeout handling."),
					},
				}, nil)
				ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)
			},
		},
		{
			name:           "normalized text empty dropped",
			expectedRoute:  reviewerExtractionRouteNonCodeRabbit,
			expectedReason: reviewerExtractionDropReasonNormalizedEmpty,
			setupGitHub: func(ghMock *mockGitHubClient) {
				ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
					{
						ID:       github.Ptr(int64(301)),
						User:     &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
						Path:     github.Ptr("server/webhook.go"),
						Line:     github.Ptr(9),
						Body:     github.Ptr(" \n\t "),
						CommitID: github.Ptr("sha-301"),
					},
				}, nil)
				ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
				ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)
			},
		},
		{
			name:           "actionable text empty dropped",
			expectedRoute:  reviewerExtractionRouteNonCodeRabbit,
			expectedReason: reviewerExtractionDropReasonActionableEmpty,
			setupGitHub: func(ghMock *mockGitHubClient) {
				ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
					{
						ID:       github.Ptr(int64(401)),
						User:     &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
						Path:     github.Ptr("server/reviewloop.go"),
						Line:     github.Ptr(77),
						Body:     github.Ptr("LGTM"),
						CommitID: github.Ptr("sha-401"),
					},
				}, nil)
				ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
				ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, api, _, ghMock := setupReviewLoopTestPlugin(t)
			p.configuration.EnableDebugLogging = true

			loop := &kvstore.ReviewLoop{
				ID:            "loop-1",
				AgentRecordID: "agent-1",
				Owner:         "org",
				Repo:          "repo",
				PRNumber:      42,
				Phase:         kvstore.ReviewPhaseAwaitingReview,
				Iteration:     2,
				PRURL:         "https://github.com/org/repo/pull/42",
			}

			tc.setupGitHub(ghMock)

			classification, _, feedback, err := p.collectReviewFeedbackBundle(loop)
			require.NoError(t, err)
			assert.Empty(t, classification.Dispatchable)
			assert.Empty(t, feedback)

			droppedLogs := collectDroppedCandidateLogs(api)
			require.Len(t, droppedLogs, 1)

			fields := droppedLogs[0]
			assertDroppedCandidateLogRequiredFields(t, fields)
			assert.Equal(t, loop.ID, fields["review_loop_id"])
			assert.Equal(t, loop.AgentRecordID, fields["agent_record_id"])
			assert.Equal(t, loop.Phase, fields["phase"])
			assert.Equal(t, loop.Iteration, fields["iteration"])
			assert.Equal(t, loop.PRURL, fields["pr_url"])
			assert.Equal(t, tc.expectedReason, fmt.Sprint(fields["drop_reason"]))
			assert.Equal(t, string(tc.expectedRoute), fmt.Sprint(fields["extraction_route"]))

			ghMock.AssertExpectations(t)
		})
	}
}

func TestCollectReviewFeedbackBundle_MixedCandidates_LogsDropsWithoutBehaviorDrift(t *testing.T) {
	p, api, _, ghMock := setupReviewLoopTestPlugin(t)
	p.configuration.EnableDebugLogging = true

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     3,
		PRURL:         "https://github.com/org/repo/pull/42",
	}

	ghMock.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			ID:       github.Ptr(int64(501)),
			User:     &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
			Path:     github.Ptr("server/api.go"),
			Line:     github.Ptr(19),
			Body:     github.Ptr("Please add an explicit nil guard."),
			CommitID: github.Ptr("sha-501"),
		},
		{
			ID:       github.Ptr(int64(502)),
			User:     &github.User{Login: github.Ptr("coderabbitai[bot]")},
			Path:     github.Ptr("server/reviewloop.go"),
			Line:     github.Ptr(65),
			Body:     github.Ptr("Please tighten this logic."),
			CommitID: github.Ptr("sha-502"),
		},
		{
			ID:       github.Ptr(int64(503)),
			User:     &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
			Path:     github.Ptr("server/reviewloop.go"),
			Line:     github.Ptr(66),
			Body:     github.Ptr(" \n "),
			CommitID: github.Ptr("sha-503"),
		},
		{
			ID:       github.Ptr(int64(504)),
			User:     &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
			Path:     github.Ptr("server/reviewloop.go"),
			Line:     github.Ptr(67),
			Body:     github.Ptr("LGTM"),
			CommitID: github.Ptr("sha-504"),
		},
	}, nil)
	ghMock.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{}, nil)
	ghMock.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{
		{
			ID:   github.Ptr(int64(601)),
			User: &github.User{Login: github.Ptr("copilot-pull-request-reviewer")},
			Body: github.Ptr("Please add a changelog entry."),
		},
	}, nil)

	classification, _, feedback, err := p.collectReviewFeedbackBundle(loop)
	require.NoError(t, err)
	require.Len(t, classification.Dispatchable, 1)
	require.Len(t, classification.New, 1)
	assert.Empty(t, classification.Repeated)
	assert.Empty(t, classification.Resolved)
	assert.Empty(t, classification.Superseded)
	assert.Contains(t, feedback, "Please add an explicit nil guard.")
	assert.NotContains(t, feedback, "Please tighten this logic.")
	assert.NotContains(t, feedback, "LGTM")
	assert.NotContains(t, feedback, "Please add a changelog entry.")

	droppedLogs := collectDroppedCandidateLogs(api)
	require.Len(t, droppedLogs, 4)
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonCodeRabbitMarkersMissing, reviewerExtractionRouteCodeRabbit))
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonNormalizedEmpty, reviewerExtractionRouteNonCodeRabbit))
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonActionableEmpty, reviewerExtractionRouteNonCodeRabbit))
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonNonCodeRabbitNonInlineSource, reviewerExtractionRouteNonCodeRabbit))

	ghMock.AssertExpectations(t)
}

func TestResolveReviewerExtractionRoute_CodeRabbit(t *testing.T) {
	cases := []string{
		"coderabbitai[bot]",
		"CodeRabbitAI[Bot]",
		"  CODERABBITAI[BOT]  ",
	}

	for _, reviewerLogin := range cases {
		candidate := reviewFeedbackCandidate{ReviewerLogin: reviewerLogin}
		assert.Equal(t, reviewerExtractionRouteCodeRabbit, resolveReviewerExtractionRoute(candidate))
	}
}

func TestResolveReviewerExtractionRoute_NonCodeRabbit(t *testing.T) {
	cases := []string{
		"human-reviewer",
		"copilot-pull-request-reviewer",
		"",
		"  ",
	}

	for _, reviewerLogin := range cases {
		candidate := reviewFeedbackCandidate{ReviewerLogin: reviewerLogin}
		assert.Equal(t, reviewerExtractionRouteNonCodeRabbit, resolveReviewerExtractionRoute(candidate))
	}
}

func TestExtractCandidateActionableText_CodeRabbitReviewComment_ExtractsPromptForAIAgentsOnly(t *testing.T) {
	candidate := reviewFeedbackCandidate{
		ReviewerLogin: "coderabbitai[bot]",
		SourceType:    "review_comment",
		NormalizedText: `## Summary
Actionable comments posted: 2

Prompt for AI Agents
Please guard against nil pointer dereference in buildPayload.
Add regression coverage for nil input.

## Walkthrough
- inspected latest patch set`,
	}

	actionable, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, "Please guard against nil pointer dereference in buildPayload.\nAdd regression coverage for nil input.", actionable)
	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
}

func TestExtractCandidateActionableText_CodeRabbitReviewBody_ExtractsPromptForAllReviewCommentsOnly(t *testing.T) {
	candidate := reviewFeedbackCandidate{
		ReviewerLogin: "coderabbitai[bot]",
		SourceType:    "review_body",
		NormalizedText: `## Summary
Actionable comments posted: 1

Prompt for all review comments with AI agents
Use strict severity labels.
Confirm behavior for empty payloads.

## Changes
metadata that should not be included`,
	}

	actionable, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, "Use strict severity labels.\nConfirm behavior for empty payloads.", actionable)
	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
}

func TestExtractCandidateActionableText_CodeRabbit_MarkerMissingReturnsEmpty(t *testing.T) {
	candidate := reviewFeedbackCandidate{
		ReviewerLogin:  "coderabbitai[bot]",
		SourceType:     "review_comment",
		NormalizedText: "Please guard against nil pointer dereference in buildPayload.",
	}

	actionable, route, dropReason := extractCandidateActionableText(candidate)

	assert.Empty(t, actionable)
	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Equal(t, reviewerExtractionDropReasonCodeRabbitMarkersMissing, dropReason)
}

func TestExtractCandidateActionableText_CodeRabbit_StripsVerifyBoilerplateAfterExtraction(t *testing.T) {
	candidate := reviewFeedbackCandidate{
		ReviewerLogin: "coderabbitai[bot]",
		SourceType:    "review_body",
		NormalizedText: `Prompt for all review comments with AI agents
Verify each finding against the current code...
Do not assume old snippets are still present.

Please guard against nil pointer dereference in buildPayload.`,
	}

	actionable, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, "Please guard against nil pointer dereference in buildPayload.", actionable)
	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
}

func TestExtractCandidateActionableText_CodeRabbit_StopsAtVerifyBoundary(t *testing.T) {
	candidate := reviewFeedbackCandidate{
		ReviewerLogin: "coderabbitai[bot]",
		SourceType:    "review_body",
		NormalizedText: `Prompt for all review comments with AI agents
Use strict severity labels.

Verify each finding against the current code...
Do not assume old snippets are still present.

Please guard against nil pointer dereference in buildPayload.`,
	}

	actionable, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, "Use strict severity labels.", actionable)
	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
}

func TestExtractCandidateActionableText_CodeRabbit_ToleratesMarkdownWrappedMarkerLine(t *testing.T) {
	candidate := reviewFeedbackCandidate{
		ReviewerLogin: "coderabbitai[bot]",
		SourceType:    "review_comment",
		NormalizedText: `## **Prompt for AI Agents:**
Please add test coverage for invalid token handling.

### Summary
non-actionable metadata`,
	}

	actionable, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, "Please add test coverage for invalid token handling.", actionable)
	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
}

func TestExtractCandidateActionableText_NonCodeRabbitInlinePassThrough_ReviewerParity(t *testing.T) {
	reviewerLogins := []string{
		"copilot-pull-request-reviewer",
		"human-reviewer",
		"some-unknown-ai-bot",
	}

	input := "  Keep this inline suggestion.\n\n\n\nAdd focused coverage for empty input.\n  "
	expected := "Keep this inline suggestion.\n\nAdd focused coverage for empty input."

	results := make([]string, 0, len(reviewerLogins))
	for _, reviewerLogin := range reviewerLogins {
		candidate := reviewFeedbackCandidate{
			ReviewerLogin:  reviewerLogin,
			SourceType:     "review_comment",
			NormalizedText: input,
		}

		actionable, route, dropReason := extractCandidateActionableText(candidate)

		assert.Equal(t, reviewerExtractionRouteNonCodeRabbit, route)
		assert.Empty(t, dropReason)
		assert.Equal(t, expected, actionable)
		results = append(results, actionable)
	}

	for i := 1; i < len(results); i++ {
		assert.Equal(t, results[0], results[i])
	}
}

func TestExtractCandidateActionableText_NonCodeRabbit_NonInlineSourcesDropped(t *testing.T) {
	sourceTypes := []string{
		"review_body",
		"issue_comment",
		"discussion_comment",
	}

	for _, sourceType := range sourceTypes {
		t.Run(sourceType, func(t *testing.T) {
			candidate := reviewFeedbackCandidate{
				ReviewerLogin:  "human-reviewer",
				SourceType:     sourceType,
				NormalizedText: "Please add input validation.",
			}

			actionable, route, dropReason := extractCandidateActionableText(candidate)

			assert.Empty(t, actionable)
			assert.Equal(t, reviewerExtractionRouteNonCodeRabbit, route)
			assert.Equal(t, reviewerExtractionDropReasonNonCodeRabbitNonInlineSource, dropReason)
		})
	}
}

func TestFormatReviewFeedbackCountSummary(t *testing.T) {
	assert.Equal(t, "3 new, 2 repeated, 1 dismissed", formatReviewFeedbackCountSummary(3, 2, 1))
}

func TestSummarizeReviewFeedbackTelemetry(t *testing.T) {
	candidates := []reviewFeedbackCandidate{
		{SourceType: "review_comment", ReviewerType: reviewerTypeAIBot},
		{SourceType: "review_body", ReviewerType: reviewerTypeAIBot},
		{SourceType: "issue_comment", ReviewerType: reviewerTypeHuman},
	}
	classification := reviewFeedbackClassification{
		New:        []kvstore.ReviewFinding{{Key: "n1"}},
		Repeated:   []kvstore.ReviewFinding{{Key: "r1"}, {Key: "r2"}},
		Resolved:   []kvstore.ReviewFinding{{Key: "res1"}},
		Superseded: []kvstore.ReviewFinding{{Key: "sup1"}, {Key: "sup2"}},
		Dispatchable: []kvstore.ReviewFinding{
			{Key: "n1"},
			{Key: "r1"},
			{Key: "r2"},
		},
	}

	telemetry := summarizeReviewFeedbackTelemetry(candidates, classification)
	assert.Equal(t, 3, telemetry.Source.Total)
	assert.Equal(t, 1, telemetry.Source.ReviewComment)
	assert.Equal(t, 1, telemetry.Source.ReviewBody)
	assert.Equal(t, 1, telemetry.Source.IssueComment)
	assert.Equal(t, 2, telemetry.Source.AIBot)
	assert.Equal(t, 1, telemetry.Source.Human)

	assert.Equal(t, 1, telemetry.Counts.New)
	assert.Equal(t, 2, telemetry.Counts.Repeated)
	assert.Equal(t, 1, telemetry.Counts.Resolved)
	assert.Equal(t, 2, telemetry.Counts.Superseded)
	assert.Equal(t, 3, telemetry.Counts.Dismissed)
	assert.Equal(t, 3, telemetry.Counts.Dispatchable)
	assert.Equal(t, telemetry.Counts.Resolved+telemetry.Counts.Superseded, telemetry.Counts.Dismissed)
}

func TestClassifyFeedback_NewRepeatedResolved(t *testing.T) {
	loop := &kvstore.ReviewLoop{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 3,
		Findings: []kvstore.ReviewFinding{
			{
				Key:            buildFindingKey(reviewFeedbackCandidate{Path: "server/api.go", Line: 12, ActionableText: "add nil check"}),
				Status:         findingStatusOpen,
				ReviewerType:   reviewerTypeAIBot,
				Path:           "server/api.go",
				Line:           12,
				ActionableText: "add nil check",
			},
			{
				Key:            buildFindingKey(reviewFeedbackCandidate{Path: "server/webhook.go", Line: 88, ActionableText: "remove dead code"}),
				Status:         findingStatusOpen,
				ReviewerType:   reviewerTypeAIBot,
				Path:           "server/webhook.go",
				Line:           88,
				ActionableText: "remove dead code",
			},
		},
	}

	candidates := []reviewFeedbackCandidate{
		{
			SourceType:     "review_comment",
			ReviewerType:   reviewerTypeAIBot,
			Path:           "server/api.go",
			Line:           12,
			RawText:        "add nil check",
			ActionableText: "add nil check",
		},
		{
			SourceType:     "review_comment",
			ReviewerType:   reviewerTypeAIBot,
			Path:           "server/poller.go",
			Line:           99,
			RawText:        "handle timeout properly",
			ActionableText: "handle timeout properly",
		},
	}

	classification := classifyFeedback(loop, candidates, 1700000000000)
	require.Len(t, classification.New, 1)
	require.Len(t, classification.Repeated, 1)
	require.Len(t, classification.Resolved, 1)
	require.Len(t, classification.Dispatchable, 2)

	assert.Equal(t, "handle timeout properly", classification.New[0].ActionableText)
	assert.Equal(t, "add nil check", classification.Repeated[0].ActionableText)
	assert.Equal(t, "remove dead code", classification.Resolved[0].ActionableText)
}

func TestClassifyFeedback_SupersedesOlderSameLocationInstruction(t *testing.T) {
	loop := &kvstore.ReviewLoop{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 2,
		Findings: []kvstore.ReviewFinding{
			{
				Key:            buildFindingKey(reviewFeedbackCandidate{Path: "server/api.go", Line: 44, ActionableText: "use pointer receiver"}),
				Status:         findingStatusOpen,
				ReviewerType:   reviewerTypeAIBot,
				Path:           "server/api.go",
				Line:           44,
				ActionableText: "use pointer receiver",
			},
		},
	}

	candidates := []reviewFeedbackCandidate{
		{
			SourceType:     "review_comment",
			ReviewerType:   reviewerTypeAIBot,
			Path:           "server/api.go",
			Line:           44,
			RawText:        "use value receiver instead",
			ActionableText: "use value receiver instead",
		},
	}

	classification := classifyFeedback(loop, candidates, 1700000000100)
	require.Len(t, classification.New, 1)
	require.Len(t, classification.Superseded, 1)
	assert.Equal(t, "use pointer receiver", classification.Superseded[0].ActionableText)
	assert.Equal(t, findingStatusSuperseded, classification.Superseded[0].Status)
}

func TestClassifyFeedback_UnscopedFeedback_DedupesAcrossSourceURLs(t *testing.T) {
	loop := &kvstore.ReviewLoop{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 4,
		Findings: []kvstore.ReviewFinding{
			{
				Key: buildFindingKey(reviewFeedbackCandidate{
					SourceType:     "review_body",
					ReviewerLogin:  "coderabbitai[bot]",
					ActionableText: "Add focused tests for nil input handling.",
				}),
				Status:         findingStatusOpen,
				SourceType:     "review_body",
				SourceURL:      "https://github.com/org/repo/pull/42#pullrequestreview-100",
				ReviewerLogin:  "coderabbitai[bot]",
				ReviewerType:   reviewerTypeAIBot,
				ActionableText: "Add focused tests for nil input handling.",
			},
		},
	}

	candidates := []reviewFeedbackCandidate{
		{
			SourceType:     "review_body",
			SourceURL:      "https://github.com/org/repo/pull/42#pullrequestreview-101",
			ReviewerLogin:  "coderabbitai[bot]",
			ReviewerType:   reviewerTypeAIBot,
			RawText:        "Add focused tests for nil input handling.",
			ActionableText: "Add focused tests for nil input handling.",
		},
	}

	classification := classifyFeedback(loop, candidates, 1700000000200)
	require.Len(t, classification.New, 0)
	require.Len(t, classification.Repeated, 1)
	require.Len(t, classification.Resolved, 0)
	require.Len(t, classification.Dispatchable, 1)
	assert.Equal(t, "Add focused tests for nil input handling.", classification.Repeated[0].ActionableText)
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
