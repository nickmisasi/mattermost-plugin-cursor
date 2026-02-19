package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/attachments"
	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

const staleAgentMaxAge = 24 * time.Hour

// pollAgentStatuses is the background job callback that checks active agents for status changes.
func (p *Plugin) pollAgentStatuses() {
	// Step 1: Get all active agents from KV store.
	activeAgents, err := p.kvstore.ListActiveAgents()
	if err != nil {
		p.API.LogError("Failed to list active agents", "error", err.Error())
		return
	}

	cleaned := p.cleanupStaleAgents(activeAgents, staleAgentMaxAge)
	if cleaned > 0 {
		p.API.LogInfo("Cleaned up stale agents", "count", cleaned, "max_age", staleAgentMaxAge.String())
	}

	if len(activeAgents) == 0 {
		return
	}

	p.API.LogDebug("Polling agent statuses", "count", len(activeAgents))

	for _, record := range activeAgents {
		if cursor.AgentStatus(record.Status).IsTerminal() {
			continue
		}
		p.pollSingleAgent(record)
	}

	// Janitor sweep: reconcile GitHub-related state for finished agents.
	p.janitorSweep()
}

// janitorSweep reconciles GitHub-related state for agents where webhooks
// may have been missed. This is the BACKUP path -- webhooks are primary.
// Called at the end of each poll cycle.
func (p *Plugin) janitorSweep() {
	config := p.getConfiguration()
	if !config.EnableAIReviewLoop || p.getGitHubClient() == nil {
		return
	}

	// Find agents that are FINISHED with PrURL but have no ReviewLoop.
	// This catches cases where the PR opened webhook was missed.
	agents, err := p.kvstore.GetAllFinishedAgentsWithPR()
	if err != nil {
		p.API.LogError("Janitor: failed to list finished agents", "error", err.Error())
		return
	}

	for _, agent := range agents {
		if agent.PrURL == "" {
			continue
		}

		existing, _ := p.kvstore.GetReviewLoopByPRURL(agent.PrURL)
		if existing != nil {
			continue // Loop already exists; nothing to reconcile.
		}

		p.API.LogInfo("Janitor: bootstrapping missing review loop",
			"agent_id", agent.CursorAgentID,
			"pr_url", agent.PrURL,
		)

		if err := p.startReviewLoop(agent); err != nil {
			p.API.LogError("Janitor: failed to start review loop",
				"error", err.Error(),
				"agent_id", agent.CursorAgentID,
			)
		}
	}
}

// pollSingleAgent fetches the current status of a single agent and handles transitions.
func (p *Plugin) pollSingleAgent(record *kvstore.AgentRecord) {
	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		return
	}

	p.logDebug("Polling agent status",
		"agent_id", record.CursorAgentID,
		"current_status", record.Status,
	)

	// Step 1: Fetch current status from Cursor API.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agent, err := cursorClient.GetAgent(ctx, record.CursorAgentID)
	if err != nil {
		p.API.LogError("Failed to get agent status",
			"agentID", record.CursorAgentID,
			"error", err.Error(),
		)
		return
	}

	// Step 1b: Re-read the record from KV to pick up any concurrent changes
	// (e.g., cancel handler may have set status to STOPPED since our ListActiveAgents call).
	freshRecord, err := p.kvstore.GetAgent(record.CursorAgentID)
	if err != nil || freshRecord == nil {
		return
	}
	record = freshRecord

	// If the record was already moved to a terminal state by another handler
	// (e.g., cancelled via dashboard), skip further processing.
	if cursor.AgentStatus(record.Status).IsTerminal() {
		return
	}

	p.logDebug("Polled agent status",
		"agent_id", record.CursorAgentID,
		"stored_status", record.Status,
		"api_status", string(agent.Status),
		"summary", agent.Summary,
		"pr_url", agent.Target.PrURL,
	)

	// Step 2: Check if status changed.
	if string(agent.Status) == record.Status {
		return // No change; skip.
	}

	previousStatus := record.Status
	newStatus := string(agent.Status)

	p.API.LogInfo("Agent status changed",
		"agentID", record.CursorAgentID,
		"from", previousStatus,
		"to", newStatus,
	)

	// Step 3: Check if this agent belongs to a HITL workflow (planners skip normal handling).
	handledByWorkflow := false
	if agent.Status.IsTerminal() {
		handledByWorkflow = p.handleWorkflowAgentTerminal(record, agent)
	}

	// Step 3b: Normal terminal handling (only if not handled by workflow planner).
	if !handledByWorkflow {
		switch agent.Status {
		case cursor.AgentStatusRunning:
			p.handleAgentRunning(record)
		case cursor.AgentStatusFinished:
			p.handleAgentFinished(record, agent)
		case cursor.AgentStatusFailed:
			p.handleAgentFailed(record, agent)
		case cursor.AgentStatusStopped:
			p.handleAgentStopped(record)
		}
	}

	// Step 4: Update stored status.
	record.Status = newStatus
	if agent.Summary != "" {
		record.Summary = agent.Summary
	}
	record.UpdatedAt = time.Now().UnixMilli()
	if err := p.kvstore.SaveAgent(record); err != nil {
		p.API.LogError("Failed to update agent record", "agentID", record.CursorAgentID, "error", err.Error())
	}

	// Step 5: Update post props on the original bot post for webapp post dropdown filtering.
	p.updateBotPostProps(record)

	// Step 6: Publish WebSocket event for real-time frontend updates.
	p.publishAgentStatusChange(record)
}

func (p *Plugin) handleAgentRunning(record *kvstore.AgentRecord) {
	// Update the original bot reply to show running status (preserving metadata).
	runningAttachment := attachments.BuildRunningAttachment(
		record.CursorAgentID, record.Repository, record.Branch, record.Model,
	)
	p.updateBotReplyWithAttachment(record.BotReplyPostID, runningAttachment)

	// Post a short text notification to trigger thread follow.
	p.postBotReplyToThread(record, "Agent is now running...")
}

func (p *Plugin) handleAgentFinished(record *kvstore.AgentRecord, agent *cursor.Agent) {
	// Step 1: Swap reactions on the TRIGGER post.
	p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
	p.addReaction(record.TriggerPostID, "white_check_mark")

	// Use the branch name from the API response if available, fall back to the stored target branch.
	targetBranch := agent.Target.BranchName
	if targetBranch == "" {
		targetBranch = record.TargetBranch
	}

	finishedAttachment := attachments.BuildFinishedAttachment(
		record.CursorAgentID, record.Repository, record.Branch, record.Model,
		agent.Summary, agent.Target.PrURL, targetBranch,
	)

	// Step 2: Update the original bot reply post with the finished attachment.
	p.updateBotReplyWithAttachment(record.BotReplyPostID, finishedAttachment)

	// Step 3: Post a short text notification to trigger thread follow.
	var msg string
	switch {
	case agent.Target.PrURL != "":
		msg = fmt.Sprintf("Agent finished! [View PR](%s)", agent.Target.PrURL)
	case targetBranch != "":
		msg = fmt.Sprintf("Agent finished but no PR was created. Changes are on branch `%s`.", targetBranch)
	default:
		msg = "Agent finished but no PR was created. Check the agent output in Cursor for details."
	}
	p.postBotReplyToThread(record, msg)

	// Step 4: Update record with PR URL and actual branch name from Cursor API.
	if agent.Target.PrURL != "" {
		record.PrURL = agent.Target.PrURL
	}
	if agent.Target.BranchName != "" && agent.Target.BranchName != record.TargetBranch {
		record.TargetBranch = agent.Target.BranchName
	}

	// Step 5: Review loop is now started by the PR opened webhook (Phase 1).
	// The janitor sweep below handles reconciliation if the webhook was missed.
	// NOTE: We intentionally do NOT call startReviewLoop() here anymore.
	// The webhook-primary architecture ensures the PR opened event drives this.
}

func (p *Plugin) handleAgentFailed(record *kvstore.AgentRecord, agent *cursor.Agent) {
	// Step 1: Swap reactions.
	p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
	p.addReaction(record.TriggerPostID, "x")

	failedAttachment := attachments.BuildFailedAttachment(
		record.CursorAgentID, record.Repository, record.Branch, record.Model, agent.Summary,
	)

	// Step 2: Update the original bot reply post with the failed attachment.
	p.updateBotReplyWithAttachment(record.BotReplyPostID, failedAttachment)

	// Step 3: Post a short text notification to trigger thread follow.
	p.postBotReplyToThread(record, "Agent failed.")
}

func (p *Plugin) handleAgentStopped(record *kvstore.AgentRecord) {
	// Step 1: Swap reactions.
	p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
	p.addReaction(record.TriggerPostID, "no_entry_sign")

	stoppedAttachment := attachments.BuildStoppedAttachment(
		record.CursorAgentID, record.Repository, record.Branch, record.Model,
	)

	// Step 2: Update the original bot reply post with the stopped attachment.
	p.updateBotReplyWithAttachment(record.BotReplyPostID, stoppedAttachment)

	// Step 3: Post a short text notification to trigger thread follow.
	p.postBotReplyToThread(record, "Agent was stopped.")
}

// postBotReplyToThread posts a message in the agent's thread.
func (p *Plugin) postBotReplyToThread(record *kvstore.AgentRecord, message string) {
	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: record.ChannelID,
		RootId:    record.PostID,
		Message:   message,
	})
	if appErr != nil {
		p.API.LogError("Failed to post bot reply to thread",
			"agentID", record.CursorAgentID,
			"error", appErr.Error(),
		)
	}
}

// updateBotReplyWithAttachment fetches the bot's initial reply post and replaces
// its content with the given SlackAttachment. This updates the "launch" card to
// reflect the terminal status so users see the final state without scrolling.
func (p *Plugin) updateBotReplyWithAttachment(botReplyPostID string, attachment *model.SlackAttachment) {
	if botReplyPostID == "" {
		return
	}
	originalPost, appErr := p.API.GetPost(botReplyPostID)
	if appErr != nil {
		p.API.LogError("Failed to get bot reply post for attachment update",
			"postID", botReplyPostID,
			"error", appErr.Error(),
		)
		return
	}
	originalPost.Message = ""
	model.ParseSlackAttachment(originalPost, []*model.SlackAttachment{attachment})
	if _, appErr := p.API.UpdatePost(originalPost); appErr != nil {
		p.API.LogError("Failed to update bot reply post with attachment",
			"postID", botReplyPostID,
			"error", appErr.Error(),
		)
	}
}

// updateBotPostProps updates the post props on bot posts in the agent thread to reflect
// the current agent status. This enables webapp post dropdown menu filtering.
func (p *Plugin) updateBotPostProps(record *kvstore.AgentRecord) {
	if record.PostID == "" {
		return
	}

	// Get posts in the thread to find bot posts with cursor_agent_id prop.
	postList, appErr := p.API.GetPostThread(record.PostID)
	if appErr != nil {
		p.API.LogError("Failed to get post thread for prop update",
			"agentID", record.CursorAgentID,
			"error", appErr.Error(),
		)
		return
	}

	botUserID := p.getBotUserID()
	for _, post := range postList.Posts {
		if post.UserId != botUserID {
			continue
		}
		if agentID, ok := post.GetProp("cursor_agent_id").(string); ok && agentID == record.CursorAgentID {
			post.AddProp("cursor_agent_status", record.Status)
			if _, appErr := p.API.UpdatePost(post); appErr != nil {
				p.API.LogError("Failed to update bot post props",
					"postID", post.Id,
					"error", appErr.Error(),
				)
			}
		}
	}
}

// publishAgentStatusChange publishes a WebSocket event when an agent's status changes.
func (p *Plugin) publishAgentStatusChange(record *kvstore.AgentRecord) {
	p.API.PublishWebSocketEvent(
		"agent_status_change",
		map[string]any{
			"agent_id":      record.CursorAgentID,
			"status":        record.Status,
			"pr_url":        record.PrURL,
			"summary":       record.Summary,
			"repository":    record.Repository,
			"target_branch": record.TargetBranch,
			"updated_at":    fmt.Sprintf("%d", record.UpdatedAt),
		},
		&model.WebsocketBroadcast{UserId: record.UserID},
	)
}

// handleWorkflowAgentTerminal checks if a terminal agent belongs to a HITL workflow
// and routes accordingly. Returns true if the agent was handled as a planner
// (meaning normal terminal handling should be skipped).
func (p *Plugin) handleWorkflowAgentTerminal(record *kvstore.AgentRecord, agent *cursor.Agent) bool {
	workflowID, err := p.kvstore.GetWorkflowByAgent(record.CursorAgentID)
	if err != nil || workflowID == "" {
		return false
	}

	workflow, err := p.kvstore.GetWorkflow(workflowID)
	if err != nil || workflow == nil {
		return false
	}

	switch workflow.Phase {
	case kvstore.PhasePlanning:
		// Planner agent finished -- extract plan and present for review.
		p.handlePlannerFinished(workflow, agent)
		return true // Skip normal terminal handling.

	case kvstore.PhaseImplementing:
		// Implementation agent finished/failed/stopped -- mark workflow complete.
		workflow.Phase = kvstore.PhaseComplete
		workflow.UpdatedAt = time.Now().UnixMilli()
		if err := p.kvstore.SaveWorkflow(workflow); err != nil {
			p.API.LogError("Failed to save workflow in implementing phase", "workflow_id", workflow.ID, "error", err.Error())
		}
		p.publishWorkflowPhaseChange(workflow)
		return false // Let normal terminal handling run (PR link, reactions, etc.)
	}

	return false
}

// publishAgentCreated publishes a WebSocket event when a new agent is created.
func (p *Plugin) publishAgentCreated(record *kvstore.AgentRecord) {
	p.API.PublishWebSocketEvent(
		"agent_created",
		map[string]any{
			"agent_id":      record.CursorAgentID,
			"status":        record.Status,
			"repository":    record.Repository,
			"branch":        record.Branch,
			"target_branch": record.TargetBranch,
			"prompt":        record.Prompt,
			"description":   record.Description,
			"channel_id":    record.ChannelID,
			"post_id":       record.PostID,
			"cursor_url":    fmt.Sprintf("https://cursor.com/agents/%s", record.CursorAgentID),
			"created_at":    fmt.Sprintf("%d", record.CreatedAt),
		},
		&model.WebsocketBroadcast{UserId: record.UserID},
	)
}

// cleanupStaleAgents marks agents stuck in CREATING or RUNNING state for longer
// than maxAge as STOPPED and notifies users via thread messages.
//
//nolint:unused // Called from scheduled maintenance, not from the main poll loop.
func (p *Plugin) cleanupStaleAgents(agents []*kvstore.AgentRecord, maxAge time.Duration) int {
	cleaned := 0
	now := time.Now()

	for _, agent := range agents {
		if agent == nil || agent.CreatedAt <= 0 {
			continue
		}

		createdAt := time.UnixMilli(agent.CreatedAt)
		if now.Sub(createdAt) <= maxAge {
			continue
		}

		agent.Status = string(cursor.AgentStatusStopped)
		agent.UpdatedAt = now.UnixMilli()
		p.handleAgentStopped(agent)
		if err := p.kvstore.SaveAgent(agent); err != nil {
			p.API.LogError("Failed to mark stale agent as stopped",
				"agent_id", agent.CursorAgentID, "error", err.Error())
			continue
		}

		p.updateBotPostProps(agent)
		p.publishAgentStatusChange(agent)
		cleaned++
	}

	return cleaned
}
