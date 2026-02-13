package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

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

	// Expect thread notification post.
	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-1" &&
			p.ChannelId == "ch-1" &&
			p.Message != "" &&
			containsSubstring(p.Message, "PR #42 has been merged")
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

	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return containsSubstring(p.Message, "PR #99 was closed")
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

func TestWebhook_PROpened_Ignored(t *testing.T) {
	p, store := setupWebhookTestPlugin(t)

	event := PullRequestEvent{
		Action: "opened",
		PullRequest: ghPullRequest{
			Number:  10,
			HTMLURL: "https://github.com/org/repo/pull/10",
		},
	}
	body, _ := json.Marshal(event)
	sig := signPayload(testWebhookSecret, body)

	store.On("HasDeliveryBeenProcessed", "delivery-pr-opened").Return(false, nil)
	store.On("MarkDeliveryProcessed", "delivery-pr-opened").Return(nil)

	req := makeWebhookRequest(t, "pull_request", "delivery-pr-opened", body, sig)
	rr := httptest.NewRecorder()

	p.handleGitHubWebhook(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	// No agent lookup should happen for non-closed actions.
	store.AssertNotCalled(t, "GetAgentByPRURL")
	store.AssertNotCalled(t, "GetAgentByBranch")
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
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/77").Return(agent, nil)

	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-rv" &&
			containsSubstring(p.Message, "PR #77 was approved") &&
			containsSubstring(p.Message, "reviewer-1")
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
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/88").Return(agent, nil)

	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-cr" &&
			containsSubstring(p.Message, "requested changes") &&
			containsSubstring(p.Message, "senior-dev") &&
			containsSubstring(p.Message, "error handling")
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
	store.On("GetAgentByPRURL", "https://github.com/org/repo/pull/66").Return(agent, nil)

	api.On("CreatePost", mock.MatchedBy(func(p *model.Post) bool {
		return p.RootId == "root-post-cm" &&
			containsSubstring(p.Message, "commented") &&
			containsSubstring(p.Message, "commenter") &&
			containsSubstring(p.Message, "nit")
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

// --- Test helpers ---

// containsSubstring is a test helper to check substrings.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mockPluginAPI type alias for the plugintest.API used in setupTestPlugin.
// This is needed because setupTestPlugin stores the mock as plugin.API interface.
type mockPluginAPI = plugintest.API
