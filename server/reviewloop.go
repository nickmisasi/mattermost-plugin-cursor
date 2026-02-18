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

// handleAIReview processes a submitted review from a known AI reviewer bot.
// It checks whether CodeRabbit is satisfied and either transitions to approved
// or posts a @cursor fix comment.
func (p *Plugin) handleAIReview(loop *kvstore.ReviewLoop, review ghReview, pr ghPullRequest) error {
	isCodeRabbit := strings.EqualFold(review.User.Login, "coderabbitai[bot]")
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

		// Post @cursor fix comment and transition to cursor_fixing.
		if err := p.postCursorFixComment(loop); err != nil {
			p.API.LogError("Failed to post @cursor fix comment", "error", err.Error(), "review_loop_id", loop.ID)
			return err
		}

		loop.Phase = kvstore.ReviewPhaseCursorFixing
		loop.Iteration++
		loop.History = append(loop.History, kvstore.ReviewLoopEvent{
			Phase:     kvstore.ReviewPhaseCursorFixing,
			Timestamp: time.Now().UnixMilli(),
			Detail:    fmt.Sprintf("Iteration %d", loop.Iteration),
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

// postCursorFixComment aggregates review feedback from AI bots and posts a
// @cursor fix comment on the PR via the GitHub API.
func (p *Plugin) postCursorFixComment(loop *kvstore.ReviewLoop) error {
	feedback, err := p.collectReviewFeedback(loop)
	if err != nil {
		return fmt.Errorf("failed to collect review feedback: %w", err)
	}
	if feedback == "" {
		feedback = "Please review and fix any outstanding issues flagged by the AI reviewers."
	}

	commentBody := fmt.Sprintf("@cursor Please address the following review feedback:\n\n%s", feedback)

	ghClient := p.getGitHubClient()
	if ghClient == nil {
		return fmt.Errorf("GitHub client is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = ghClient.CreateComment(ctx, loop.Owner, loop.Repo, loop.PRNumber, commentBody)
	if err != nil {
		return fmt.Errorf("failed to post @cursor comment on PR: %w", err)
	}

	return nil
}

// collectReviewFeedback fetches all review comments from AI bots on the PR
// and formats them into a consolidated feedback string for the @cursor fix comment.
func (p *Plugin) collectReviewFeedback(loop *kvstore.ReviewLoop) (string, error) {
	ghClient := p.getGitHubClient()
	if ghClient == nil {
		return "", fmt.Errorf("GitHub client is not configured")
	}
	config := p.getConfiguration()
	botUsernames := config.ParseAIReviewerBots()
	botSet := make(map[string]bool, len(botUsernames))
	for _, bot := range botUsernames {
		botSet[strings.ToLower(bot)] = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	comments, err := ghClient.ListReviewComments(ctx, loop.Owner, loop.Repo, loop.PRNumber)
	if err != nil {
		return "", fmt.Errorf("failed to list review comments: %w", err)
	}

	var sb strings.Builder
	commentNum := 0

	for _, c := range comments {
		// Skip non-bot comments.
		if c.User == nil || !botSet[strings.ToLower(c.User.GetLogin())] {
			continue
		}

		// Filter by commit SHA if we have one -- only include comments on the latest commit.
		if loop.LastCommitSHA != "" && c.CommitID != nil && *c.CommitID != loop.LastCommitSHA {
			continue
		}

		commentNum++
		file := c.GetPath()
		line := c.GetLine()
		body := sanitizeReviewBodyForMattermost(c.GetBody())

		switch {
		case file != "" && line > 0:
			sb.WriteString(fmt.Sprintf("%d. **%s:%d** - %s\n", commentNum, file, line, body))
		case file != "":
			sb.WriteString(fmt.Sprintf("%d. **%s** - %s\n", commentNum, file, body))
		default:
			sb.WriteString(fmt.Sprintf("%d. %s\n", commentNum, body))
		}
	}

	// Also fetch top-level review body text.
	reviews, err := ghClient.ListReviews(ctx, loop.Owner, loop.Repo, loop.PRNumber)
	if err != nil {
		p.API.LogWarn("Failed to list reviews for feedback collection", "error", err.Error())
		// Non-fatal: we still have inline comments.
	} else {
		for _, r := range reviews {
			if r.User == nil || !botSet[strings.ToLower(r.User.GetLogin())] {
				continue
			}
			body := r.GetBody()
			if body == "" {
				continue
			}
			// Skip the "satisfied" review body.
			if strings.Contains(body, "Actionable comments posted: 0") {
				continue
			}
			body = sanitizeReviewBodyForMattermost(body)
			if commentNum == 0 {
				sb.WriteString("**Review summary:**\n")
			}
			commentNum++
			sb.WriteString(fmt.Sprintf("%d. %s\n", commentNum, truncateText(body, 500)))
		}
	}

	return sb.String(), nil
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
