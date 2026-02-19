package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

const pr25FixtureDir = "testdata/reviewfeedback/pr25"

type mixedPipelineFixtureEntry struct {
	ID           int64  `json:"id"`
	NodeID       string `json:"node_id"`
	URL          string `json:"url"`
	ReviewerLogin string `json:"reviewer_login"`
	Path         string `json:"path"`
	Line         int    `json:"line"`
	CommitSHA    string `json:"commit_sha"`
	Body         string `json:"body"`
	BodyFixture  string `json:"body_fixture"`
}

type mixedPipelineFixture struct {
	ReviewComments []mixedPipelineFixtureEntry `json:"review_comments"`
	Reviews        []mixedPipelineFixtureEntry `json:"reviews"`
	IssueComments  []mixedPipelineFixtureEntry `json:"issue_comments"`
}

func mustReadFixture(t *testing.T, relativePath string) string {
	t.Helper()

	fullPath := filepath.Join(pr25FixtureDir, relativePath)
	data, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	return string(data)
}

func mustLoadMixedPipelineFixture(t *testing.T, relativePath string) mixedPipelineFixture {
	t.Helper()

	var fixture mixedPipelineFixture
	raw := mustReadFixture(t, relativePath)
	require.NoError(t, json.Unmarshal([]byte(raw), &fixture))
	return fixture
}

func newCandidateFromFixture(t *testing.T, sourceType, reviewerLogin, relativePath string) reviewFeedbackCandidate {
	t.Helper()

	return reviewFeedbackCandidate{
		SourceType:    sourceType,
		ReviewerLogin: reviewerLogin,
		RawText:       mustReadFixture(t, relativePath),
	}
}

func resolveFixtureBody(t *testing.T, entry mixedPipelineFixtureEntry) string {
	t.Helper()

	if entry.BodyFixture != "" {
		return mustReadFixture(t, entry.BodyFixture)
	}
	return entry.Body
}

func mockGitHubWithMixedPipelineFixture(t *testing.T, ghMock *mockGitHubClient, owner, repo string, prNumber int, fixture mixedPipelineFixture) {
	t.Helper()

	reviewComments := make([]*github.PullRequestComment, 0, len(fixture.ReviewComments))
	for _, entry := range fixture.ReviewComments {
		body := resolveFixtureBody(t, entry)
		comment := &github.PullRequestComment{
			ID:   github.Ptr(entry.ID),
			User: &github.User{Login: github.Ptr(entry.ReviewerLogin)},
			Body: github.Ptr(body),
		}
		if entry.NodeID != "" {
			comment.NodeID = github.Ptr(entry.NodeID)
		}
		if entry.URL != "" {
			comment.HTMLURL = github.Ptr(entry.URL)
		}
		if entry.Path != "" {
			comment.Path = github.Ptr(entry.Path)
		}
		if entry.Line > 0 {
			comment.Line = github.Ptr(entry.Line)
		}
		if entry.CommitSHA != "" {
			comment.CommitID = github.Ptr(entry.CommitSHA)
		}
		reviewComments = append(reviewComments, comment)
	}

	reviews := make([]*github.PullRequestReview, 0, len(fixture.Reviews))
	for _, entry := range fixture.Reviews {
		body := resolveFixtureBody(t, entry)
		review := &github.PullRequestReview{
			ID:   github.Ptr(entry.ID),
			User: &github.User{Login: github.Ptr(entry.ReviewerLogin)},
			Body: github.Ptr(body),
		}
		if entry.NodeID != "" {
			review.NodeID = github.Ptr(entry.NodeID)
		}
		if entry.URL != "" {
			review.HTMLURL = github.Ptr(entry.URL)
		}
		if entry.CommitSHA != "" {
			review.CommitID = github.Ptr(entry.CommitSHA)
		}
		reviews = append(reviews, review)
	}

	issueComments := make([]*github.IssueComment, 0, len(fixture.IssueComments))
	for _, entry := range fixture.IssueComments {
		body := resolveFixtureBody(t, entry)
		issueComment := &github.IssueComment{
			ID:   github.Ptr(entry.ID),
			User: &github.User{Login: github.Ptr(entry.ReviewerLogin)},
			Body: github.Ptr(body),
		}
		if entry.NodeID != "" {
			issueComment.NodeID = github.Ptr(entry.NodeID)
		}
		if entry.URL != "" {
			issueComment.HTMLURL = github.Ptr(entry.URL)
		}
		issueComments = append(issueComments, issueComment)
	}

	ghMock.On("ListReviewComments", mock.Anything, owner, repo, prNumber).Return(reviewComments, nil)
	ghMock.On("ListReviews", mock.Anything, owner, repo, prNumber).Return(reviews, nil)
	ghMock.On("ListIssueComments", mock.Anything, owner, repo, prNumber).Return(issueComments, nil)
}

func mustExtractActionableCandidate(t *testing.T, candidate reviewFeedbackCandidate) reviewFeedbackCandidate {
	t.Helper()

	candidate = normalizeFeedbackCandidate(candidate)
	actionableText, _, dropReason := extractCandidateActionableText(candidate)
	require.Empty(t, dropReason)
	require.NotEmpty(t, actionableText)
	candidate.ActionableText = actionableText
	return candidate
}

func assertClassificationTuple(t *testing.T, classification reviewFeedbackClassification, newCount, repeatedCount, resolvedCount, supersededCount, dispatchableCount int) {
	t.Helper()

	assert.Len(t, classification.New, newCount)
	assert.Len(t, classification.Repeated, repeatedCount)
	assert.Len(t, classification.Resolved, resolvedCount)
	assert.Len(t, classification.Superseded, supersededCount)
	assert.Len(t, classification.Dispatchable, dispatchableCount)
}

func TestExtractCandidateActionableText_CodeRabbit_PR25ReviewCommentPrompt_ExtractsOnlyPromptBlock(t *testing.T) {
	candidate := newCandidateFromFixture(t, "review_comment", "coderabbitai[bot]", "coderabbit_review_comment_prompt.md")
	candidate = normalizeFeedbackCandidate(candidate)

	actionableText, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
	assert.Equal(t, "Please guard against nil pointer dereference in buildPayload.\nAdd regression coverage for nil input.", actionableText)
	assert.NotContains(t, actionableText, "Summary")
	assert.NotContains(t, actionableText, "Walkthrough")
}

func TestExtractCandidateActionableText_CodeRabbit_PR25ReviewBodyPrompt_FencedBlockExtracted(t *testing.T) {
	candidate := newCandidateFromFixture(t, "review_body", "coderabbitai[bot]", "coderabbit_review_body_prompt.md")
	candidate = normalizeFeedbackCandidate(candidate)

	actionableText, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
	assert.Equal(t, "Use context.WithTimeout in API requests.\nConfirm behavior for canceled contexts.", actionableText)
}

func TestExtractCandidateActionableText_CodeRabbit_PR25IssueCommentNoise_DropsWhenMarkerMissing(t *testing.T) {
	candidate := newCandidateFromFixture(t, "issue_comment", "coderabbitai[bot]", "coderabbit_issue_comment_noise.md")
	candidate = normalizeFeedbackCandidate(candidate)

	actionableText, route, dropReason := extractCandidateActionableText(candidate)

	assert.Empty(t, actionableText)
	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Equal(t, reviewerExtractionDropReasonCodeRabbitMarkersMissing, dropReason)
}

func TestExtractCandidateActionableText_CodeRabbit_PR25MarkdownWrappedMarker_PreservedBehavior(t *testing.T) {
	candidate := newCandidateFromFixture(t, "review_comment", "coderabbitai[bot]", "coderabbit_markdown_wrapped_marker.md")
	candidate = normalizeFeedbackCandidate(candidate)

	actionableText, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, reviewerExtractionRouteCodeRabbit, route)
	assert.Empty(t, dropReason)
	assert.Equal(t, "Please add test coverage for invalid token handling.", actionableText)
}

func TestExtractCandidateActionableText_NonCodeRabbit_PR25InlinePassThrough_MinimalNormalizationOnly(t *testing.T) {
	candidate := newCandidateFromFixture(t, "review_comment", "copilot-pull-request-reviewer", "noncoderabbit_inline_review_comment.md")
	candidate = normalizeFeedbackCandidate(candidate)

	actionableText, route, dropReason := extractCandidateActionableText(candidate)

	assert.Equal(t, reviewerExtractionRouteNonCodeRabbit, route)
	assert.Empty(t, dropReason)
	assert.Equal(t, "Keep this inline suggestion.\n\nAdd focused coverage for empty input.", actionableText)
}

func TestExtractCandidateActionableText_NonCodeRabbit_PR25NonInlineDropped(t *testing.T) {
	sourceTypes := []string{"review_body", "issue_comment"}

	for _, sourceType := range sourceTypes {
		t.Run(sourceType, func(t *testing.T) {
			candidate := newCandidateFromFixture(t, sourceType, "human-reviewer", "noncoderabbit_noninline_review_body.md")
			candidate = normalizeFeedbackCandidate(candidate)

			actionableText, route, dropReason := extractCandidateActionableText(candidate)

			assert.Empty(t, actionableText)
			assert.Equal(t, reviewerExtractionRouteNonCodeRabbit, route)
			assert.Equal(t, reviewerExtractionDropReasonNonCodeRabbitNonInlineSource, dropReason)
		})
	}
}

func TestCollectReviewFeedbackPipeline_AwaitingReview_PR25Fixtures_MixedSources(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)
	fixture := mustLoadMixedPipelineFixture(t, "mixed_pipeline_bundle.json")
	mockGitHubWithMixedPipelineFixture(t, ghMock, "org", "repo", 42, fixture)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-pr25-awaiting",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		LastCommitSHA: "sha-current-001",
	}

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	assert.Contains(t, feedback, "Please guard against nil pointer dereference in buildPayload.")
	assert.Contains(t, feedback, "Use context.WithTimeout in API requests.")
	assert.Contains(t, feedback, "Keep this inline suggestion.")
	assert.NotContains(t, feedback, "Stale commit comment should be filtered before extraction.")
	assert.NotContains(t, feedback, "Please add a changelog entry.")
	assert.NotContains(t, feedback, "metadata that should not be included")
	assert.NotContains(t, feedback, "rate limit remaining")

	ghMock.AssertExpectations(t)
}

func TestCollectReviewFeedbackPipeline_HumanReview_PR25Fixtures_ClassificationInputShape(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)
	fixture := mustLoadMixedPipelineFixture(t, "mixed_pipeline_bundle.json")
	mockGitHubWithMixedPipelineFixture(t, ghMock, "org", "repo", 42, fixture)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-pr25-human",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseHumanReview,
		LastCommitSHA: "sha-current-001",
	}

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	assert.Contains(t, feedback, "server/webhook.go:51")
	assert.Contains(t, feedback, "Keep this inline suggestion.")
	assert.NotContains(t, feedback, "Please guard against nil pointer dereference in buildPayload.")
	assert.NotContains(t, feedback, "Use context.WithTimeout in API requests.")
	assert.NotContains(t, feedback, "Please add integration tests for timeout handling in the webhook retry path.")
	assert.NotContains(t, feedback, "Please add a changelog entry.")

	ghMock.AssertExpectations(t)
}

func TestCollectReviewFeedbackBundle_PR25Fixtures_LogsExpectedDropReasons(t *testing.T) {
	p, api, _, ghMock := setupReviewLoopTestPlugin(t)
	p.configuration.EnableDebugLogging = true

	fixture := mustLoadMixedPipelineFixture(t, "mixed_pipeline_bundle.json")
	mockGitHubWithMixedPipelineFixture(t, ghMock, "org", "repo", 42, fixture)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-pr25-logs",
		AgentRecordID: "agent-pr25",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     4,
		LastCommitSHA: "sha-current-001",
		PRURL:         "https://github.com/org/repo/pull/42",
	}

	classification, _, feedback, err := p.collectReviewFeedbackBundle(loop)
	require.NoError(t, err)
	assertClassificationTuple(t, classification, 3, 0, 0, 0, 3)
	assert.Contains(t, feedback, "Please guard against nil pointer dereference in buildPayload.")
	assert.Contains(t, feedback, "Use context.WithTimeout in API requests.")
	assert.Contains(t, feedback, "Keep this inline suggestion.")

	droppedLogs := collectDroppedCandidateLogs(api)
	require.Len(t, droppedLogs, 5)
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonCodeRabbitMarkersMissing, reviewerExtractionRouteCodeRabbit))
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonNonCodeRabbitNonInlineSource, reviewerExtractionRouteNonCodeRabbit))
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonNormalizedEmpty, reviewerExtractionRouteNonCodeRabbit))
	assert.True(t, hasDroppedCandidateLog(droppedLogs, reviewerExtractionDropReasonActionableEmpty, reviewerExtractionRouteNonCodeRabbit))

	ghMock.AssertExpectations(t)
}

func TestCollectReviewFeedbackPipeline_PR25Fixtures_CommitSHAFilterStillApplies(t *testing.T) {
	p, _, _, ghMock := setupReviewLoopTestPlugin(t)
	fixture := mustLoadMixedPipelineFixture(t, "mixed_pipeline_bundle.json")
	mockGitHubWithMixedPipelineFixture(t, ghMock, "org", "repo", 42, fixture)

	loop := &kvstore.ReviewLoop{
		ID:            "loop-pr25-commit-filter",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		LastCommitSHA: "sha-current-001",
	}

	feedback, err := p.collectReviewFeedback(loop)
	require.NoError(t, err)

	assert.Contains(t, feedback, "server/reviewloop_feedback.go:144")
	assert.Contains(t, feedback, "Please guard against nil pointer dereference in buildPayload.")
	assert.NotContains(t, feedback, "server/reviewloop_feedback.go:33")
	assert.NotContains(t, feedback, "Stale commit comment should be filtered before extraction.")
}

func TestClassificationRegression_PR25Fixtures_NewRepeatedResolved_Unchanged(t *testing.T) {
	loop := &kvstore.ReviewLoop{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 1,
	}

	commentCandidate := newCandidateFromFixture(t, "review_comment", "coderabbitai[bot]", "coderabbit_review_comment_prompt.md")
	commentCandidate.Path = "server/reviewloop_feedback.go"
	commentCandidate.Line = 144
	commentCandidate.SourceURL = "https://github.com/org/repo/pull/42#discussion_r1001"
	commentCandidate.CommitSHA = "sha-current-001"
	commentCandidate.ReviewerType = reviewerTypeAIBot
	commentCandidate = mustExtractActionableCandidate(t, commentCandidate)

	inlineCandidate := newCandidateFromFixture(t, "review_comment", "copilot-pull-request-reviewer", "noncoderabbit_inline_review_comment.md")
	inlineCandidate.Path = "server/api.go"
	inlineCandidate.Line = 77
	inlineCandidate.SourceURL = "https://github.com/org/repo/pull/42#discussion_r1003"
	inlineCandidate.CommitSHA = "sha-current-001"
	inlineCandidate.ReviewerType = reviewerTypeAIBot
	inlineCandidate = mustExtractActionableCandidate(t, inlineCandidate)

	classificationIteration1 := classifyFeedback(loop, []reviewFeedbackCandidate{commentCandidate, inlineCandidate}, 1700000001000)
	assertClassificationTuple(t, classificationIteration1, 2, 0, 0, 0, 2)

	loop.Iteration = 2

	newCandidate := newCandidateFromFixture(t, "review_comment", "coderabbitai[bot]", "coderabbit_markdown_wrapped_marker.md")
	newCandidate.Path = "server/webhook.go"
	newCandidate.Line = 91
	newCandidate.SourceURL = "https://github.com/org/repo/pull/42#discussion_r1400"
	newCandidate.CommitSHA = "sha-current-002"
	newCandidate.ReviewerType = reviewerTypeAIBot
	newCandidate = mustExtractActionableCandidate(t, newCandidate)

	classificationIteration2 := classifyFeedback(loop, []reviewFeedbackCandidate{commentCandidate, newCandidate}, 1700000002000)
	assertClassificationTuple(t, classificationIteration2, 1, 1, 1, 0, 2)
	assert.Equal(t, buildFindingKey(newCandidate), classificationIteration2.New[0].Key)
	assert.Equal(t, buildFindingKey(commentCandidate), classificationIteration2.Repeated[0].Key)
	assert.Equal(t, buildFindingKey(inlineCandidate), classificationIteration2.Resolved[0].Key)
}

func TestClassificationRegression_PR25Fixtures_SupersedeAtSameLocation_Unchanged(t *testing.T) {
	loop := &kvstore.ReviewLoop{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 1,
	}

	originalCandidate := newCandidateFromFixture(t, "review_comment", "coderabbitai[bot]", "coderabbit_review_comment_prompt.md")
	originalCandidate.Path = "server/reviewloop.go"
	originalCandidate.Line = 65
	originalCandidate.SourceURL = "https://github.com/org/repo/pull/42#discussion_r2001"
	originalCandidate.CommitSHA = "sha-current-001"
	originalCandidate.ReviewerType = reviewerTypeAIBot
	originalCandidate = mustExtractActionableCandidate(t, originalCandidate)

	classificationIteration1 := classifyFeedback(loop, []reviewFeedbackCandidate{originalCandidate}, 1700000002100)
	assertClassificationTuple(t, classificationIteration1, 1, 0, 0, 0, 1)

	loop.Iteration = 2

	updatedCandidate := newCandidateFromFixture(t, "review_comment", "coderabbitai[bot]", "coderabbit_markdown_wrapped_marker.md")
	updatedCandidate.Path = "server/reviewloop.go"
	updatedCandidate.Line = 65
	updatedCandidate.SourceURL = "https://github.com/org/repo/pull/42#discussion_r2002"
	updatedCandidate.CommitSHA = "sha-current-002"
	updatedCandidate.ReviewerType = reviewerTypeAIBot
	updatedCandidate = mustExtractActionableCandidate(t, updatedCandidate)

	classificationIteration2 := classifyFeedback(loop, []reviewFeedbackCandidate{updatedCandidate}, 1700000002200)
	assertClassificationTuple(t, classificationIteration2, 1, 0, 0, 1, 1)
	assert.Equal(t, "Please guard against nil pointer dereference in buildPayload.\nAdd regression coverage for nil input.", classificationIteration2.Superseded[0].ActionableText)
	assert.Equal(t, findingStatusSuperseded, classificationIteration2.Superseded[0].Status)
}

func TestClassificationRegression_PR25Fixtures_UnscopedDedupAcrossReviewBodyURLs_Unchanged(t *testing.T) {
	loop := &kvstore.ReviewLoop{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 1,
	}

	initialCandidate := newCandidateFromFixture(t, "review_body", "coderabbitai[bot]", "coderabbit_review_body_prompt.md")
	initialCandidate.SourceURL = "https://github.com/org/repo/pull/42#pullrequestreview-2001"
	initialCandidate.SourceID = 2001
	initialCandidate.ReviewerType = reviewerTypeAIBot
	initialCandidate = mustExtractActionableCandidate(t, initialCandidate)

	classificationIteration1 := classifyFeedback(loop, []reviewFeedbackCandidate{initialCandidate}, 1700000003000)
	assertClassificationTuple(t, classificationIteration1, 1, 0, 0, 0, 1)

	loop.Iteration = 2

	repeatedCandidate := newCandidateFromFixture(t, "review_body", "coderabbitai[bot]", "coderabbit_review_body_prompt.md")
	repeatedCandidate.SourceURL = "https://github.com/org/repo/pull/42#pullrequestreview-2002"
	repeatedCandidate.SourceID = 2002
	repeatedCandidate.ReviewerType = reviewerTypeAIBot
	repeatedCandidate = mustExtractActionableCandidate(t, repeatedCandidate)

	classificationIteration2 := classifyFeedback(loop, []reviewFeedbackCandidate{repeatedCandidate}, 1700000003100)
	assertClassificationTuple(t, classificationIteration2, 0, 1, 0, 0, 1)
	assert.Equal(t, buildFindingKey(initialCandidate), classificationIteration2.Repeated[0].Key)
}

func TestClassificationRegression_PR25Fixtures_ExtractionOutputDoesNotChangeKeyingRules(t *testing.T) {
	fixtureCandidate := newCandidateFromFixture(t, "review_comment", "copilot-pull-request-reviewer", "noncoderabbit_inline_review_comment.md")
	fixtureCandidate.Path = "server/api.go"
	fixtureCandidate.Line = 77
	fixtureCandidate.SourceURL = "https://github.com/org/repo/pull/42#discussion_r3001"
	fixtureCandidate.SourceID = 3001
	fixtureCandidate.ReviewerType = reviewerTypeAIBot
	fixtureCandidate = mustExtractActionableCandidate(t, fixtureCandidate)

	directCandidate := reviewFeedbackCandidate{
		SourceType:     "review_comment",
		SourceID:       3002,
		SourceURL:      "https://github.com/org/repo/pull/42#discussion_r3002",
		ReviewerLogin:  "copilot-pull-request-reviewer",
		ReviewerType:   reviewerTypeAIBot,
		Path:           "server/api.go",
		Line:           77,
		RawText:        "Keep this inline suggestion.\n\nAdd focused coverage for empty input.",
		ActionableText: "Keep this inline suggestion.\n\nAdd focused coverage for empty input.",
	}

	assert.Equal(t, buildFindingKey(fixtureCandidate), buildFindingKey(directCandidate))
	assert.True(t, shouldCollapseByText(fixtureCandidate, directCandidate))

	loop := &kvstore.ReviewLoop{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Iteration: 1,
	}

	classification := classifyFeedback(loop, []reviewFeedbackCandidate{fixtureCandidate, directCandidate}, 1700000004000)
	assertClassificationTuple(t, classification, 1, 0, 0, 0, 1)
}
