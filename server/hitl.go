package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/attachments"
	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/parser"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// resolveHITLFlags determines whether to skip context review and plan loop
// using the resolution cascade: per-mention > user settings > global config.
func (p *Plugin) resolveHITLFlags(parsed *parser.ParsedMention, userID string) (skipReview, skipPlan bool) {
	config := p.getConfiguration()

	// Start with global config defaults (inverted: config says "Enable", we need "Skip").
	skipReview = !config.EnableContextReview
	skipPlan = !config.EnablePlanLoop

	// Override with user settings (if set, non-nil).
	userSettings, _ := p.kvstore.GetUserSettings(userID)
	if userSettings != nil {
		if userSettings.EnableContextReview != nil {
			skipReview = !*userSettings.EnableContextReview
		}
		if userSettings.EnablePlanLoop != nil {
			skipPlan = !*userSettings.EnablePlanLoop
		}
	}

	// Override with per-mention flags (highest priority).
	if parsed.Direct {
		return true, true
	}
	if parsed.SkipReview != nil {
		skipReview = *parsed.SkipReview
	}
	if parsed.SkipPlan != nil {
		skipPlan = *parsed.SkipPlan
	}

	return skipReview, skipPlan
}

// getUsername returns the username for a user ID. Returns "user" as fallback.
func (p *Plugin) getUsername(userID string) string {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil || user == nil {
		return "user"
	}
	return user.Username
}

// getPluginURL returns the full URL prefix for plugin HTTP endpoints.
// Format: {siteURL}/plugins/{pluginID}
func (p *Plugin) getPluginURL() string {
	siteURL := ""
	if p.client != nil {
		cfg := p.client.Configuration.GetConfig()
		if cfg != nil && cfg.ServiceSettings.SiteURL != nil {
			siteURL = strings.TrimRight(*cfg.ServiceSettings.SiteURL, "/")
		}
	}
	return siteURL + "/plugins/com.mattermost.plugin-cursor"
}

// startContextReview creates a new HITL workflow and posts the enriched context
// for user review with Accept/Reject buttons.
func (p *Plugin) startContextReview(
	post *model.Post,
	parsed *parser.ParsedMention,
	repo, branch, modelName string,
	autoCreatePR bool,
	enrichedContext string,
	images []kvstore.ImageRef,
	skipPlan bool,
) {
	// Step 1: Determine the thread root ID.
	rootID := post.Id
	if post.RootId != "" {
		rootID = post.RootId
	}

	// Step 2: Create the workflow record.
	now := time.Now().UnixMilli()
	workflow := &kvstore.HITLWorkflow{
		ID:                uuid.New().String(),
		UserID:            post.UserId,
		ChannelID:         post.ChannelId,
		RootPostID:        rootID,
		TriggerPostID:     post.Id,
		Phase:             kvstore.PhaseContextReview,
		Repository:        repo,
		Branch:            branch,
		Model:             modelName,
		AutoCreatePR:      autoCreatePR,
		OriginalPrompt:    parsed.Prompt,
		EnrichedContext:   enrichedContext,
		ContextImages:     images,
		SkipContextReview: false,
		SkipPlanLoop:      skipPlan,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// Step 3: Save workflow to KV store.
	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to save HITL workflow", "error", err.Error())
		p.postBotReply(post, "Failed to start context review. Launching agent directly.")
		return
	}

	// Step 4: Set thread mapping to the workflow.
	if err := p.kvstore.SetThreadWorkflow(rootID, workflow.ID); err != nil {
		p.API.LogError("Failed to set thread workflow mapping", "error", err.Error())
	}

	// Step 5: Build and post the context review attachment.
	username := p.getUsername(post.UserId)
	pluginURL := p.getPluginURL()
	attachment := attachments.BuildContextReviewAttachment(
		enrichedContext, repo, branch, modelName, workflow.ID, pluginURL, username,
	)

	reviewPost := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: post.ChannelId,
		RootId:    rootID,
	}
	model.ParseSlackAttachment(reviewPost, []*model.SlackAttachment{attachment})

	createdPost, appErr := p.API.CreatePost(reviewPost)
	if appErr != nil {
		p.API.LogError("Failed to post context review", "error", appErr.Error())
		return
	}

	// Step 6: Save the context post ID back to the workflow for later updates.
	workflow.ContextPostID = createdPost.Id
	workflow.UpdatedAt = time.Now().UnixMilli()
	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to update workflow with context post ID", "error", err.Error())
	}

	// Step 7: Publish WebSocket event.
	p.publishWorkflowPhaseChange(workflow)
}

// acceptContext processes a context approval. It updates the workflow
// and advances to the next phase (planning or implementing).
// Note: The button post update is handled by the PostActionIntegrationResponse.
func (p *Plugin) acceptContext(workflow *kvstore.HITLWorkflow) {
	// Step 1: Update workflow state.
	workflow.ApprovedContext = workflow.EnrichedContext
	workflow.UpdatedAt = time.Now().UnixMilli()

	// Step 2: Advance to next phase.
	if workflow.SkipPlanLoop {
		// Plan loop disabled: go directly to implementing.
		workflow.Phase = kvstore.PhaseImplementing
		if err := p.kvstore.SaveWorkflow(workflow); err != nil {
			p.API.LogError("Failed to save workflow after context accept", "error", err.Error())
			return
		}
		p.publishWorkflowPhaseChange(workflow)
		// Launch the implementation agent directly.
		p.launchImplementerFromWorkflow(workflow)
	} else {
		// Plan loop enabled: transition to planning phase.
		workflow.Phase = kvstore.PhasePlanning
		if err := p.kvstore.SaveWorkflow(workflow); err != nil {
			p.API.LogError("Failed to save workflow after context accept", "error", err.Error())
			return
		}
		p.publishWorkflowPhaseChange(workflow)
		p.startPlanLoop(workflow)
	}
}

// defaultPlannerSystemPrompt is the built-in prompt sent to planner agents.
// It is a package-level variable so tests can verify it.
var defaultPlannerSystemPrompt = `## Planning Mode - DO NOT MODIFY CODE

You are in PLANNING MODE. Your task is to deeply analyze the codebase and create a detailed
implementation plan for the requested change. You must NOT:
- Create, modify, or delete any files
- Create branches or pull requests
- Make any code changes whatsoever

You MUST:
1. Run ` + "`./enable-claude-docs.sh`" + ` if it exists in the repository root
2. Read any CLAUDE.md files in the repository for project-specific instructions
3. Thoroughly investigate the codebase areas relevant to the task
4. Identify all files that would need to change
5. Describe the specific changes needed in each file
6. Consider edge cases, tests that need updating, and potential regressions
7. Output a clear, structured implementation plan

Format your plan as:
### Summary
[1-2 sentence overview of the approach]

### Files to Change
For each file:
- **` + "`path/to/file`" + `**: [What changes and why]

### Implementation Steps
[Numbered steps in dependency order]

### Testing Strategy
[What tests to add/modify]

### Risks & Considerations
[Edge cases, potential regressions, things to watch for]`

// getPlannerSystemPrompt returns the planner system prompt from config, or the default.
func (p *Plugin) getPlannerSystemPrompt() string {
	config := p.getConfiguration()
	if config.PlannerSystemPrompt != "" {
		return config.PlannerSystemPrompt
	}
	return defaultPlannerSystemPrompt
}

// startPlanLoop transitions the workflow to the planning phase and launches a planner agent.
func (p *Plugin) startPlanLoop(workflow *kvstore.HITLWorkflow) {
	// Post a status message in the thread.
	planningAttachment := attachments.BuildPlanningStatusAttachment(
		workflow.Repository, workflow.Branch, workflow.Model, workflow.PlanIterationCount,
	)
	statusPost := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: workflow.ChannelID,
		RootId:    workflow.RootPostID,
	}
	model.ParseSlackAttachment(statusPost, []*model.SlackAttachment{planningAttachment})
	if _, appErr := p.API.CreatePost(statusPost); appErr != nil {
		p.API.LogError("Failed to post planning status", "error", appErr.Error())
	}

	// Launch the planner agent.
	if err := p.launchPlannerAgent(workflow); err != nil {
		p.API.LogError("Failed to launch planner agent",
			"workflow_id", workflow.ID,
			"error", err.Error(),
		)
		p.postBotReplyInThread(workflow, fmt.Sprintf(":x: **Failed to launch planning agent**: %s", err.Error()))
		return
	}
}

// launchPlannerAgent creates a new Cursor agent in planning mode for the given workflow.
func (p *Plugin) launchPlannerAgent(workflow *kvstore.HITLWorkflow) error {
	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		return fmt.Errorf("cursor API key is not configured")
	}

	// Build the planner prompt.
	plannerPrompt := p.buildPlannerPrompt(workflow)

	// Build the repo URL.
	repoURL := workflow.Repository
	if !strings.Contains(repoURL, "://") {
		repoURL = "https://github.com/" + repoURL
	}

	launchReq := cursor.LaunchAgentRequest{
		Prompt: cursor.Prompt{Text: plannerPrompt},
		Source: cursor.Source{
			Repository: repoURL,
			Ref:        workflow.Branch,
		},
		Target: &cursor.Target{
			AutoCreatePr: false, // Planner must NOT create PRs
			AutoBranch:   false, // CRITICAL: prevent orphan branches
		},
		Model: workflow.Model,
	}

	p.logDebug("Launching planner agent",
		"workflow_id", workflow.ID,
		"repository", repoURL,
		"branch", workflow.Branch,
		"model", workflow.Model,
		"iteration", workflow.PlanIterationCount,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agent, err := cursorClient.LaunchAgent(ctx, launchReq)
	if err != nil {
		return fmt.Errorf("cursor API error: %w", err)
	}

	// Create an AgentRecord for the planner (so the poller tracks it).
	now := time.Now().UnixMilli()
	agentRecord := &kvstore.AgentRecord{
		CursorAgentID: agent.ID,
		Status:        string(agent.Status),
		TriggerPostID: workflow.TriggerPostID,
		PostID:        workflow.RootPostID,
		ChannelID:     workflow.ChannelID,
		UserID:        workflow.UserID,
		Repository:    workflow.Repository,
		Branch:        workflow.Branch,
		Prompt:        fmt.Sprintf("[planner iteration %d]", workflow.PlanIterationCount),
		Model:         workflow.Model,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := p.kvstore.SaveAgent(agentRecord); err != nil {
		p.API.LogError("Failed to save planner agent record",
			"agent_id", agent.ID,
			"error", err.Error(),
		)
	}

	// Update the workflow with the planner agent ID.
	workflow.PlannerAgentID = agent.ID
	workflow.Phase = kvstore.PhasePlanning
	workflow.UpdatedAt = now

	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to save workflow after planner launch",
			"workflow_id", workflow.ID,
			"error", err.Error(),
		)
	}

	// Save the reverse index: cursor agent ID -> workflow ID.
	if err := p.kvstore.SetAgentWorkflow(agent.ID, workflow.ID); err != nil {
		p.API.LogError("Failed to save agent-to-workflow mapping",
			"agent_id", agent.ID,
			"workflow_id", workflow.ID,
			"error", err.Error(),
		)
	}

	// Publish WebSocket event.
	p.publishWorkflowPhaseChange(workflow)

	return nil
}

// buildPlannerPrompt constructs the prompt for a planner agent.
func (p *Plugin) buildPlannerPrompt(workflow *kvstore.HITLWorkflow) string {
	plannerSystemPrompt := p.getPlannerSystemPrompt()

	var sb strings.Builder
	sb.WriteString("<system-instructions>\n")
	sb.WriteString(plannerSystemPrompt)
	sb.WriteString("\n</system-instructions>\n\n")

	// Use the approved context if available, otherwise the original prompt.
	taskContext := workflow.ApprovedContext
	if taskContext == "" {
		taskContext = workflow.OriginalPrompt
	}

	sb.WriteString("<task>\n")
	sb.WriteString(taskContext)
	sb.WriteString("\n</task>\n")

	// On iteration 2+, include the previous plan and the user's feedback.
	if workflow.PlanIterationCount > 0 && workflow.RetrievedPlan != "" {
		sb.WriteString("\n<previous-plan>\n")
		sb.WriteString(workflow.RetrievedPlan)
		sb.WriteString("\n</previous-plan>\n")
	}

	if workflow.PlanFeedback != "" {
		sb.WriteString("\n<user-feedback>\n")
		sb.WriteString(workflow.PlanFeedback)
		sb.WriteString("\n</user-feedback>\n")
		sb.WriteString("\nPlease revise the plan based on the user's feedback above.\n")
	}

	return sb.String()
}

// handlePlannerFinished processes a planner agent that has reached a terminal state.
func (p *Plugin) handlePlannerFinished(workflow *kvstore.HITLWorkflow, agent *cursor.Agent) {
	if agent.Status == cursor.AgentStatusFailed {
		p.postBotReplyInThread(workflow,
			":x: **Planning agent failed.** You can reply in this thread to try again.",
		)
		workflow.Phase = kvstore.PhasePlanReview // Allow retry via thread reply
		workflow.UpdatedAt = time.Now().UnixMilli()
		_ = p.kvstore.SaveWorkflow(workflow)
		p.publishWorkflowPhaseChange(workflow)
		return
	}

	if agent.Status == cursor.AgentStatusStopped {
		// Planner was stopped (e.g., user cancelled). Don't post plan review.
		return
	}

	// Agent FINISHED -- retrieve the conversation to extract the plan.
	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		p.postBotReplyInThread(workflow, ":x: **Cannot retrieve plan**: Cursor API key is not configured.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conv, err := cursorClient.GetConversation(ctx, workflow.PlannerAgentID)
	if err != nil {
		p.API.LogError("Failed to get planner conversation",
			"agent_id", workflow.PlannerAgentID,
			"error", err.Error(),
		)
		p.postBotReplyInThread(workflow,
			fmt.Sprintf(":x: **Failed to retrieve plan**: %s\n\nReply in this thread to retry.", err.Error()),
		)
		workflow.Phase = kvstore.PhasePlanReview
		workflow.UpdatedAt = time.Now().UnixMilli()
		_ = p.kvstore.SaveWorkflow(workflow)
		return
	}

	// Extract the plan from the last assistant message.
	plan := extractPlanFromConversation(conv)
	if plan == "" {
		p.postBotReplyInThread(workflow,
			":warning: **Planning agent finished but produced no plan.** Reply in this thread to try again with more specific instructions.",
		)
		workflow.Phase = kvstore.PhasePlanReview
		workflow.UpdatedAt = time.Now().UnixMilli()
		_ = p.kvstore.SaveWorkflow(workflow)
		p.publishWorkflowPhaseChange(workflow)
		return
	}

	// Store the plan in the workflow.
	workflow.RetrievedPlan = plan
	workflow.UpdatedAt = time.Now().UnixMilli()

	// Check if there's pending feedback from the user submitted during planning.
	if workflow.PendingFeedback != "" {
		// Don't show the plan for review -- auto-iterate with the pending feedback.
		feedback := workflow.PendingFeedback
		workflow.PendingFeedback = ""
		if err := p.kvstore.SaveWorkflow(workflow); err != nil {
			p.API.LogError("Failed to clear pending feedback", "workflow_id", workflow.ID, "error", err.Error())
		}
		p.postBotReplyInThread(workflow, "Applying your feedback that was submitted during planning...")
		p.iteratePlan(workflow, feedback)
		return
	}

	workflow.Phase = kvstore.PhasePlanReview

	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to save workflow with plan",
			"workflow_id", workflow.ID,
			"error", err.Error(),
		)
	}

	// Post the plan review attachment.
	username := p.getUsername(workflow.UserID)
	pluginURL := p.getPluginURL()
	planAttachment := attachments.BuildPlanReviewAttachment(
		plan,
		workflow.Repository,
		workflow.Branch,
		workflow.Model,
		workflow.ID,
		pluginURL,
		username,
		workflow.PlanIterationCount,
	)

	reviewPost := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: workflow.ChannelID,
		RootId:    workflow.RootPostID,
	}
	model.ParseSlackAttachment(reviewPost, []*model.SlackAttachment{planAttachment})

	createdPost, appErr := p.API.CreatePost(reviewPost)
	if appErr != nil {
		p.API.LogError("Failed to post plan review", "error", appErr.Error())
	} else {
		workflow.PlanPostID = createdPost.Id
		workflow.UpdatedAt = time.Now().UnixMilli()
		_ = p.kvstore.SaveWorkflow(workflow)
	}

	// Publish WebSocket event.
	p.publishWorkflowPhaseChange(workflow)
}

// extractPlanFromConversation returns the text of the last assistant_message
// in the conversation. Earlier assistant messages are progress updates;
// the final one contains the structured plan.
func extractPlanFromConversation(conv *cursor.Conversation) string {
	if conv == nil {
		return ""
	}
	// Iterate in reverse to find the last assistant message.
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		if conv.Messages[i].Type == "assistant_message" {
			return strings.TrimSpace(conv.Messages[i].Text)
		}
	}
	return ""
}

// acceptPlan approves the plan and launches the implementation agent.
// Note: The button post update is handled by the PostActionIntegrationResponse in handleHITLResponse.
func (p *Plugin) acceptPlan(workflow *kvstore.HITLWorkflow) {
	workflow.ApprovedPlan = workflow.RetrievedPlan
	workflow.UpdatedAt = time.Now().UnixMilli()

	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to save workflow after plan approval",
			"workflow_id", workflow.ID,
			"error", err.Error(),
		)
	}

	// Stop the planner agent if it's still running (unlikely but possible).
	p.stopAgentIfRunning(workflow.PlannerAgentID)

	// Launch the implementation agent using the existing launchImplementerFromWorkflow.
	p.launchImplementerFromWorkflow(workflow)
}

// iteratePlan stops the current planner (if running), stores user feedback,
// increments the iteration counter, and launches a new planner agent.
func (p *Plugin) iteratePlan(workflow *kvstore.HITLWorkflow, userFeedback string) {
	// Stop current planner agent if it's still running.
	p.stopAgentIfRunning(workflow.PlannerAgentID)

	// Store the user's feedback for the next planner prompt.
	workflow.PlanFeedback = userFeedback
	workflow.PlanIterationCount++
	workflow.UpdatedAt = time.Now().UnixMilli()

	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to save workflow for plan iteration",
			"workflow_id", workflow.ID,
			"error", err.Error(),
		)
	}

	// Post acknowledgment.
	p.postBotReplyInThread(workflow, "Launching a new planning pass with your feedback...")

	// Launch a new planner agent with the feedback incorporated.
	if err := p.launchPlannerAgent(workflow); err != nil {
		p.API.LogError("Failed to launch new planner for iteration",
			"workflow_id", workflow.ID,
			"iteration", workflow.PlanIterationCount,
			"error", err.Error(),
		)
		p.postBotReplyInThread(workflow,
			fmt.Sprintf(":x: **Failed to launch planning agent**: %s", err.Error()),
		)
	}
}

// stopAgentIfRunning attempts to stop a Cursor agent. Logs but does not return errors.
func (p *Plugin) stopAgentIfRunning(agentID string) {
	if agentID == "" {
		return
	}

	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		return
	}

	// Check the record to see if the agent is still active.
	record, err := p.kvstore.GetAgent(agentID)
	if err != nil || record == nil {
		return
	}
	if cursor.AgentStatus(record.Status).IsTerminal() {
		return // Already done.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := cursorClient.StopAgent(ctx, agentID); err != nil {
		p.API.LogWarn("Failed to stop agent",
			"agent_id", agentID,
			"error", err.Error(),
		)
	}
}

// publishWorkflowPhaseChange publishes a WebSocket event when a workflow phase changes.
func (p *Plugin) publishWorkflowPhaseChange(workflow *kvstore.HITLWorkflow) {
	p.API.PublishWebSocketEvent(
		"workflow_phase_change",
		map[string]any{
			"workflow_id":          workflow.ID,
			"phase":                workflow.Phase,
			"planner_agent_id":     workflow.PlannerAgentID,
			"implementer_agent_id": workflow.ImplementerAgentID,
			"plan_iteration_count": fmt.Sprintf("%d", workflow.PlanIterationCount),
			"updated_at":           fmt.Sprintf("%d", workflow.UpdatedAt),
		},
		&model.WebsocketBroadcast{UserId: workflow.UserID},
	)
}

// launchImplementerFromWorkflow launches a Cursor implementation agent
// using the workflow's approved context and optional approved plan.
func (p *Plugin) launchImplementerFromWorkflow(workflow *kvstore.HITLWorkflow) {
	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		p.removeReaction(workflow.TriggerPostID, "hourglass_flowing_sand")
		p.addReaction(workflow.TriggerPostID, "x")
		p.postBotReplyInThread(workflow, "Cursor API key is not configured. Ask your admin to configure the plugin.")
		return
	}

	// Build the prompt text.
	promptText := workflow.ApprovedContext
	if promptText == "" {
		promptText = workflow.EnrichedContext
	}

	// If there's an approved plan, incorporate it.
	if workflow.ApprovedPlan != "" {
		promptText = fmt.Sprintf("<approved-plan>\n%s\n</approved-plan>\n\n<task>\nFollow the approved plan above to implement the requested changes.\nAdhere to the plan's file changes, implementation steps, and testing strategy.\nIf you discover the plan needs adjustment during implementation, note the deviation\nbut continue with the most reasonable approach.\n</task>", workflow.ApprovedPlan)
	}

	// Wrap with system instructions.
	promptText = p.wrapPromptWithSystemInstructions(promptText)

	// Build launch request.
	repoURL := workflow.Repository
	if !strings.Contains(repoURL, "://") {
		repoURL = "https://github.com/" + repoURL
	}

	launchReq := cursor.LaunchAgentRequest{
		Prompt: cursor.Prompt{Text: promptText},
		Source: cursor.Source{Repository: repoURL, Ref: workflow.Branch},
		Target: &cursor.Target{
			BranchName:   sanitizeBranchName(workflow.OriginalPrompt),
			AutoCreatePr: workflow.AutoCreatePR,
		},
		Model: workflow.Model,
	}

	// Re-attach images if available.
	if len(workflow.ContextImages) > 0 {
		launchReq.Prompt.Images = p.loadImagesFromRefs(workflow.ContextImages)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agent, err := cursorClient.LaunchAgent(ctx, launchReq)
	if err != nil {
		p.API.LogError("Failed to launch implementation agent", "error", err.Error())
		p.removeReaction(workflow.TriggerPostID, "hourglass_flowing_sand")
		p.addReaction(workflow.TriggerPostID, "x")
		p.postBotReplyInThread(workflow, formatAPIError("Failed to launch agent", err))
		workflow.Phase = kvstore.PhaseRejected
		workflow.UpdatedAt = time.Now().UnixMilli()
		_ = p.kvstore.SaveWorkflow(workflow)
		return
	}

	// Post launch attachment in thread.
	launchAttachment := attachments.BuildImplementerLaunchAttachment(agent.ID, workflow.Repository, workflow.Branch, workflow.Model)
	replyPost := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: workflow.ChannelID,
		RootId:    workflow.RootPostID,
	}
	model.ParseSlackAttachment(replyPost, []*model.SlackAttachment{launchAttachment})
	replyPost.AddProp("cursor_agent_id", agent.ID)
	replyPost.AddProp("cursor_agent_status", string(agent.Status))
	createdReply, appErr := p.API.CreatePost(replyPost)
	if appErr != nil {
		p.API.LogError("Failed to create launch reply", "error", appErr.Error())
	}

	// Save agent record (same as existing launchNewAgent flow).
	botReplyID := ""
	if createdReply != nil {
		botReplyID = createdReply.Id
	}
	now := time.Now().UnixMilli()
	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  agent.ID,
		Status:         string(agent.Status),
		TriggerPostID:  workflow.TriggerPostID,
		PostID:         workflow.RootPostID,
		ChannelID:      workflow.ChannelID,
		UserID:         workflow.UserID,
		Repository:     workflow.Repository,
		Branch:         workflow.Branch,
		TargetBranch:   launchReq.Target.BranchName,
		Prompt:         workflow.OriginalPrompt,
		Model:          workflow.Model,
		BotReplyPostID: botReplyID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := p.kvstore.SaveAgent(agentRecord); err != nil {
		p.API.LogError("Failed to save agent record", "error", err.Error())
	}

	// Update workflow with implementer agent ID.
	workflow.ImplementerAgentID = agent.ID
	workflow.Phase = kvstore.PhaseImplementing
	workflow.UpdatedAt = now
	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to update workflow with implementer agent", "error", err.Error())
	}

	// Publish WebSocket event.
	p.publishWorkflowPhaseChange(workflow)

	// Set thread->agent mapping (for follow-up support during implementation).
	if err := p.kvstore.SetThreadAgent(workflow.RootPostID, agent.ID); err != nil {
		p.API.LogError("Failed to set thread agent mapping", "error", err.Error())
	}

	// Save reverse index: cursor agent ID -> workflow ID (for poller routing).
	if err := p.kvstore.SetAgentWorkflow(agent.ID, workflow.ID); err != nil {
		p.API.LogError("Failed to save agent-to-workflow mapping for implementer",
			"agent_id", agent.ID,
			"workflow_id", workflow.ID,
			"error", err.Error(),
		)
	}

	// Publish WebSocket event.
	p.publishAgentCreated(agentRecord)
}

// rejectWorkflow cancels a workflow at the given phase.
// Note: The button post update is handled by the PostActionIntegrationResponse.
func (p *Plugin) rejectWorkflow(workflow *kvstore.HITLWorkflow) {
	workflow.Phase = kvstore.PhaseRejected
	workflow.UpdatedAt = time.Now().UnixMilli()
	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to save rejected workflow", "error", err.Error())
	}

	// Publish WebSocket event.
	p.publishWorkflowPhaseChange(workflow)

	// Swap reactions on trigger post.
	p.removeReaction(workflow.TriggerPostID, "hourglass_flowing_sand")
	p.addReaction(workflow.TriggerPostID, "no_entry_sign")
}

// iterateContext re-enriches the context using the user's feedback,
// updates the workflow, and posts a new context review attachment.
func (p *Plugin) iterateContext(workflow *kvstore.HITLWorkflow, userFeedback string, post *model.Post) {
	// Step 1: Post acknowledgment.
	p.postBotReplyInThread(workflow, "Re-analyzing with your feedback...")

	// Step 2: Re-enrich by combining the original context with user feedback.
	combinedInput := fmt.Sprintf(
		"Previous analysis:\n%s\n\nUser feedback requesting changes:\n%s\n\nPlease re-analyze the context incorporating the user's feedback above.",
		workflow.EnrichedContext,
		userFeedback,
	)

	reEnriched := p.enrichPromptViaBridge(combinedInput)
	if reEnriched == "" {
		// Fallback: append user feedback to existing context.
		reEnriched = workflow.EnrichedContext + "\n\n--- Additional Context ---\n" + userFeedback
	}

	// Step 3: Update workflow.
	workflow.EnrichedContext = reEnriched
	workflow.UpdatedAt = time.Now().UnixMilli()

	// Step 4: Post a NEW context review attachment.
	username := p.getUsername(workflow.UserID)
	pluginURL := p.getPluginURL()
	attachment := attachments.BuildContextReviewAttachment(
		reEnriched, workflow.Repository, workflow.Branch, workflow.Model,
		workflow.ID, pluginURL, username,
	)

	reviewPost := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: workflow.ChannelID,
		RootId:    workflow.RootPostID,
	}
	model.ParseSlackAttachment(reviewPost, []*model.SlackAttachment{attachment})

	createdPost, appErr := p.API.CreatePost(reviewPost)
	if appErr != nil {
		p.API.LogError("Failed to post updated context review", "error", appErr.Error())
		return
	}

	// Step 5: Update the old context post to remove buttons (show as superseded).
	if workflow.ContextPostID != "" {
		supersededAttachment := &model.SlackAttachment{
			Color: attachments.ColorGrey,
			Title: "Context review superseded by updated version below.",
		}
		p.updatePostWithAttachment(workflow.ContextPostID, supersededAttachment)
	}

	// Step 6: Save new context post ID.
	workflow.ContextPostID = createdPost.Id
	if err := p.kvstore.SaveWorkflow(workflow); err != nil {
		p.API.LogError("Failed to save workflow after context iteration", "error", err.Error())
	}
}

// handlePossibleWorkflowReply checks if a thread reply is in a HITL workflow thread
// and routes it to the appropriate phase handler. Returns true if handled.
func (p *Plugin) handlePossibleWorkflowReply(post *model.Post) bool {
	// Step 1: Look up thread mapping for a workflow.
	workflow, err := p.kvstore.GetWorkflowByThread(post.RootId)
	if err != nil || workflow == nil {
		return false // Not a workflow thread.
	}

	// Step 2: Only the workflow initiator's replies matter for iteration.
	if post.UserId != workflow.UserID {
		return false // Different user; don't intercept.
	}

	// Step 3: Route by current workflow phase.
	switch workflow.Phase {
	case kvstore.PhaseContextReview:
		p.iterateContext(workflow, post.Message, post)
		return true

	case kvstore.PhasePlanReview:
		// User replied during plan review -- treat as iteration feedback.
		p.iteratePlan(workflow, post.Message)
		return true

	case kvstore.PhasePlanning:
		// User replied while planner agent is running.
		// Queue their feedback for when the planner finishes.
		feedbackText := strings.TrimSpace(post.Message)

		// Strip bot mention if present (user may @cursor while replying).
		botMention := "@" + p.getBotUsername()
		if idx := strings.Index(strings.ToLower(feedbackText), strings.ToLower(botMention)); idx >= 0 {
			feedbackText = feedbackText[:idx] + feedbackText[idx+len(botMention):]
			feedbackText = strings.TrimSpace(feedbackText)
		}

		if feedbackText == "" {
			return true
		}

		// Append to existing pending feedback (user might reply multiple times).
		if workflow.PendingFeedback != "" {
			workflow.PendingFeedback += "\n\n" + feedbackText
		} else {
			workflow.PendingFeedback = feedbackText
		}
		workflow.UpdatedAt = time.Now().UnixMilli()

		if err := p.kvstore.SaveWorkflow(workflow); err != nil {
			p.API.LogError("Failed to save pending feedback",
				"workflow_id", workflow.ID,
				"error", err.Error(),
			)
			return true
		}

		// Acknowledge to the user that their feedback will be applied.
		p.postBotReplyInThread(workflow, "Got it. I'll apply your feedback when the current planning pass finishes.")
		return true

	default:
		// Workflow is in implementing, rejected, or complete phase.
		// Don't intercept; let normal follow-up handling take over.
		return false
	}
}

// postBotReplyInThread posts a bot message in the workflow's thread.
func (p *Plugin) postBotReplyInThread(workflow *kvstore.HITLWorkflow, message string) {
	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: workflow.ChannelID,
		RootId:    workflow.RootPostID,
		Message:   message,
	})
	if appErr != nil {
		p.API.LogError("Failed to post bot reply in thread", "error", appErr.Error())
	}
}

// updatePostWithAttachment replaces a post's attachments with the given attachment.
func (p *Plugin) updatePostWithAttachment(postID string, attachment *model.SlackAttachment) {
	if postID == "" {
		return
	}
	originalPost, appErr := p.API.GetPost(postID)
	if appErr != nil {
		p.API.LogError("Failed to get post for attachment update",
			"postID", postID,
			"error", appErr.Error(),
		)
		return
	}
	originalPost.Message = ""
	model.ParseSlackAttachment(originalPost, []*model.SlackAttachment{attachment})
	if _, appErr := p.API.UpdatePost(originalPost); appErr != nil {
		p.API.LogError("Failed to update post with attachment",
			"postID", postID,
			"error", appErr.Error(),
		)
	}
}

// loadImagesFromRefs loads image data from Mattermost file IDs stored in ImageRef.
func (p *Plugin) loadImagesFromRefs(refs []kvstore.ImageRef) []cursor.Image {
	var images []cursor.Image
	for _, ref := range refs {
		fileData, appErr := p.API.GetFile(ref.FileID)
		if appErr != nil {
			p.API.LogWarn("Failed to load image from file ref", "fileID", ref.FileID, "error", appErr.Error())
			continue
		}
		images = append(images, cursor.Image{
			Data: base64.StdEncoding.EncodeToString(fileData),
			Dimension: cursor.ImageDimension{
				Width:  ref.Width,
				Height: ref.Height,
			},
		})
	}
	return images
}

// buildImageRefs converts thread images into serializable ImageRef values.
// This avoids storing large base64 data in the KV store.
func (p *Plugin) buildImageRefs(post *model.Post) []kvstore.ImageRef {
	if post.RootId == "" {
		return nil
	}
	postList, appErr := p.API.GetPostThread(post.RootId)
	if appErr != nil {
		return nil
	}

	var refs []kvstore.ImageRef
	for _, postID := range postList.Order {
		threadPost := postList.Posts[postID]
		for _, fileID := range threadPost.FileIds {
			fileInfo, appErr := p.API.GetFileInfo(fileID)
			if appErr != nil || !strings.HasPrefix(fileInfo.MimeType, "image/") {
				continue
			}
			fileData, appErr := p.API.GetFile(fileID)
			if appErr != nil {
				continue
			}
			imgConfig, _, imgErr := image.DecodeConfig(bytes.NewReader(fileData))
			if imgErr != nil {
				continue
			}
			refs = append(refs, kvstore.ImageRef{
				FileID: fileID,
				Width:  imgConfig.Width,
				Height: imgConfig.Height,
			})
			if len(refs) >= maxThreadImages {
				return refs
			}
		}
	}
	return refs
}
