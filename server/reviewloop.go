package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/google/uuid"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/attachments"
	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/ghclient"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// ensureReviewLoop returns the existing ReviewLoop for the given PR URL,
// or bootstraps a new one if the agent is in a terminal state and the
// review loop feature is enabled. Returns nil if no loop can be created.
func (p *Plugin) ensureReviewLoop(prURL string) *kvstore.ReviewLoop {
	// Check for existing loop first.
	loop, err := p.kvstore.GetReviewLoopByPRURL(prURL)
	if err != nil {
		p.API.LogError("Failed to look up review loop", "error", err.Error(), "pr_url", prURL)
		return nil
	}
	if loop != nil {
		return loop
	}

	// No loop exists. Try to bootstrap from the agent record.
	if !p.getConfiguration().EnableAIReviewLoop || p.getGitHubClient() == nil {
		return nil
	}

	agent, err := p.kvstore.GetAgentByPRURL(prURL)
	if err != nil || agent == nil {
		return nil
	}

	if !cursor.AgentStatus(agent.Status).IsTerminal() {
		return nil // Agent not done yet; loop will start when it finishes.
	}

	if err := p.startReviewLoop(agent); err != nil {
		p.API.LogError("Failed to bootstrap review loop from review webhook",
			"error", err.Error(),
			"agent_id", agent.CursorAgentID,
			"pr_url", prURL,
		)
		return nil
	}

	// Refetch the freshly-created loop.
	loop, _ = p.kvstore.GetReviewLoopByPRURL(prURL)
	return loop
}

// startReviewLoop creates a ReviewLoop record and requests AI reviewers on the PR.
// Called from handleAgentFinished when EnableAIReviewLoop is true and the agent has a PR URL.
func (p *Plugin) startReviewLoop(record *kvstore.AgentRecord) error {
	prRef, err := ghclient.ParsePRURL(record.PrURL)
	if err != nil {
		return fmt.Errorf("failed to parse PR URL %q: %w", record.PrURL, err)
	}

	// Idempotency: check for existing review loop for this PR.
	existing, _ := p.kvstore.GetReviewLoopByPRURL(record.PrURL)
	if existing != nil {
		p.API.LogDebug("Review loop already exists for PR, skipping", "pr_url", record.PrURL, "review_loop_id", existing.ID)
		return nil
	}

	now := time.Now().UnixMilli()
	loop := &kvstore.ReviewLoop{
		ID:            uuid.New().String(),
		AgentRecordID: record.CursorAgentID,
		UserID:        record.UserID,
		ChannelID:     record.ChannelID,
		RootPostID:    record.PostID,
		TriggerPostID: record.TriggerPostID,
		PRURL:         record.PrURL,
		PRNumber:      prRef.Number,
		Repository:    prRef.Owner + "/" + prRef.Repo,
		Owner:         prRef.Owner,
		Repo:          prRef.Repo,
		Phase:         kvstore.ReviewPhaseRequestingReview,
		Iteration:     1,
		History: []kvstore.ReviewLoopEvent{
			{Phase: kvstore.ReviewPhaseRequestingReview, Timestamp: now},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Check for HITL workflow linkage.
	workflowID, _ := p.kvstore.GetWorkflowByAgent(record.CursorAgentID)
	if workflowID != "" {
		loop.WorkflowID = workflowID
	}

	if err := p.kvstore.SaveReviewLoop(loop); err != nil {
		return fmt.Errorf("failed to save review loop: %w", err)
	}

	// Mark the PR as ready for review. Cursor creates PRs as drafts,
	// and AI reviewers (e.g., CodeRabbit) skip draft PRs. If this fails,
	// we abort and leave the loop in requesting_review so the janitor can
	// retry on the next sweep.
	ghClient := p.getGitHubClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := ghClient.MarkPRReadyForReview(ctx, prRef.Owner, prRef.Repo, prRef.Number); err != nil {
		p.API.LogError("Failed to mark PR as ready for review; review loop will retry",
			"error", err.Error(),
			"pr_url", record.PrURL,
		)
		// Delete the loop record so the janitor can re-bootstrap it cleanly.
		_ = p.kvstore.DeleteReviewLoop(loop.ID)
		return fmt.Errorf("failed to mark PR as ready for review: %w", err)
	}

	// Request AI reviewers via GitHub API (optional -- bots like CodeRabbit
	// auto-detect PRs, so this is a best-effort nudge).
	config := p.getConfiguration()
	botUsernames := config.ParseAIReviewerBots()
	if len(botUsernames) == 0 {
		p.API.LogInfo("No AI reviewer bots configured, skipping explicit review request")
	} else {
		err := ghClient.RequestReviewers(ctx, prRef.Owner, prRef.Repo, prRef.Number, github.ReviewersRequest{
			Reviewers: botUsernames,
		})
		if err != nil {
			p.API.LogWarn("Failed to request AI reviewers (non-fatal, bots may auto-detect the PR)",
				"error", err.Error(),
				"pr_url", record.PrURL,
				"reviewers", strings.Join(botUsernames, ", "),
			)
			// Non-fatal: bots like CodeRabbit auto-detect new PRs.
		}
	}

	// Transition to awaiting_review.
	loop.Phase = kvstore.ReviewPhaseAwaitingReview
	loop.History = append(loop.History, kvstore.ReviewLoopEvent{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Timestamp: time.Now().UnixMilli(),
		Detail:    reviewLoopAwaitDetail(botUsernames),
	})
	loop.UpdatedAt = time.Now().UnixMilli()
	if err := p.kvstore.SaveReviewLoop(loop); err != nil {
		return fmt.Errorf("failed to update review loop phase: %w", err)
	}

	// Update the "Agent finished!" card with review status.
	p.updateReviewLoopInlineStatus(loop)
	p.publishReviewLoopChange(loop)

	// Add eyes reaction on trigger post.
	p.addReaction(loop.TriggerPostID, "eyes")

	return nil
}

const (
	reviewDispatchModeDirect            = "direct"
	reviewDispatchModeSkippedIdempotent = "skipped_idempotent"
	reviewDispatchModeFailed            = "failed"

	reviewDispatchReasonDirectSuccess       = "direct_success"
	reviewDispatchReasonIdempotentSameState = "idempotent_same_sha_digest"
	reviewDispatchReasonDirectFailed        = "direct_failed"
	reviewDispatchReasonCursorClientNil     = "cursor_client_nil"
	reviewDispatchReasonAddFollowupError    = "add_followup_error"

	reviewFeedbackDropReasonUnknown = "unknown_drop_reason"
)

type reviewDispatchOutcome struct {
	Dispatched bool
	Skipped    bool
	Failed     bool
	Mode       string
	Counts     reviewFeedbackClassificationSummary
}

func formatReviewDispatchHistoryDetail(baseDetail, modeLabel string, counts reviewFeedbackClassificationSummary) string {
	countSummary := formatReviewFeedbackCountSummary(counts.New, counts.Repeated, counts.Dismissed)
	if modeLabel == "" {
		return fmt.Sprintf("%s (%s)", baseDetail, countSummary)
	}
	return fmt.Sprintf("%s (%s; %s)", baseDetail, modeLabel, countSummary)
}

// handleAIReview processes a submitted review from a known AI reviewer bot.
// It checks whether CodeRabbit is satisfied and either transitions to approved
// or dispatches follow-up feedback.
func (p *Plugin) handleAIReview(loop *kvstore.ReviewLoop, review ghReview, pr ghPullRequest) error {
	isCodeRabbit := strings.EqualFold(review.User.Login, codeRabbitReviewerLogin)
	codeRabbitSatisfied := false

	if isCodeRabbit {
		// Primary signal: review state is APPROVED.
		if strings.EqualFold(review.State, reviewStateApproved) {
			codeRabbitSatisfied = true
		}
		// Fallback signal: body contains "Actionable comments posted: 0".
		if !codeRabbitSatisfied && strings.Contains(review.Body, "Actionable comments posted: 0") {
			codeRabbitSatisfied = true
		}
	}

	// If CodeRabbit is satisfied, transition to approved.
	if codeRabbitSatisfied {
		loop.Phase = kvstore.ReviewPhaseApproved
		loop.History = append(loop.History, kvstore.ReviewLoopEvent{
			Phase:     kvstore.ReviewPhaseApproved,
			Timestamp: time.Now().UnixMilli(),
			Detail:    fmt.Sprintf("Approved after %d iteration(s)", loop.Iteration),
		})
		loop.UpdatedAt = time.Now().UnixMilli()
		if err := p.kvstore.SaveReviewLoop(loop); err != nil {
			return fmt.Errorf("failed to save approved review loop: %w", err)
		}

		p.updateReviewLoopInlineStatus(loop)
		p.publishReviewLoopChange(loop)
		p.postReviewLoopCompletion(loop, attachments.BuildReviewApprovedAttachment(
			loop.PRURL,
			loop.Iteration,
		))
		p.swapReaction(loop.TriggerPostID, "eyes", "white_check_mark")

		return p.transitionToHumanReview(loop)
	}

	// If CodeRabbit has actionable feedback (not satisfied AND is CodeRabbit).
	if isCodeRabbit {
		// Check iteration limit.
		config := p.getConfiguration()
		if loop.Iteration >= config.MaxReviewIterations {
			loop.Phase = kvstore.ReviewPhaseMaxIterations
			loop.History = append(loop.History, kvstore.ReviewLoopEvent{
				Phase:     kvstore.ReviewPhaseMaxIterations,
				Timestamp: time.Now().UnixMilli(),
				Detail:    fmt.Sprintf("Reached max iterations (%d)", config.MaxReviewIterations),
			})
			loop.UpdatedAt = time.Now().UnixMilli()
			_ = p.kvstore.SaveReviewLoop(loop)

			p.updateReviewLoopInlineStatus(loop)
			p.publishReviewLoopChange(loop)
			p.postReviewLoopCompletion(loop, attachments.BuildMaxIterationsAttachment(
				loop.PRURL,
				config.MaxReviewIterations,
			))
			p.swapReaction(loop.TriggerPostID, "eyes", "warning")
			return nil
		}

		if pr.Head.SHA != "" {
			loop.LastCommitSHA = pr.Head.SHA
		}

		outcome, err := p.dispatchReviewFeedback(loop, pr)
		if err != nil {
			p.API.LogError("Failed to dispatch AI review feedback",
				"error", err.Error(),
				"review_loop_id", loop.ID,
			)
			return err
		}

		if outcome.Skipped || outcome.Failed {
			if err := p.kvstore.SaveReviewLoop(loop); err != nil {
				return fmt.Errorf("failed to save review loop after dispatch outcome: %w", err)
			}
			p.publishReviewLoopChange(loop)
			return nil
		}
		if !outcome.Dispatched {
			return nil
		}

		detail := formatReviewDispatchHistoryDetail(
			fmt.Sprintf("Iteration %d", loop.Iteration+1),
			"",
			outcome.Counts,
		)
		if outcome.Mode == reviewDispatchModeDirect {
			detail = formatReviewDispatchHistoryDetail(
				fmt.Sprintf("Iteration %d", loop.Iteration+1),
				"direct follow-up dispatched",
				outcome.Counts,
			)
		}

		loop.Phase = kvstore.ReviewPhaseCursorFixing
		loop.Iteration++
		loop.History = append(loop.History, kvstore.ReviewLoopEvent{
			Phase:     kvstore.ReviewPhaseCursorFixing,
			Timestamp: time.Now().UnixMilli(),
			Detail:    detail,
		})
		loop.UpdatedAt = time.Now().UnixMilli()
		if err := p.kvstore.SaveReviewLoop(loop); err != nil {
			return fmt.Errorf("failed to save review loop: %w", err)
		}

		p.updateReviewLoopInlineStatus(loop)
		p.publishReviewLoopChange(loop)
		return nil
	}

	// Non-CodeRabbit bot reviews are informational only.
	p.API.LogDebug("Non-CodeRabbit AI review received, not driving state transition",
		"reviewer", review.User.Login,
		"review_loop_id", loop.ID,
	)
	return nil
}

// handlePRSynchronize processes a push to a PR with an active review loop.
// Transitions from cursor_fixing -> awaiting_review to trigger re-review.
func (p *Plugin) handlePRSynchronize(loop *kvstore.ReviewLoop, pr ghPullRequest) error {
	if pr.Head.SHA != "" {
		loop.LastCommitSHA = pr.Head.SHA
	}

	loop.Phase = kvstore.ReviewPhaseAwaitingReview
	loop.History = append(loop.History, kvstore.ReviewLoopEvent{
		Phase:     kvstore.ReviewPhaseAwaitingReview,
		Timestamp: time.Now().UnixMilli(),
		Detail:    "Cursor pushed fixes",
	})
	loop.UpdatedAt = time.Now().UnixMilli()

	if err := p.kvstore.SaveReviewLoop(loop); err != nil {
		return fmt.Errorf("failed to save review loop: %w", err)
	}

	p.updateReviewLoopInlineStatus(loop)
	p.publishReviewLoopChange(loop)
	return nil
}

func (p *Plugin) dispatchReviewFeedback(loop *kvstore.ReviewLoop, pr ghPullRequest) (reviewDispatchOutcome, error) {
	classification, telemetry, _, err := p.collectReviewFeedbackBundle(loop)
	if err != nil {
		return reviewDispatchOutcome{}, fmt.Errorf("failed to collect review feedback: %w", err)
	}

	counts := telemetry.Counts
	dispatchSHA := strings.TrimSpace(pr.Head.SHA)
	if dispatchSHA == "" {
		dispatchSHA = strings.TrimSpace(loop.LastCommitSHA)
	}
	dispatchDigest := reviewFeedbackDigest(classification.Dispatchable)
	lastDispatchSHA := loop.LastFeedbackDispatchSHA
	lastDispatchDigest := loop.LastFeedbackDigest

	p.logReviewFeedbackCollectionSummary(loop, dispatchSHA, telemetry)

	if loop.LastFeedbackDispatchAt > 0 &&
		dispatchSHA == loop.LastFeedbackDispatchSHA &&
		dispatchDigest == loop.LastFeedbackDigest {
		loop.History = append(loop.History, kvstore.ReviewLoopEvent{
			Phase:     loop.Phase,
			Timestamp: time.Now().UnixMilli(),
			Detail: fmt.Sprintf(
				"Skipped duplicate review feedback dispatch (same SHA and digest; %s)",
				formatReviewFeedbackCountSummary(counts.New, counts.Repeated, counts.Dismissed),
			),
		})
		loop.UpdatedAt = time.Now().UnixMilli()

		p.logReviewFeedbackDispatchDecision(
			loop,
			reviewDispatchModeSkippedIdempotent,
			reviewDispatchReasonIdempotentSameState,
			dispatchSHA,
			dispatchDigest,
			lastDispatchSHA,
			lastDispatchDigest,
			counts,
			"",
		)

		return reviewDispatchOutcome{
			Skipped: true,
			Mode:    reviewDispatchModeSkippedIdempotent,
			Counts:  counts,
		}, nil
	}

	followupPrompt := formatFindingsForCursorFollowup(loop, pr, classification.Dispatchable)
	if strings.TrimSpace(followupPrompt) == "" {
		followupPrompt = defaultReviewLoopFeedbackText()
	}

	var primaryErr error
	decisionReason := reviewDispatchReasonDirectFailed
	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		decisionReason = reviewDispatchReasonCursorClientNil
		primaryErr = fmt.Errorf("cursor client is not configured")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_, primaryErr = cursorClient.AddFollowup(ctx, loop.AgentRecordID, cursor.FollowupRequest{
			Prompt: cursor.Prompt{
				Text: followupPrompt,
			},
		})
		if primaryErr != nil {
			decisionReason = reviewDispatchReasonAddFollowupError
		}
	}

	if primaryErr == nil {
		applyReviewFeedbackDispatchTracking(loop, dispatchSHA, dispatchDigest)

		p.logReviewFeedbackDispatchDecision(
			loop,
			reviewDispatchModeDirect,
			reviewDispatchReasonDirectSuccess,
			dispatchSHA,
			dispatchDigest,
			lastDispatchSHA,
			lastDispatchDigest,
			counts,
			"",
		)

		return reviewDispatchOutcome{
			Dispatched: true,
			Mode:       reviewDispatchModeDirect,
			Counts:     counts,
		}, nil
	}

	loop.History = append(loop.History, kvstore.ReviewLoopEvent{
		Phase:     loop.Phase,
		Timestamp: time.Now().UnixMilli(),
		Detail: fmt.Sprintf(
			"Failed to dispatch review feedback; manual intervention required (%s)",
			formatReviewFeedbackCountSummary(counts.New, counts.Repeated, counts.Dismissed),
		),
	})
	loop.UpdatedAt = time.Now().UnixMilli()

	errorPrimary := primaryErr.Error()
	if decisionReason == "" {
		decisionReason = reviewDispatchReasonDirectFailed
	}

	p.logReviewFeedbackDispatchDecision(
		loop,
		reviewDispatchModeFailed,
		decisionReason,
		dispatchSHA,
		dispatchDigest,
		lastDispatchSHA,
		lastDispatchDigest,
		counts,
		errorPrimary,
	)

	return reviewDispatchOutcome{
		Failed: true,
		Mode:   reviewDispatchModeFailed,
		Counts: counts,
	}, nil
}

func applyReviewFeedbackDispatchTracking(loop *kvstore.ReviewLoop, dispatchSHA, dispatchDigest string) {
	now := time.Now().UnixMilli()
	loop.LastFeedbackDispatchAt = now
	loop.LastFeedbackDispatchSHA = dispatchSHA
	loop.LastFeedbackDigest = dispatchDigest
	loop.FeedbackCursor = fmt.Sprintf("%d", now)
}

func (p *Plugin) logReviewFeedbackCollectionSummary(loop *kvstore.ReviewLoop, dispatchSHA string, telemetry reviewFeedbackTelemetry) {
	p.logDebug("Review feedback collection summary",
		"review_loop_id", loop.ID,
		"agent_record_id", loop.AgentRecordID,
		"phase", loop.Phase,
		"iteration", loop.Iteration,
		"pr_url", loop.PRURL,
		"dispatch_sha", dispatchSHA,
		"candidate_total", telemetry.Source.Total,
		"candidate_review_comment", telemetry.Source.ReviewComment,
		"candidate_review_body", telemetry.Source.ReviewBody,
		"candidate_issue_comment", telemetry.Source.IssueComment,
		"candidate_ai_bot", telemetry.Source.AIBot,
		"candidate_human", telemetry.Source.Human,
		"new_count", telemetry.Counts.New,
		"repeated_count", telemetry.Counts.Repeated,
		"resolved_count", telemetry.Counts.Resolved,
		"superseded_count", telemetry.Counts.Superseded,
		"dismissed_count", telemetry.Counts.Dismissed,
		"dispatchable_count", telemetry.Counts.Dispatchable,
	)
}

func (p *Plugin) logReviewFeedbackCandidateDropped(
	loop *kvstore.ReviewLoop,
	candidate reviewFeedbackCandidate,
	route reviewerExtractionRoute,
	dropReason string,
) {
	if strings.TrimSpace(dropReason) == "" {
		dropReason = reviewFeedbackDropReasonUnknown
	}

	p.logDebug("Review feedback candidate dropped",
		"review_loop_id", loop.ID,
		"agent_record_id", loop.AgentRecordID,
		"phase", loop.Phase,
		"iteration", loop.Iteration,
		"pr_url", loop.PRURL,
		"extraction_route", string(route),
		"drop_reason", dropReason,
		"candidate_source_type", candidate.SourceType,
		"candidate_source_id", candidate.SourceID,
		"candidate_reviewer_login", candidate.ReviewerLogin,
		"candidate_reviewer_type", candidate.ReviewerType,
		"candidate_path", candidate.Path,
		"candidate_line", candidate.Line,
		"candidate_commit_sha", candidate.CommitSHA,
		"candidate_raw_text_len", len(candidate.RawText),
		"candidate_normalized_text_len", len(candidate.NormalizedText),
	)
}

func (p *Plugin) logReviewFeedbackDispatchDecision(
	loop *kvstore.ReviewLoop,
	dispatchMode string,
	decisionReason string,
	dispatchSHA string,
	dispatchDigest string,
	lastDispatchSHA string,
	lastDispatchDigest string,
	counts reviewFeedbackClassificationSummary,
	errorPrimary string,
) {
	debugFields := []any{
		"review_loop_id", loop.ID,
		"agent_record_id", loop.AgentRecordID,
		"phase", loop.Phase,
		"iteration", loop.Iteration,
		"dispatch_mode", dispatchMode,
		"decision_reason", decisionReason,
		"dispatch_sha", dispatchSHA,
		"dispatch_digest", dispatchDigest,
		"last_dispatch_sha", lastDispatchSHA,
		"last_dispatch_digest", lastDispatchDigest,
		"new_count", counts.New,
		"repeated_count", counts.Repeated,
		"dismissed_count", counts.Dismissed,
		"dispatchable_count", counts.Dispatchable,
	}
	if errorPrimary != "" {
		debugFields = append(debugFields, "error_primary", errorPrimary)
	}
	p.logDebug("Review feedback dispatch decision", debugFields...)

	switch dispatchMode {
	case reviewDispatchModeFailed:
		p.API.LogError("Review feedback dispatch decision",
			"review_loop_id", loop.ID,
			"dispatch_mode", dispatchMode,
			"decision_reason", decisionReason,
			"iteration", loop.Iteration,
			"dispatch_sha", dispatchSHA,
			"dispatch_digest", dispatchDigest,
			"new_count", counts.New,
			"repeated_count", counts.Repeated,
			"dismissed_count", counts.Dismissed,
			"dispatchable_count", counts.Dispatchable,
			"error_primary", errorPrimary,
		)
	}
}

// collectReviewFeedback keeps the existing method shape for tests/callers while
// delegating to the staged feedback pipeline.
func (p *Plugin) collectReviewFeedback(loop *kvstore.ReviewLoop) (string, error) {
	_, _, feedback, err := p.collectReviewFeedbackBundle(loop)
	return feedback, err
}

func (p *Plugin) collectReviewFeedbackBundle(loop *kvstore.ReviewLoop) (reviewFeedbackClassification, reviewFeedbackTelemetry, string, error) {
	candidates, err := p.collectFeedbackCandidates(loop)
	if err != nil {
		return reviewFeedbackClassification{}, reviewFeedbackTelemetry{}, "", err
	}

	normalized := make([]reviewFeedbackCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = normalizeFeedbackCandidate(candidate)
		actionableText, route, dropReason := extractCandidateActionableText(candidate)
		candidate.ActionableText = actionableText
		if candidate.ActionableText == "" {
			p.logReviewFeedbackCandidateDropped(loop, candidate, route, dropReason)
			continue
		}

		normalized = append(normalized, candidate)
	}

	classification := classifyFeedback(loop, normalized, time.Now().UnixMilli())
	telemetry := summarizeReviewFeedbackTelemetry(candidates, classification)
	return classification, telemetry, formatFindingsForCursorComment(classification.Dispatchable), nil
}

// transitionToHumanReview assigns human reviewers and transitions the loop to human_review.
func (p *Plugin) transitionToHumanReview(loop *kvstore.ReviewLoop) error {
	loop.Phase = kvstore.ReviewPhaseHumanReview
	loop.History = append(loop.History, kvstore.ReviewLoopEvent{
		Phase:     kvstore.ReviewPhaseHumanReview,
		Timestamp: time.Now().UnixMilli(),
	})
	loop.UpdatedAt = time.Now().UnixMilli()

	// TODO: Uncomment when ready for production use.
	// config := p.getConfiguration()
	// if config.HumanReviewTeam != "" {
	//     ghClient := p.getGitHubClient()
	//     if ghClient != nil {
	//         ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	//         defer cancel()
	//         err := ghClient.RequestReviewers(ctx, loop.Owner, loop.Repo, loop.PRNumber, github.ReviewersRequest{
	//             TeamReviewers: []string{config.HumanReviewTeam},
	//         })
	//         if err != nil {
	//             p.API.LogError("Failed to request human reviewers", "error", err.Error())
	//         }
	//     }
	// }

	if err := p.kvstore.SaveReviewLoop(loop); err != nil {
		return fmt.Errorf("failed to save review loop: %w", err)
	}

	p.updateReviewLoopInlineStatus(loop)
	p.publishReviewLoopChange(loop)
	return nil
}

// updateReviewLoopInlineStatus updates the "Agent finished!" bot reply post
// in-place with the current review loop status line. This avoids posting new
// thread messages on every state transition.
func (p *Plugin) updateReviewLoopInlineStatus(loop *kvstore.ReviewLoop) {
	// Fetch the agent record to get BotReplyPostID and metadata.
	record, err := p.kvstore.GetAgent(loop.AgentRecordID)
	if err != nil || record == nil {
		p.API.LogError("Failed to get agent record for inline status update",
			"agent_record_id", loop.AgentRecordID,
			"error", fmt.Sprintf("%v", err),
		)
		return
	}

	if record.BotReplyPostID == "" {
		return
	}

	att := attachments.BuildFinishedWithReviewStatusAttachment(
		record.CursorAgentID,
		record.Repository,
		record.Branch,
		record.Model,
		record.Summary,
		record.PrURL,
		loop.Phase,
		loop.Iteration,
	)

	p.updateBotReplyWithAttachment(record.BotReplyPostID, att)
}

// postReviewLoopCompletion posts a terminal completion attachment as a new
// thread message. Only called for terminal review loop states.
func (p *Plugin) postReviewLoopCompletion(loop *kvstore.ReviewLoop, attachment *model.SlackAttachment) {
	if loop.RootPostID == "" {
		return
	}

	post := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: loop.ChannelID,
		RootId:    loop.RootPostID,
	}
	model.ParseSlackAttachment(post, []*model.SlackAttachment{attachment})

	_, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Failed to post review loop completion",
			"error", appErr.Error(),
			"review_loop_id", loop.ID,
		)
	}
}

// publishReviewLoopChange publishes a WebSocket event when a review loop phase changes.
func (p *Plugin) publishReviewLoopChange(loop *kvstore.ReviewLoop) {
	p.API.PublishWebSocketEvent(
		"review_loop_changed",
		map[string]any{
			"review_loop_id":  loop.ID,
			"agent_record_id": loop.AgentRecordID,
			"phase":           loop.Phase,
			"iteration":       fmt.Sprintf("%d", loop.Iteration),
			"pr_url":          loop.PRURL,
			"updated_at":      fmt.Sprintf("%d", loop.UpdatedAt),
		},
		&model.WebsocketBroadcast{UserId: loop.UserID},
	)
}

// handleHumanReviewFeedback processes human review submissions in human_review
// phase and triggers a cursor_fixing iteration only for changes_requested.
func (p *Plugin) handleHumanReviewFeedback(loop *kvstore.ReviewLoop, review ghReview, pr ghPullRequest) error {
	state := strings.ToLower(strings.TrimSpace(review.State))
	if state != reviewStateChangesRequested {
		return nil
	}

	config := p.getConfiguration()
	if loop.Iteration >= config.MaxReviewIterations {
		loop.Phase = kvstore.ReviewPhaseMaxIterations
		loop.History = append(loop.History, kvstore.ReviewLoopEvent{
			Phase:     kvstore.ReviewPhaseMaxIterations,
			Timestamp: time.Now().UnixMilli(),
			Detail:    fmt.Sprintf("Reached max iterations (%d)", config.MaxReviewIterations),
		})
		loop.UpdatedAt = time.Now().UnixMilli()
		_ = p.kvstore.SaveReviewLoop(loop)

		p.updateReviewLoopInlineStatus(loop)
		p.publishReviewLoopChange(loop)
		p.postReviewLoopCompletion(loop, attachments.BuildMaxIterationsAttachment(
			loop.PRURL,
			config.MaxReviewIterations,
		))
		p.swapReaction(loop.TriggerPostID, "eyes", "warning")
		return nil
	}

	if pr.Head.SHA != "" {
		loop.LastCommitSHA = pr.Head.SHA
	}

	outcome, err := p.dispatchReviewFeedback(loop, pr)
	if err != nil {
		p.API.LogError("Failed to dispatch human review feedback",
			"error", err.Error(),
			"review_loop_id", loop.ID,
		)
		return err
	}
	if outcome.Skipped || outcome.Failed {
		if err := p.kvstore.SaveReviewLoop(loop); err != nil {
			return fmt.Errorf("failed to save review loop after dispatch outcome: %w", err)
		}
		p.publishReviewLoopChange(loop)
		return nil
	}
	if !outcome.Dispatched {
		return nil
	}

	detail := formatReviewDispatchHistoryDetail(
		fmt.Sprintf("Human feedback iteration %d", loop.Iteration+1),
		"",
		outcome.Counts,
	)
	if outcome.Mode == reviewDispatchModeDirect {
		detail = formatReviewDispatchHistoryDetail(
			fmt.Sprintf("Human feedback iteration %d", loop.Iteration+1),
			"direct follow-up dispatched",
			outcome.Counts,
		)
	}

	loop.Phase = kvstore.ReviewPhaseCursorFixing
	loop.Iteration++
	loop.History = append(loop.History, kvstore.ReviewLoopEvent{
		Phase:     kvstore.ReviewPhaseCursorFixing,
		Timestamp: time.Now().UnixMilli(),
		Detail:    detail,
	})
	loop.UpdatedAt = time.Now().UnixMilli()
	if err := p.kvstore.SaveReviewLoop(loop); err != nil {
		return fmt.Errorf("failed to save review loop: %w", err)
	}

	p.updateReviewLoopInlineStatus(loop)
	p.publishReviewLoopChange(loop)
	return nil
}

// handleHumanReviewApproval transitions the review loop to complete when a human
// reviewer approves the PR.
func (p *Plugin) handleHumanReviewApproval(loop *kvstore.ReviewLoop, reviewer string) error {
	loop.Phase = kvstore.ReviewPhaseComplete
	loop.History = append(loop.History, kvstore.ReviewLoopEvent{
		Phase:     kvstore.ReviewPhaseComplete,
		Timestamp: time.Now().UnixMilli(),
		Detail:    fmt.Sprintf("Approved by %s", reviewer),
	})
	loop.UpdatedAt = time.Now().UnixMilli()

	if err := p.kvstore.SaveReviewLoop(loop); err != nil {
		return fmt.Errorf("failed to save completed review loop: %w", err)
	}

	p.updateReviewLoopInlineStatus(loop)
	p.postReviewLoopCompletion(loop, attachments.BuildReviewCompleteAttachment(
		loop.PRURL,
		reviewer,
	))
	p.addReaction(loop.TriggerPostID, "rocket")
	p.publishReviewLoopChange(loop)

	return nil
}

// reviewLoopAwaitDetail returns a human-readable detail string for the
// awaiting_review history entry.
func reviewLoopAwaitDetail(bots []string) string {
	if len(bots) == 0 {
		return "Awaiting AI review (auto-detection)"
	}
	return fmt.Sprintf("Requested: %s", strings.Join(bots, ", "))
}

// isAIReviewerBot checks if the given GitHub username matches a configured AI reviewer bot.
func (p *Plugin) isAIReviewerBot(login string) bool {
	config := p.getConfiguration()
	botUsernames := config.ParseAIReviewerBots()
	loginLower := strings.ToLower(login)
	for _, bot := range botUsernames {
		if strings.ToLower(bot) == loginLower {
			return true
		}
	}
	return false
}
