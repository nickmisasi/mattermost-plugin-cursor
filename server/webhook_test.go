package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

//nolint:gosec // test constant, not a real credential
const testWebhookSecret = "test-webhook-secret"

// signPayload generates a valid HMAC-SHA256 signature for test payloads.
func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// makeWebhookRequest creates an HTTP request suitable for webhook testing.
func makeWebhookRequest(t *testing.T, eventType, deliveryID string, body []byte, signature string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if eventType != "" {
		req.Header.Set(eventHeader, eventType)
	}
	if deliveryID != "" {
		req.Header.Set(deliveryHeader, deliveryID)
	}
	if signature != "" {
		req.Header.Set(signatureHeaderSHA256, signature)
	}
	return req
}

func setupWebhookTestPlugin(t *testing.T) (*Plugin, *mockKVStore) {
	t.Helper()
	p, _, _, store := setupTestPlugin(t)
	p.configuration = &configuration{
		CursorAPIKey:        "test-key",
		GitHubWebhookSecret: testWebhookSecret,
	}
	return p, store
}

// --- Signature verification tests ---

func TestVerifySignature_Valid(t *testing.T) {
	secret := []byte("mysecret")
	body := []byte(`{"action":"closed"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	assert.True(t, verifyWebhookSignature(secret, sig, body))
}

func TestVerifySignature_WrongSecret(t *testing.T) {
	body := []byte(`{"action":"closed"}`)
	mac := hmac.New(sha256.New, []byte("wrong-secret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	assert.False(t, verifyWebhookSignature([]byte("correct-secret"), sig, body))
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	secret := []byte("mysecret")
	body := []byte(`{"action":"closed"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tamperedBody := []byte(`{"action":"opened"}`)
	assert.False(t, verifyWebhookSignature(secret, sig, tamperedBody))
}

func TestVerifySignature_EmptySignature(t *testing.T) {
	assert.False(t, verifyWebhookSignature([]byte("secret"), "", []byte("body")))
}

func TestVerifySignature_MalformedSignature(t *testing.T) {
	// Missing sha256= prefix
	assert.False(t, verifyWebhookSignature([]byte("secret"), "abcdef1234567890", []byte("body")))
}

func TestVerifySignature_InvalidHex(t *testing.T) {
	assert.False(t, verifyWebhookSignature([]byte("secret"), "sha256=notvalidhex!!", []byte("body")))
}

// --- Webhook handler tests ---

func TestWebhook_NoSecret(t *testing.T) {
	p, _ := setupWebhookTestPlugin(t)
	p.configuration.GitHubWebhookSecret = "" // Clear secret

	body := []byte(`{}`)
	req := makeWebhookRequest(t, "ping", "delivery-1", body, "sha256=abc")
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "webhook secret not configured")
}

func TestWebhook_InvalidSignature(t *testing.T) {
	p, _ := setupWebhookTestPlugin(t)

	body := []byte(`{}`)
	req := makeWebhookRequest(t, "ping", "delivery-1", body, "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid signature")
}

func TestWebhook_PingEvent(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	pingPayload := PingEvent{Zen: "Keep it logically awesome.", HookID: 42}
	body, _ := json.Marshal(pingPayload)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-ping").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-ping").Return(nil)

	req := makeWebhookRequest(t, "ping", "delivery-ping", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"status": "ok"`)
}

func TestWebhook_UnknownEvent(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	body := []byte(`{}`)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-unknown").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-unknown").Return(nil)

	req := makeWebhookRequest(t, "issues", "delivery-unknown", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestWebhook_PRMerged(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-123",
		PostID:        "root-post-1",
		TriggerPostID: "trigger-post-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
		Status:        "FINISHED",
		PrURL:         "https://github.com/org/repo/pull/42",
	}

	event := PullRequestEvent{
		Action: "closed",
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
			Title:   "Fix login bug",
			State:   "closed",
			Merged:  true,
		},
	}
	event.PullRequest.Head.Ref = "cursor/fix-login"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-merged").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-merged").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(agent, nil)

	// Expect thread notification attachment post: green color for merged PR.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-1" &&
			p.ChannelId == "ch-1" &&
			hasAttachmentWithColor(p, "#3DB887")
	})).Return(&model.Post{Id: "notification-1"}, nil)

	// Expect reaction swap on trigger post.
	api.On("RemoveReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-post-1" && r.EmojiName == "white_check_mark"
	})).Return(nil)
	api.On("AddReaction", mock.MatchedBy(func(r *model.Reaction) bool {
		return r.PostId == "trigger-post-1" && r.EmojiName == "rocket"
	})).Return(nil, nil)

	// Expect agent status update.
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-123" && r.Status == "MERGED"
	})).Return(nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-merged", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_PRClosedNotMerged(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-456",
		PostID:        "root-post-2",
		TriggerPostID: "trigger-post-2",
		ChannelID:     "ch-2",
		UserID:        "user-2",
		Status:        "FINISHED",
		PrURL:         "https://github.com/org/repo/pull/99",
	}

	event := PullRequestEvent{
		Action: "closed",
		PullRequest: ghPullRequest{
			Number:  99,
			HTMLURL: "https://github.com/org/repo/pull/99",
			Title:   "Unused feature",
			State:   "closed",
			Merged:  false,
		},
	}
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-closed").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-closed").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/99").Return(agent, nil)

	// Notification attachment post: grey color for closed-without-merge PR.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-2" &&
			p.ChannelId == "ch-2" &&
			hasAttachmentWithColor(p, "#8B8FA7")
	})).Return(&model.Post{Id: "notification-2"}, nil)

	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-456" && r.Status == "PR_CLOSED"
	})).Return(nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-closed", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_PROpened_NoAgentFound(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	event := PullRequestEvent{
		Action: "opened",
		PullRequest: ghPullRequest{
			Number:  10,
			HTMLURL: "https://github.com/org/repo/pull/10",
			Title:   "New feature",
		},
	}
	event.PullRequest.Head.Ref = "cursor/new-feature"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-opened").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-opened").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/10").Return(nil, nil)
	store.On("GetAgentByBranch", "cursor/new-feature").Return(nil, nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-opened", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// No agent found, so no save or notification.
	store.AssertNotCalled(t, "SaveAgent")
}

func TestWebhook_PROpened_BackfillsPrURL(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-opened-1",
		PostID:        "root-post-opened",
		ChannelID:     "ch-opened",
		UserID:        "user-1",
		Status:        "RUNNING",
		PrURL:         "", // empty -- should be backfilled
		TargetBranch:  "", // empty -- should be backfilled
	}

	event := PullRequestEvent{
		Action: "opened",
		PullRequest: ghPullRequest{
			Number:  10,
			HTMLURL: "https://github.com/org/repo/pull/10",
			Title:   "New feature",
		},
	}
	event.PullRequest.Head.Ref = "cursor/new-feature"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-opened-backfill").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-opened-backfill").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/10").Return(nil, nil)
	store.On("GetAgentByBranch", "cursor/new-feature").Return(agent, nil)

	// Expect SaveAgent with backfilled PrURL and TargetBranch.
	store.On("SaveAgent", mock.MatchedBy(func(r *kvstore.AgentRecord) bool {
		return r.CursorAgentID == "agent-opened-1" &&
			r.PrURL == "https://github.com/org/repo/pull/10" &&
			r.TargetBranch == "cursor/new-feature" &&
			r.UpdatedAt > 0
	})).Return(nil)

	// WebSocket event for RHS update.
	api.On("PublishWebSocketEvent", "agent_status_change", mock.Anything, mock.Anything).Return()

	// PR notification attachment post: blue color for opened PR.
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-post-opened" &&
			post.ChannelId == "ch-opened" &&
			hasAttachmentWithColor(post, "#2389D7")
	})).Return(&model.Post{Id: "notif-opened-1"}, nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-opened-backfill", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_PROpened_SkipsIfRunning(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	// Agent is still RUNNING -- should NOT start review loop.
	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-running-1",
		PostID:        "root-post-running",
		ChannelID:     "ch-running",
		UserID:        "user-1",
		Status:        "RUNNING",
		PrURL:         "",
	}

	p.configuration.EnableAIReviewLoop = true

	event := PullRequestEvent{
		Action: "opened",
		PullRequest: ghPullRequest{
			Number:  11,
			HTMLURL: "https://github.com/org/repo/pull/11",
			Title:   "Feature X",
		},
	}
	event.PullRequest.Head.Ref = "cursor/feature-x"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-opened-running").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-opened-running").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/11").Return(nil, nil)
	store.On("GetAgentByBranch", "cursor/feature-x").Return(agent, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)

	api.On("PublishWebSocketEvent", "agent_status_change", mock.Anything, mock.Anything).Return()
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "notif-1"}, nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-opened-running", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Review loop should NOT be started since agent is still RUNNING.
	store.AssertNotCalled(t, "GetReviewLoopByPRURL")
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestWebhook_PROpened_StartsReviewLoop(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	// Agent is FINISHED -- should start review loop.
	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-finished-1",
		PostID:        "root-post-finished",
		TriggerPostID: "trigger-post-finished",
		ChannelID:     "ch-finished",
		UserID:        "user-1",
		Status:        "FINISHED",
		PrURL:         "", // will be backfilled
		Repository:    "org/repo",
	}

	p.configuration.EnableAIReviewLoop = true
	p.configuration.AIReviewerBots = "coderabbitai[bot]"
	p.configuration.MaxReviewIterations = 5

	// Set up a mock GitHub client so the review loop condition passes.
	mockGH := &mockGitHubClient{}
	p.githubClient = mockGH

	event := PullRequestEvent{
		Action: "opened",
		PullRequest: ghPullRequest{
			Number:  12,
			HTMLURL: "https://github.com/org/repo/pull/12",
			Title:   "Fix bug",
		},
	}
	event.PullRequest.Head.Ref = "cursor/fix-bug"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-opened-loop").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-opened-loop").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/12").Return(nil, nil)
	store.On("GetAgentByBranch", "cursor/fix-bug").Return(agent, nil)
	store.On("SaveAgent", mock.Anything).Return(nil)

	// startReviewLoop: idempotency check.
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/12").Return(nil, nil)
	// startReviewLoop: HITL workflow check.
	store.On("GetWorkflowByAgent", "agent-finished-1").Return("", nil)
	// startReviewLoop: save the review loop (called twice: create + phase update).
	store.On("SaveReviewLoop", mock.Anything).Return(nil)

	// startReviewLoop: GetAgent for inline status update.
	store.On("GetAgent", "agent-finished-1").Return(agent, nil).Maybe()

	// GitHub client: MarkPRReadyForReview + RequestReviewers.
	mockGH.On("MarkPRReadyForReview", mock.Anything, "org", "repo", 12).Return(nil)
	mockGH.On("RequestReviewers", mock.Anything, "org", "repo", 12, mock.Anything).Return(nil)

	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "notif-1"}, nil).Maybe()
	api.On("GetPost", mock.Anything).Return(&model.Post{Id: "reply-1"}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil).Maybe()
	api.On("AddReaction", mock.Anything).Return(nil, nil).Maybe()

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-opened-loop", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Verify review loop was saved.
	store.AssertCalled(t, "SaveReviewLoop", mock.Anything)
}

func TestWebhook_PROpened_IdempotentPrURL(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	// Agent already has PrURL -- should NOT re-save.
	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-idem-1",
		PostID:        "root-post-idem",
		ChannelID:     "ch-idem",
		UserID:        "user-1",
		Status:        "FINISHED",
		PrURL:         "https://github.com/org/repo/pull/13",
		TargetBranch:  "cursor/already-set",
	}

	event := PullRequestEvent{
		Action: "opened",
		PullRequest: ghPullRequest{
			Number:  13,
			HTMLURL: "https://github.com/org/repo/pull/13",
			Title:   "Already linked",
		},
	}
	event.PullRequest.Head.Ref = "cursor/already-set"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-opened-idem").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-opened-idem").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/13").Return(agent, nil)

	// PR notification attachment post.
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-post-idem" &&
			hasAttachmentWithColor(post, "#2389D7")
	})).Return(&model.Post{Id: "notif-idem-1"}, nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-opened-idem", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// PrURL and TargetBranch were already set, so SaveAgent should NOT be called.
	store.AssertNotCalled(t, "SaveAgent")
	// WebSocket event should NOT be published since nothing changed.
	api.AssertNotCalled(t, "PublishWebSocketEvent")
}

func TestWebhook_PRNotFound(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	event := PullRequestEvent{
		Action: "closed",
		PullRequest: ghPullRequest{
			Number:  55,
			HTMLURL: "https://github.com/org/repo/pull/55",
			Merged:  true,
		},
	}
	event.PullRequest.Head.Ref = "some-branch"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-notfound").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-notfound").Return(nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/55").Return(nil, nil)
	store.On("GetAgentByBranch", "some-branch").Return(nil, nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-notfound", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// No post should be created.
	store.AssertNotCalled(t, "SaveAgent")
}

func TestWebhook_ReviewApproved(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-review-1",
		PostID:        "root-post-rv",
		ChannelID:     "ch-rv",
		Status:        "FINISHED",
		PrURL:         "https://github.com/org/repo/pull/77",
	}

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "approved",
			HTMLURL: "https://github.com/org/repo/pull/77#pullrequestreview-1",
		},
		PullRequest: ghPullRequest{
			Number:  77,
			HTMLURL: "https://github.com/org/repo/pull/77",
		},
	}
	event.Review.User.Login = "reviewer-1"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-rv-approved").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-rv-approved").Return(nil)
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/77").Return(nil, nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/77").Return(agent, nil)

	// Review approved attachment: green color for approved.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-rv" &&
			hasAttachmentWithColor(p, "#3DB887") &&
			hasAttachmentWithTitle(p, "approved")
	})).Return(&model.Post{Id: "rv-notification-1"}, nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-rv-approved", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_ReviewChangesRequested(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-review-2",
		PostID:        "root-post-cr",
		ChannelID:     "ch-cr",
		Status:        "FINISHED",
		PrURL:         "https://github.com/org/repo/pull/88",
	}

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "changes_requested",
			Body:    "Please fix the error handling in the login function.",
			HTMLURL: "https://github.com/org/repo/pull/88#pullrequestreview-2",
		},
		PullRequest: ghPullRequest{
			Number:  88,
			HTMLURL: "https://github.com/org/repo/pull/88",
		},
	}
	event.Review.User.Login = "senior-dev"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-rv-changes").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-rv-changes").Return(nil)
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/88").Return(nil, nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/88").Return(agent, nil)

	// Changes requested attachment: red color for changes_requested.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-cr" &&
			hasAttachmentWithColor(p, "#D24B4E") &&
			hasAttachmentWithTitle(p, "requested changes")
	})).Return(&model.Post{Id: "cr-notification-1"}, nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-rv-changes", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_ReviewCommented(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-review-3",
		PostID:        "root-post-cm",
		ChannelID:     "ch-cm",
		Status:        "FINISHED",
		PrURL:         "https://github.com/org/repo/pull/66",
	}

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "commented",
			Body:    "Looks good overall, just a nit.",
			HTMLURL: "https://github.com/org/repo/pull/66#pullrequestreview-3",
		},
		PullRequest: ghPullRequest{
			Number:  66,
			HTMLURL: "https://github.com/org/repo/pull/66",
		},
	}
	event.Review.User.Login = "commenter"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-rv-commented").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-rv-commented").Return(nil)
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/66").Return(nil, nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/66").Return(agent, nil)

	// Comment notification attachment: blue color for comment.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-cm" &&
			hasAttachmentWithColor(p, "#2389D7") &&
			hasAttachmentWithTitle(p, "commented")
	})).Return(&model.Post{Id: "cm-notification-1"}, nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-rv-commented", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_ReviewCommentedEmpty(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	agent := &kvstore.AgentRecord{
		CursorAgentID: "agent-review-4",
		PostID:        "root-post-empty",
		ChannelID:     "ch-empty",
		Status:        "FINISHED",
		PrURL:         "https://github.com/org/repo/pull/44",
	}

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "commented",
			Body:    "", // Empty body = inline-only comments
			HTMLURL: "https://github.com/org/repo/pull/44#pullrequestreview-4",
		},
		PullRequest: ghPullRequest{
			Number:  44,
			HTMLURL: "https://github.com/org/repo/pull/44",
		},
	}
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-rv-empty").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-rv-empty").Return(nil)
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/44").Return(nil, nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/44").Return(agent, nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-rv-empty", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// No post should be created for empty review body.
	api.AssertNotCalled(t, "CreatePost")
}

func TestWebhook_ReviewEdited_Ignored(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	event := PullRequestReviewEvent{
		Action: "edited",
		Review: ghReview{
			State: "approved",
		},
	}
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-rv-edited").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-rv-edited").Return(nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-rv-edited", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertNotCalled(t, "GetAgentByPRURL")
	api.AssertNotCalled(t, "CreatePost")
}

func TestWebhook_DuplicateDelivery(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	body := []byte(`{"action":"closed"}`)
	sig := signPayload(testWebhookSecret, body)

	// First call: not seen yet.
	store.On("HasDeliveryBeenProcessed", "dup-delivery-1").Return(true, nil)

	req := makeWebhookRequest(t, "pull_request", "dup-delivery-1", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Should not process any events.
	store.AssertNotCalled(t, "GetAgentByPRURL")
	store.AssertNotCalled(t, "MarkDeliveryProcessed")
	api.AssertNotCalled(t, "CreatePost")
}

// --- Agent lookup tests ---

func TestFindAgent_ByPRURL(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	expected := &kvstore.AgentRecord{
		CursorAgentID: "agent-pr-url",
		PostID:        "post-1",
	}

	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/1").Return(expected, nil)

	pr := ghPullRequest{HTMLURL: "https://github.com/org/repo/pull/1"}
	pr.Head.Ref = "cursor/fix-bug"

	result := p.findAgentForPR(pr)
	require.NotNil(t, result)
	assert.Equal(t, "agent-pr-url", result.CursorAgentID)

	// Branch lookup should NOT have been called since PR URL matched.
	store.AssertNotCalled(t, "GetAgentByBranch")
}

func TestFindAgent_ByBranch(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	expected := &kvstore.AgentRecord{
		CursorAgentID: "agent-branch",
		PostID:        "post-2",
	}

	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/2").Return(nil, nil)
	store.On("GetAgentByBranch", "cursor/fix-auth").Return(expected, nil)

	pr := ghPullRequest{HTMLURL: "https://github.com/org/repo/pull/2"}
	pr.Head.Ref = "cursor/fix-auth"

	result := p.findAgentForPR(pr)
	require.NotNil(t, result)
	assert.Equal(t, "agent-branch", result.CursorAgentID)
}

func TestFindAgent_NoMatch(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/3").Return(nil, nil)
	store.On("GetAgentByBranch", "feature/unrelated").Return(nil, nil)

	pr := ghPullRequest{HTMLURL: "https://github.com/org/repo/pull/3"}
	pr.Head.Ref = "feature/unrelated"

	result := p.findAgentForPR(pr)
	assert.Nil(t, result)
}

func TestFindAgent_PRURLTakesPrecedence(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	agentByURL := &kvstore.AgentRecord{
		CursorAgentID: "agent-url",
		PostID:        "post-url",
	}

	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/4").Return(agentByURL, nil)

	pr := ghPullRequest{HTMLURL: "https://github.com/org/repo/pull/4"}
	pr.Head.Ref = "cursor/some-branch"

	result := p.findAgentForPR(pr)
	require.NotNil(t, result)
	assert.Equal(t, "agent-url", result.CursorAgentID)
	store.AssertNotCalled(t, "GetAgentByBranch")
}

// --- Helper function tests ---

func TestTruncateText(t *testing.T) {
	assert.Equal(t, "", truncateText("", 100))
	assert.Equal(t, "short", truncateText("short", 100))
	assert.Equal(t, "exactly ten", truncateText("exactly ten", 11))
	assert.Equal(t, "hel...", truncateText("hello world", 6))
	assert.Equal(t, "trimmed", truncateText("  trimmed  ", 100))
}

func TestTruncateText_LongReviewBody(t *testing.T) {
	long := "This is a very long review comment that goes well beyond 200 characters. " +
		"It contains detailed feedback about the implementation, suggestions for improvements, " +
		"and references to specific code sections that need attention before the PR can be approved."
	result := truncateText(long, 200)
	assert.Len(t, result, 200)
	assert.True(t, len(result) <= 200)
	assert.True(t, result[len(result)-3:] == "...")
}

func TestSanitizeReviewBodyForMattermost(t *testing.T) {
	t.Run("removes details tags", func(t *testing.T) {
		input := "<details>Some hidden content</details>"
		result := sanitizeReviewBodyForMattermost(input)
		assert.NotContains(t, result, "<details>")
		assert.NotContains(t, result, "</details>")
		assert.Contains(t, result, "Some hidden content")
	})

	t.Run("converts summary to bold", func(t *testing.T) {
		input := "<summary>Walkthrough</summary>"
		result := sanitizeReviewBodyForMattermost(input)
		assert.Equal(t, "**Walkthrough**", result)
	})

	t.Run("converts blockquote to markdown quote", func(t *testing.T) {
		input := "<blockquote>This is quoted text</blockquote>"
		result := sanitizeReviewBodyForMattermost(input)
		assert.Equal(t, "> This is quoted text", result)
	})

	t.Run("converts multiline blockquote", func(t *testing.T) {
		input := "<blockquote>Line one\nLine two</blockquote>"
		result := sanitizeReviewBodyForMattermost(input)
		assert.Contains(t, result, "> Line one")
		assert.Contains(t, result, "> Line two")
	})

	t.Run("strips remaining HTML tags", func(t *testing.T) {
		input := "Hello <b>bold</b> and <i>italic</i> world"
		result := sanitizeReviewBodyForMattermost(input)
		assert.Equal(t, "Hello bold and italic world", result)
	})

	t.Run("collapses excessive blank lines", func(t *testing.T) {
		input := "Line one\n\n\n\n\nLine two"
		result := sanitizeReviewBodyForMattermost(input)
		assert.Equal(t, "Line one\n\nLine two", result)
	})

	t.Run("handles case insensitive tags", func(t *testing.T) {
		input := "<DETAILS><SUMMARY>Title</SUMMARY>Content</DETAILS>"
		result := sanitizeReviewBodyForMattermost(input)
		assert.Contains(t, result, "**Title**")
		assert.Contains(t, result, "Content")
		assert.NotContains(t, result, "<")
	})

	t.Run("handles typical CodeRabbit review body", func(t *testing.T) {
		input := `<details>
<summary>Walkthrough</summary>
The changes introduce a new feature for handling webhooks.
</details>

<blockquote>Please review the error handling carefully.</blockquote>

<details>
<summary>Changes</summary>
- Added webhook handler
- Updated tests
</details>`
		result := sanitizeReviewBodyForMattermost(input)
		assert.NotContains(t, result, "<details>")
		assert.NotContains(t, result, "</details>")
		assert.NotContains(t, result, "<summary>")
		assert.NotContains(t, result, "<blockquote>")
		assert.Contains(t, result, "**Walkthrough**")
		assert.Contains(t, result, "**Changes**")
		assert.Contains(t, result, "> Please review the error handling carefully.")
	})

	t.Run("empty string returns empty", func(t *testing.T) {
		assert.Equal(t, "", sanitizeReviewBodyForMattermost(""))
	})

	t.Run("plain text passes through unchanged", func(t *testing.T) {
		input := "This is plain text with no HTML."
		assert.Equal(t, input, sanitizeReviewBodyForMattermost(input))
	})

	t.Run("preserves markdown content", func(t *testing.T) {
		input := "**Bold text** and `code` and [link](url)"
		assert.Equal(t, input, sanitizeReviewBodyForMattermost(input))
	})
}

// mockPluginAPI type alias for the plugintest.API used in setupTestPlugin.
// This is needed because setupTestPlugin stores the mock as plugin.API interface.
type mockPluginAPI = plugintest.API

// --- Review Loop webhook tests ---

func TestWebhook_SynchronizeTriggersReviewLoop(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	// Active review loop for this PR.
	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseCursorFixing,
		Iteration:     2,
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		PRURL:         "https://github.com/org/repo/pull/42",
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(loop, nil)

	// handlePRSynchronize saves.
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseAwaitingReview
	})).Return(nil)

	// Inline status update: GetAgent -> GetPost -> UpdatePost.
	store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}, nil).Maybe()
	api.On("GetPost", "reply-1").Return(&model.Post{Id: "reply-1", ChannelId: "ch-1"}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil).Maybe()

	// WebSocket event for review loop phase change.
	api.On("PublishWebSocketEvent", "review_loop_changed", mock.Anything, mock.Anything).Return().Maybe()

	event := PullRequestEvent{
		Action: "synchronize",
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	event.PullRequest.Head.SHA = "newsha"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-sync").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-sync").Return(nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-sync", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_SynchronizeNoReviewLoop(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(nil, nil)

	event := PullRequestEvent{
		Action: "synchronize",
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-sync-noop").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-sync-noop").Return(nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-sync-noop", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Should not save anything.
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestWebhook_BotReviewTriggersReviewLoop(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	// Enable AI review loop config.
	p.configuration.EnableAIReviewLoop = true
	p.configuration.AIReviewerBots = "coderabbitai[bot]"

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseAwaitingReview,
		Iteration:     1,
		TriggerPostID: "trigger-1",
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		PRURL:         "https://github.com/org/repo/pull/42",
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(loop, nil)

	// CodeRabbit approved (via body text).
	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State: "commented",
			Body:  "Actionable comments posted: 0\nAll clean!",
		},
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	event.Review.User.Login = "coderabbitai[bot]"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-bot-review").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-bot-review").Return(nil)

	// handleAIReview will save with approved then human_review.
	store.On("SaveReviewLoop", mock.Anything).Return(nil)

	// Inline status update: GetAgent -> GetPost -> UpdatePost.
	store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}, nil).Maybe()
	api.On("GetPost", "reply-1").Return(&model.Post{Id: "reply-1", ChannelId: "ch-1"}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil).Maybe()

	// Completion post and reactions.
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "notif-1"}, nil)
	api.On("RemoveReaction", mock.Anything).Return(nil)
	api.On("AddReaction", mock.Anything).Return(nil, nil)

	// WebSocket event for review loop phase changes.
	api.On("PublishWebSocketEvent", "review_loop_changed", mock.Anything, mock.Anything).Return().Maybe()

	req := makeWebhookRequest(t, "pull_request_review", "delivery-bot-review", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertExpectations(t)
}

func TestWebhook_HumanReviewApproval(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	p.configuration.AIReviewerBots = "coderabbitai[bot]"
	p.configuration.EnableAIReviewLoop = true

	// Active review loop in human_review phase for this PR.
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
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(loop, nil)

	// handleHumanReviewApproval: save loop.
	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseComplete
	})).Return(nil)

	// Inline status update.
	store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}, nil).Maybe()
	api.On("GetPost", "reply-1").Return(&model.Post{Id: "reply-1", ChannelId: "ch-1"}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil).Maybe()

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

	// Also need agent lookup for normal notification (falls through).
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
		PrURL:         "https://github.com/org/repo/pull/42",
	}, nil)

	// Normal review notification post (green for approved).
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-1" && hasAttachmentWithColor(post, "#3DB887")
	})).Return(&model.Post{Id: "rv-1"}, nil).Maybe()

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "approved",
			HTMLURL: "https://github.com/org/repo/pull/42#pullrequestreview-1",
		},
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	event.Review.User.Login = "humandev"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-human-approval").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-human-approval").Return(nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-human-approval", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Verify the review loop was saved with complete phase.
	store.AssertCalled(t, "SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseComplete
	}))
}

func TestWebhook_HumanReview_Commented_IsInformationalOnly(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)
	cursorMock := p.cursorClient.(*mockCursorClient)

	p.configuration.AIReviewerBots = "coderabbitai[bot]"
	p.configuration.MaxReviewIterations = 5

	// Active review loop in human_review phase.
	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseHumanReview,
		Iteration:     2,
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
		PRURL:         "https://github.com/org/repo/pull/42",
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(loop, nil)

	// Agent lookup for normal review notification.
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
		PrURL:         "https://github.com/org/repo/pull/42",
	}, nil)

	// Comment review notification (blue for commented).
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-1" && hasAttachmentWithColor(post, "#2389D7")
	})).Return(&model.Post{Id: "rv-1"}, nil)

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "commented",
			Body:    "Needs a bit more work here.",
			HTMLURL: "https://github.com/org/repo/pull/42#pullrequestreview-2",
		},
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	event.PullRequest.Head.Ref = "cursor/fix-review-loop"
	event.PullRequest.Head.SHA = "human-sha-1"
	event.Review.User.Login = "humandev"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-human-comment").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-human-comment").Return(nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-human-comment", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertNotCalled(t, "SaveReviewLoop")
	cursorMock.AssertNotCalled(t, "AddFollowup", mock.Anything, mock.Anything, mock.Anything)
}

func TestWebhook_HumanReview_ChangesRequested_TriggersCursorFixing(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)
	mockGH := &mockGitHubClient{}
	cursorMock := p.cursorClient.(*mockCursorClient)
	p.githubClient = mockGH

	p.configuration.AIReviewerBots = "coderabbitai[bot]"
	p.configuration.MaxReviewIterations = 5

	loop := &kvstore.ReviewLoop{
		ID:            "loop-1",
		AgentRecordID: "agent-1",
		Phase:         kvstore.ReviewPhaseHumanReview,
		Iteration:     1,
		Owner:         "org",
		Repo:          "repo",
		PRNumber:      42,
		RootPostID:    "root-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
		PRURL:         "https://github.com/org/repo/pull/42",
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(loop, nil)

	mockGH.On("ListReviewComments", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestComment{
		{
			User: &github.User{Login: github.Ptr("humandev")},
			Path: github.Ptr("server/reviewloop.go"),
			Line: github.Ptr(88),
			Body: github.Ptr("Please move this to a helper and cover with tests."),
		},
	}, nil)
	mockGH.On("ListReviews", mock.Anything, "org", "repo", 42).Return([]*github.PullRequestReview{
		{
			User: &github.User{Login: github.Ptr("humandev")},
			Body: github.Ptr("Please move this to a helper and cover with tests."),
		},
	}, nil)
	mockGH.On("ListIssueComments", mock.Anything, "org", "repo", 42).Return([]*github.IssueComment{}, nil)
	cursorMock.On("AddFollowup", mock.Anything, "agent-1", mock.MatchedBy(func(req cursor.FollowupRequest) bool {
		return strings.Contains(req.Prompt.Text, "Please move this to a helper")
	})).Return(&cursor.FollowupResponse{ID: "agent-1"}, nil)

	store.On("SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseCursorFixing &&
			l.Iteration == 2 &&
			l.LastFeedbackDispatchSHA == "human-sha-2" &&
			len(l.History) > 0 &&
			strings.Contains(l.History[len(l.History)-1].Detail, "Human feedback iteration 2") &&
			strings.Contains(l.History[len(l.History)-1].Detail, "direct follow-up dispatched") &&
			strings.Contains(l.History[len(l.History)-1].Detail, "1 new, 0 repeated, 0 dismissed") &&
			l.LastFeedbackDigest != ""
	})).Return(nil)

	store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
		CursorAgentID:  "agent-1",
		BotReplyPostID: "reply-1",
		ChannelID:      "ch-1",
	}, nil).Maybe()
	api.On("GetPost", "reply-1").Return(&model.Post{Id: "reply-1", ChannelId: "ch-1"}, nil).Maybe()
	api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil).Maybe()
	api.On("PublishWebSocketEvent", "review_loop_changed", mock.Anything, mock.Anything).Return().Maybe()

	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
		PrURL:         "https://github.com/org/repo/pull/42",
	}, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.RootId == "root-1" && hasAttachmentWithColor(post, "#D24B4E")
	})).Return(&model.Post{Id: "rv-1"}, nil)

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "changes_requested",
			Body:    "Need one more refactor pass.",
			HTMLURL: "https://github.com/org/repo/pull/42#pullrequestreview-3",
		},
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	event.PullRequest.Head.Ref = "cursor/fix-review-loop"
	event.PullRequest.Head.SHA = "human-sha-2"
	event.Review.User.Login = "humandev"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-human-changes").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-human-changes").Return(nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-human-changes", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertCalled(t, "SaveReviewLoop", mock.MatchedBy(func(l *kvstore.ReviewLoop) bool {
		return l.Phase == kvstore.ReviewPhaseCursorFixing
	}))
	cursorMock.AssertExpectations(t)
}

func TestWebhook_HumanReview_AIBotReview_IsInformationalOnly(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	p.configuration.AIReviewerBots = "coderabbitai[bot]"

	loop := &kvstore.ReviewLoop{
		ID:    "loop-1",
		Phase: kvstore.ReviewPhaseHumanReview,
		PRURL: "https://github.com/org/repo/pull/42",
	}
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(loop, nil)

	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
		PrURL:         "https://github.com/org/repo/pull/42",
	}, nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{Id: "rv-1"}, nil)

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "commented",
			Body:    "AI note in human phase",
			HTMLURL: "https://github.com/org/repo/pull/42#pullrequestreview-4",
		},
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	event.Review.User.Login = "coderabbitai[bot]"
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-human-ai-info").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-human-ai-info").Return(nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-human-ai-info", body, sig)
	rr := httptest.NewRecorder()
	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	store.AssertNotCalled(t, "SaveReviewLoop")
}

func TestWebhook_AwaitingReview_HumanReview_IsInformationalOnly(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)
	api := p.API.(*mockPluginAPI)

	p.configuration.AIReviewerBots = "coderabbitai[bot]"

	loop := &kvstore.ReviewLoop{
		ID:    "loop-1",
		Phase: kvstore.ReviewPhaseAwaitingReview,
		PRURL: "https://github.com/org/repo/pull/42",
	}

	event := PullRequestReviewEvent{
		Action: "submitted",
		Review: ghReview{
			State:   "commented",
			Body:    "Human comment should be informational while awaiting AI gate.",
			HTMLURL: "https://github.com/org/repo/pull/42#pullrequestreview-1",
		},
		PullRequest: ghPullRequest{
			Number:  42,
			HTMLURL: "https://github.com/org/repo/pull/42",
		},
	}
	event.Review.User.Login = "human-dev" // Not an AI bot.

	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-human-rv").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-human-rv").Return(nil)
	store.On("GetReviewLoopByPRURL", "https://github.com/org/repo/pull/42").Return(loop, nil)
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/42").Return(&kvstore.AgentRecord{
		CursorAgentID: "agent-1",
		PostID:        "root-1",
		ChannelID:     "ch-1",
		PrURL:         "https://github.com/org/repo/pull/42",
	}, nil)

	// Normal review notification still posts.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-1" && hasAttachmentWithColor(p, "#2389D7")
	})).Return(&model.Post{Id: "rv-1"}, nil)

	req := makeWebhookRequest(t, "pull_request_review", "delivery-human-rv", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// Human reviews do not drive awaiting_review transitions.
	store.AssertNotCalled(t, "SaveReviewLoop")
}
