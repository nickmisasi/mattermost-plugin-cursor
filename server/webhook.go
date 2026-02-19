package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

const (
	signatureHeaderSHA256 = "X-Hub-Signature-256"
	eventHeader           = "X-GitHub-Event"
	deliveryHeader        = "X-GitHub-Delivery"

	eventPullRequest       = "pull_request"
	eventPullRequestReview = "pull_request_review"
	eventPing              = "ping"

	prActionClosed      = "closed"
	prActionOpened      = "opened"
	prActionSynchronize = "synchronize"

	reviewActionSubmitted = "submitted"

	reviewStateApproved         = "approved"
	reviewStateChangesRequested = "changes_requested"
	reviewStateCommented        = "commented"

	// maxWebhookBodySize limits the body we read to prevent DoS.
	maxWebhookBodySize = 1 << 20 // 1 MB
)

// --- GitHub event payload types ---

// ghPullRequest represents the minimal PR fields we need from GitHub webhooks.
type ghPullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

// ghReview represents a PR review from GitHub webhooks.
type ghReview struct {
	State   string `json:"state"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
}

// ghRepository represents the minimal repo fields from GitHub webhooks.
type ghRepository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

// ghSender represents the user who triggered the webhook.
type ghSender struct {
	Login string `json:"login"`
}

// PullRequestEvent is the GitHub webhook payload for pull_request events.
type PullRequestEvent struct {
	Action      string        `json:"action"`
	PullRequest ghPullRequest `json:"pull_request"`
	Repository  ghRepository  `json:"repository"`
	Sender      ghSender      `json:"sender"`
}

// PullRequestReviewEvent is the GitHub webhook payload for pull_request_review events.
type PullRequestReviewEvent struct {
	Action      string        `json:"action"`
	Review      ghReview      `json:"review"`
	PullRequest ghPullRequest `json:"pull_request"`
	Repository  ghRepository  `json:"repository"`
	Sender      ghSender      `json:"sender"`
}

// PingEvent is the GitHub webhook payload for ping events (sent on webhook creation).
type PingEvent struct {
	Zen    string `json:"zen"`
	HookID int    `json:"hook_id"`
}

// --- HMAC-SHA256 verification ---

// verifyWebhookSignature validates the HMAC-SHA256 signature from GitHub.
func verifyWebhookSignature(secret []byte, signature string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}

	sigBytes, err := hex.DecodeString(signature[len(prefix):])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(sigBytes, expected)
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// --- Main webhook handler ---

func (p *Plugin) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()

	// 1. Read the body with size limit.
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// 2. Verify HMAC signature.
	secret := config.GitHubWebhookSecret
	if secret == "" {
		p.API.LogWarn("GitHub webhook received but GitHubWebhookSecret is not configured")
		http.Error(w, "webhook secret not configured", http.StatusInternalServerError)
		return
	}

	signature := r.Header.Get(signatureHeaderSHA256)
	if !verifyWebhookSignature([]byte(secret), signature, body) {
		p.API.LogWarn("GitHub webhook signature verification failed")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// 3. Idempotency: check delivery ID.
	deliveryID := r.Header.Get(deliveryHeader)
	if deliveryID != "" {
		seen, _ := p.kvstore.HasDeliveryBeenProcessed(deliveryID)
		if seen {
			p.API.LogDebug("Duplicate GitHub webhook delivery, skipping", "delivery", deliveryID)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// 4. Route by event type, recording the response status.
	sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	eventType := r.Header.Get(eventHeader)
	p.API.LogDebug("GitHub webhook received", "event", eventType, "delivery", deliveryID)

	switch eventType {
	case eventPing:
		p.handlePingEvent(sr, body)
	case eventPullRequest:
		p.handlePullRequestEvent(sr, body)
	case eventPullRequestReview:
		p.handlePullRequestReviewEvent(sr, body)
	default:
		p.API.LogDebug("Ignoring unhandled GitHub event type", "event", eventType)
		sr.WriteHeader(http.StatusOK)
	}

	// 5. Mark delivery as processed only after successful handling.
	if deliveryID != "" && sr.status >= 200 && sr.status < 300 {
		_ = p.kvstore.MarkDeliveryProcessed(deliveryID)
	}
}

// --- Event handlers ---

func (p *Plugin) handlePingEvent(w http.ResponseWriter, body []byte) {
	var event PingEvent
	if err := json.Unmarshal(body, &event); err != nil {
		p.API.LogWarn("Failed to parse ping event", "error", err.Error())
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	p.API.LogInfo("GitHub webhook ping received",
		"zen", event.Zen,
		"hook_id", fmt.Sprintf("%d", event.HookID),
	)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (p *Plugin) handlePullRequestEvent(w http.ResponseWriter, body []byte) {
	var event PullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		p.API.LogWarn("Failed to parse pull_request event", "error", err.Error())
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Route by action.
	switch event.Action {
	case prActionSynchronize:
		p.handlePRSynchronizeWebhook(event, w)
		return
	case prActionOpened:
		p.handlePROpened(event, w)
		return
	case prActionClosed:
		// Fall through to existing closed handling below.
	default:
		w.WriteHeader(http.StatusOK)
		return
	}

	// Look up the agent associated with this PR.
	agent := p.findAgentForPR(event.PullRequest)
	if agent == nil {
		p.API.LogDebug("No agent found for PR", "pr_url", event.PullRequest.HTMLURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	prTitle := fmt.Sprintf("PR #%d: %s", event.PullRequest.Number, event.PullRequest.Title)

	if event.PullRequest.Merged {
		mergedAttachment := &model.SlackAttachment{
			Color:     "#3DB887", // green
			Title:     prTitle,
			TitleLink: event.PullRequest.HTMLURL,
			Text:      "This pull request has been merged.",
		}
		p.postThreadNotificationWithAttachment(agent, mergedAttachment)
	} else {
		closedAttachment := &model.SlackAttachment{
			Color:     "#8B8FA7", // grey
			Title:     prTitle,
			TitleLink: event.PullRequest.HTMLURL,
			Text:      "This pull request was closed without merging.",
		}
		p.postThreadNotificationWithAttachment(agent, closedAttachment)
	}

	// Update reaction on the trigger post for merged PRs.
	if event.PullRequest.Merged {
		p.swapReaction(agent.TriggerPostID, "white_check_mark", "rocket")
	}

	// Update agent status in KV store.
	if event.PullRequest.Merged {
		agent.Status = "MERGED"
	} else {
		agent.Status = "PR_CLOSED"
	}
	_ = p.kvstore.SaveAgent(agent)

	w.WriteHeader(http.StatusOK)
}

// handlePRSynchronizeWebhook handles the synchronize action (new commits pushed) for a PR.
// If the PR has an active review loop in the cursor_fixing phase, it triggers re-review.
func (p *Plugin) handlePRSynchronizeWebhook(event PullRequestEvent, w http.ResponseWriter) {
	loop, err := p.kvstore.GetReviewLoopByPRURL(event.PullRequest.HTMLURL)
	if err != nil {
		p.API.LogError("Failed to look up review loop for synchronize event",
			"error", err.Error(),
			"pr_url", event.PullRequest.HTMLURL,
		)
		w.WriteHeader(http.StatusOK)
		return
	}

	if loop == nil || loop.Phase != kvstore.ReviewPhaseCursorFixing {
		// No active review loop or not in cursor_fixing phase -- ignore.
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := p.handlePRSynchronize(loop, event.PullRequest); err != nil {
		p.API.LogError("Failed to handle PR synchronize for review loop",
			"error", err.Error(),
			"review_loop_id", loop.ID,
		)
	}

	w.WriteHeader(http.StatusOK)
}

// handlePROpened handles a newly opened PR. This is the PRIMARY path for:
// 1. Linking a PR to an agent (backfilling PrURL)
// 2. Starting the AI review loop
// 3. Posting a PR notification in the agent's thread
func (p *Plugin) handlePROpened(event PullRequestEvent, w http.ResponseWriter) {
	agent := p.findAgentForPR(event.PullRequest)
	if agent == nil {
		p.API.LogDebug("No agent found for opened PR", "pr_url", event.PullRequest.HTMLURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	prURL := event.PullRequest.HTMLURL
	changed := false

	// Step 1: Backfill PrURL if empty.
	if agent.PrURL == "" {
		agent.PrURL = prURL
		changed = true
	}

	// Step 2: Backfill TargetBranch if empty.
	if agent.TargetBranch == "" && event.PullRequest.Head.Ref != "" {
		agent.TargetBranch = event.PullRequest.Head.Ref
		changed = true
	}

	if changed {
		agent.UpdatedAt = time.Now().UnixMilli()
		if err := p.kvstore.SaveAgent(agent); err != nil {
			p.API.LogError("Failed to backfill agent from PR opened webhook",
				"error", err.Error(),
				"agent_id", agent.CursorAgentID,
				"pr_url", prURL,
			)
		}
		// Publish WebSocket event so RHS updates immediately.
		p.publishAgentStatusChange(agent)
	}

	// Step 3: Post PR notification in thread.
	prTitle := fmt.Sprintf("PR #%d: %s", event.PullRequest.Number, event.PullRequest.Title)
	prAttachment := &model.SlackAttachment{
		Color:     "#2389D7", // blue
		Title:     prTitle,
		TitleLink: prURL,
		Text:      fmt.Sprintf("Pull request opened on branch `%s`.", event.PullRequest.Head.Ref),
	}
	p.postThreadNotificationWithAttachment(agent, prAttachment)

	// Step 4: Start review loop if agent is FINISHED and review loop is enabled.
	// If agent is still RUNNING, the poller will handle it when it detects FINISHED.
	if cursor.AgentStatus(agent.Status).IsTerminal() &&
		p.getConfiguration().EnableAIReviewLoop &&
		p.getGitHubClient() != nil {
		if err := p.startReviewLoop(agent); err != nil {
			p.API.LogError("Failed to start review loop from PR opened webhook",
				"error", err.Error(),
				"agent_id", agent.CursorAgentID,
				"pr_url", prURL,
			)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (p *Plugin) handlePullRequestReviewEvent(w http.ResponseWriter, body []byte) {
	var event PullRequestReviewEvent
	if err := json.Unmarshal(body, &event); err != nil {
		p.API.LogWarn("Failed to parse pull_request_review event", "error", err.Error())
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Only handle submitted reviews (not edited/dismissed).
	if event.Action != reviewActionSubmitted {
		w.WriteHeader(http.StatusOK)
		return
	}

	// --- Review Loop phase-aware gating ---
	reviewerType := p.reviewerTypeForLogin(event.Review.User.Login)
	loop := p.ensureReviewLoop(event.PullRequest.HTMLURL)
	if loop != nil {
		switch loop.Phase {
		case kvstore.ReviewPhaseAwaitingReview:
			// AI reviews in awaiting_review drive CodeRabbit gate + fix iterations.
			if reviewerType == reviewerTypeAIBot {
				if err := p.handleAIReview(loop, event.Review, event.PullRequest); err != nil {
					p.API.LogError("Failed to handle AI review",
						"error", err.Error(),
						"review_loop_id", loop.ID,
					)
				}
				w.WriteHeader(http.StatusOK)
				return
			}
		case kvstore.ReviewPhaseHumanReview:
			// Human approval is terminal; human commented/changes_requested now
			// trigger another cursor_fixing iteration.
			if reviewerType == reviewerTypeHuman {
				if strings.EqualFold(event.Review.State, reviewStateApproved) {
					if err := p.handleHumanReviewApproval(loop, event.Review.User.Login); err != nil {
						p.API.LogError("Failed to handle human review approval",
							"error", err.Error(),
							"review_loop_id", loop.ID,
						)
					}
				} else if strings.EqualFold(event.Review.State, reviewStateCommented) ||
					strings.EqualFold(event.Review.State, reviewStateChangesRequested) {
					if err := p.handleHumanReviewFeedback(loop, event.Review, event.PullRequest); err != nil {
						p.API.LogError("Failed to handle human review feedback",
							"error", err.Error(),
							"review_loop_id", loop.ID,
						)
					}
				}
			}
		}
	}

	// --- Existing: Look up the agent and post notification ---
	agent := p.findAgentForPR(event.PullRequest)
	if agent == nil {
		p.API.LogDebug("No agent found for reviewed PR", "pr_url", event.PullRequest.HTMLURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Backfill PrURL if empty (agent may have finished before PR was linked).
	if agent.PrURL == "" && event.PullRequest.HTMLURL != "" {
		agent.PrURL = event.PullRequest.HTMLURL
		agent.UpdatedAt = time.Now().UnixMilli()
		_ = p.kvstore.SaveAgent(agent)
		p.publishAgentStatusChange(agent)
	}

	reviewer := event.Review.User.Login
	prNumber := event.PullRequest.Number
	reviewURL := event.Review.HTMLURL

	prTitle := fmt.Sprintf("PR #%d", prNumber)

	var reviewAttachment *model.SlackAttachment

	switch event.Review.State {
	case reviewStateApproved:
		reviewAttachment = &model.SlackAttachment{
			Color:     "#3DB887", // green
			Title:     fmt.Sprintf("%s approved by %s", prTitle, reviewer),
			TitleLink: reviewURL,
		}
	case reviewStateChangesRequested:
		bodyText := truncateText(sanitizeReviewBodyForMattermost(event.Review.Body), 200)
		reviewAttachment = &model.SlackAttachment{
			Color:     "#D24B4E", // red (changes requested)
			Title:     fmt.Sprintf("%s: %s requested changes", prTitle, reviewer),
			TitleLink: reviewURL,
			Text:      bodyText,
		}
	case reviewStateCommented:
		bodyText := truncateText(sanitizeReviewBodyForMattermost(event.Review.Body), 200)
		if bodyText == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		reviewAttachment = &model.SlackAttachment{
			Color:     "#2389D7", // blue
			Title:     fmt.Sprintf("%s: %s commented", prTitle, reviewer),
			TitleLink: reviewURL,
			Text:      bodyText,
		}
	default:
		p.API.LogDebug("Unhandled review state", "state", event.Review.State)
		w.WriteHeader(http.StatusOK)
		return
	}

	p.postThreadNotificationWithAttachment(agent, reviewAttachment)

	w.WriteHeader(http.StatusOK)
}

// --- Agent lookup ---

// findAgentForPR looks up a Cursor agent record associated with the given PR.
// It tries multiple lookup strategies in order of specificity.
func (p *Plugin) findAgentForPR(pr ghPullRequest) *kvstore.AgentRecord {
	// Strategy 1: Lookup by exact PR URL.
	if pr.HTMLURL != "" {
		agent, err := p.kvstore.GetAgentByPRURL(pr.HTMLURL)
		if err == nil && agent != nil {
			return agent
		}
	}

	// Strategy 2: Lookup by head branch name.
	if pr.Head.Ref != "" {
		agent, err := p.kvstore.GetAgentByBranch(pr.Head.Ref)
		if err == nil && agent != nil {
			return agent
		}
	}

	return nil
}

// --- Helpers ---

// postThreadNotificationWithAttachment posts a SlackAttachment in the agent's Mattermost thread.
func (p *Plugin) postThreadNotificationWithAttachment(agent *kvstore.AgentRecord, attachment *model.SlackAttachment) {
	if agent.PostID == "" {
		p.API.LogWarn("Cannot post thread notification: no root post ID",
			"agent_id", agent.CursorAgentID)
		return
	}

	post := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: agent.ChannelID,
		RootId:    agent.PostID,
	}
	model.ParseSlackAttachment(post, []*model.SlackAttachment{attachment})

	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogError("Failed to post GitHub notification attachment in thread",
			"error", appErr.Error(),
			"agent_id", agent.CursorAgentID,
			"root_post_id", agent.PostID,
		)
	}
}

// swapReaction removes one reaction and adds another on the trigger post.
func (p *Plugin) swapReaction(postID, removeEmoji, addEmoji string) {
	if postID == "" {
		return
	}
	p.removeReaction(postID, removeEmoji)
	p.addReaction(postID, addEmoji)
}

// sanitizeReviewBodyForMattermost converts common HTML tags (from tools like
// CodeRabbit) into Markdown equivalents suitable for Mattermost posts.
func sanitizeReviewBodyForMattermost(body string) string {
	// Remove <details> and </details> tags.
	body = regexp.MustCompile(`(?i)</?details>`).ReplaceAllString(body, "")

	// Convert <summary>text</summary> to **text**.
	body = regexp.MustCompile(`(?i)<summary>(.*?)</summary>`).ReplaceAllString(body, "**$1**")

	// Convert <blockquote>text</blockquote> to > quoted lines.
	body = regexp.MustCompile(`(?is)<blockquote>(.*?)</blockquote>`).ReplaceAllStringFunc(body, func(match string) string {
		inner := regexp.MustCompile(`(?is)<blockquote>(.*?)</blockquote>`).FindStringSubmatch(match)
		if len(inner) > 1 {
			lines := strings.Split(strings.TrimSpace(inner[1]), "\n")
			for i, l := range lines {
				lines[i] = "> " + strings.TrimSpace(l)
			}
			return strings.Join(lines, "\n")
		}
		return match
	})

	// Strip remaining HTML tags.
	body = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(body, "")

	// Clean up excessive blank lines.
	body = regexp.MustCompile(`\n{3,}`).ReplaceAllString(body, "\n\n")

	return strings.TrimSpace(body)
}

// truncateText truncates a string to maxLen characters, appending "..." if truncated.
func truncateText(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
