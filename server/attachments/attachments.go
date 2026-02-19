package attachments

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

// Color constants for agent status attachment cards.
const (
	ColorBlue   = "#2389D7" // CREATING, RUNNING
	ColorGreen  = "#3DB887" // FINISHED
	ColorRed    = "#D24B4E" // FAILED
	ColorGrey   = "#8B8FA7" // STOPPED
	ColorYellow = "#F5C518" // HITL review states (context_review, plan_review)
)

// StatusColor maps an agent status string to its corresponding hex color.
func StatusColor(status string) string {
	switch strings.ToUpper(status) {
	case "CREATING", "RUNNING":
		return ColorBlue
	case "FINISHED":
		return ColorGreen
	case "FAILED":
		return ColorRed
	case "STOPPED":
		return ColorGrey
	default:
		return ColorGrey
	}
}

// cursorAgentURL returns the Cursor web URL for the given agent ID.
func cursorAgentURL(agentID string) string {
	return "https://cursor.com/agents/" + agentID
}

// agentLinks returns the standard "Open in Cursor | Open in Web" markdown links.
// These must be placed in the Text field (which renders markdown), not Footer (plain text).
func agentLinks(agentID string) string {
	url := cursorAgentURL(agentID)
	return fmt.Sprintf("[Open in Cursor](%s) | [Open in Web](%s)", url, url)
}

// textWithLinks appends agent links to existing text content.
// If text is empty, returns just the links. Otherwise separates with a blank line.
func textWithLinks(text, agentID string) string {
	links := agentLinks(agentID)
	if text == "" {
		return links
	}
	return text + "\n\n" + links
}

// metadataFields returns SlackAttachmentFields for non-empty repo, branch, and model values.
func metadataFields(repo, branch, modelName string) []*model.SlackAttachmentField {
	var fields []*model.SlackAttachmentField
	if repo != "" {
		fields = append(fields, &model.SlackAttachmentField{
			Title: "Repo",
			Value: repo,
			Short: model.SlackCompatibleBool(true),
		})
	}
	if branch != "" {
		fields = append(fields, &model.SlackAttachmentField{
			Title: "Branch",
			Value: branch,
			Short: model.SlackCompatibleBool(true),
		})
	}
	if modelName != "" {
		fields = append(fields, &model.SlackAttachmentField{
			Title: "Model",
			Value: modelName,
			Short: model.SlackCompatibleBool(true),
		})
	}
	return fields
}

// BuildLaunchAttachment creates an attachment for a newly launched agent.
func BuildLaunchAttachment(agentID, repo, branch, modelName string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color:  ColorBlue,
		Title:  "Launched an agent. I'll notify here when it's finished.",
		Fields: metadataFields(repo, branch, modelName),
		Text:   agentLinks(agentID),
	}
}

// BuildRunningAttachment creates an attachment for an agent that has started running.
func BuildRunningAttachment(agentID, repo, branch, modelName string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color:  ColorBlue,
		Title:  "Agent is now running...",
		Fields: metadataFields(repo, branch, modelName),
		Text:   agentLinks(agentID),
	}
}

// BuildFinishedAttachment creates an attachment for a successfully finished agent.
// If prURL is non-empty, a "View PR" link is prepended to the links line.
// If prURL is empty but targetBranch is non-empty, a note about the missing PR is shown
// with the branch name so users can create a PR manually.
func BuildFinishedAttachment(agentID, repo, branch, modelName, summary, prURL, targetBranch string) *model.SlackAttachment {
	links := agentLinks(agentID)
	if prURL != "" {
		links = fmt.Sprintf("[View PR](%s) | %s", prURL, links)
	}

	var textParts []string
	if summary != "" {
		textParts = append(textParts, summary)
	}
	if prURL == "" {
		noPRNote := "No pull request was created."
		if targetBranch != "" {
			noPRNote += fmt.Sprintf(" The agent's changes are on branch `%s` -- you can create a PR manually.", targetBranch)
		}
		textParts = append(textParts, noPRNote)
	}
	textParts = append(textParts, links)

	return &model.SlackAttachment{
		Color:  ColorGreen,
		Title:  "Agent finished!",
		Text:   strings.Join(textParts, "\n\n"),
		Fields: metadataFields(repo, branch, modelName),
	}
}

// BuildFailedAttachment creates an attachment for a failed agent.
func BuildFailedAttachment(agentID, repo, branch, modelName, summary string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color:  ColorRed,
		Title:  "Agent failed.",
		Text:   textWithLinks(summary, agentID),
		Fields: metadataFields(repo, branch, modelName),
	}
}

// BuildStoppedAttachment creates an attachment for a stopped agent.
func BuildStoppedAttachment(agentID, repo, branch, modelName string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color:  ColorGrey,
		Title:  "Agent was stopped.",
		Fields: metadataFields(repo, branch, modelName),
		Text:   agentLinks(agentID),
	}
}

// BuildImplementerLaunchAttachment creates an attachment for when the implementation agent
// launches after plan approval. Distinct title from BuildLaunchAttachment to indicate
// this is the implementation phase of a HITL workflow.
func BuildImplementerLaunchAttachment(agentID, repo, branch, modelName string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color:  ColorBlue,
		Title:  "Implementation agent launched. I'll notify here when it's finished.",
		Fields: metadataFields(repo, branch, modelName),
		Text:   agentLinks(agentID),
	}
}

// BuildPlanningStatusAttachment creates an attachment shown while the planner agent is running.
func BuildPlanningStatusAttachment(repo, branch, modelName string, iterationCount int) *model.SlackAttachment {
	title := "Planning agent is analyzing the codebase..."
	if iterationCount > 1 {
		title = fmt.Sprintf("Planning agent is revising the plan (pass %d)...", iterationCount)
	}
	return &model.SlackAttachment{
		Color:  ColorBlue,
		Title:  title,
		Fields: metadataFields(repo, branch, modelName),
	}
}

// BuildPlanReviewAttachment creates an attachment for reviewing a plan.
// The plan text is truncated if it exceeds 14000 characters (leaving room for
// attachment metadata within Mattermost's 16KB post limit).
func BuildPlanReviewAttachment(plan, repo, branch, modelName, workflowID, pluginURL, username string, iterationCount int) *model.SlackAttachment {
	title := fmt.Sprintf("@%s, here's the implementation plan:", username)
	if iterationCount > 1 {
		title = fmt.Sprintf("@%s, here's the revised implementation plan (v%d):", username, iterationCount)
	}

	const maxPlanLen = 14000
	text := plan
	fallbackText := plan
	if len(text) > maxPlanLen {
		text = text[:maxPlanLen] + "\n\n*... (plan truncated -- view full plan in Cursor)*"
		fallbackText = text // Use the same truncated text for fallback
	}

	return &model.SlackAttachment{
		Color:    ColorYellow,
		Title:    title,
		Text:     text,
		Fields:   metadataFields(repo, branch, modelName),
		Fallback: "Plan review: " + fallbackText,
		Actions: []*model.PostAction{
			{
				Id:    "acceptplan",
				Name:  "Accept Plan",
				Type:  model.PostActionTypeButton,
				Style: "good",
				Integration: &model.PostActionIntegration{
					URL: pluginURL + "/api/v1/actions/hitl-response",
					Context: map[string]any{
						"workflow_id": workflowID,
						"action":      "accept",
						"phase":       "plan_review",
					},
				},
			},
			{
				Id:    "rejectplan",
				Name:  "Reject",
				Type:  model.PostActionTypeButton,
				Style: "danger",
				Integration: &model.PostActionIntegration{
					URL: pluginURL + "/api/v1/actions/hitl-response",
					Context: map[string]any{
						"workflow_id": workflowID,
						"action":      "reject",
						"phase":       "plan_review",
					},
				},
			},
		},
		Footer: "Reply in this thread to request changes to the plan.",
	}
}

// BuildPlanAcceptedAttachment creates an attachment replacing the plan review
// after the user accepts the plan (buttons removed).
func BuildPlanAcceptedAttachment(username string, iterationCount int) *model.SlackAttachment {
	title := fmt.Sprintf("Plan approved by @%s -- launching implementation agent.", username)
	if iterationCount > 1 {
		title = fmt.Sprintf("Plan (v%d) approved by @%s -- launching implementation agent.", iterationCount, username)
	}
	return &model.SlackAttachment{
		Color: ColorGreen,
		Title: title,
	}
}

// BuildPlanRejectedAttachment creates an attachment replacing the plan review
// after the user rejects the plan.
func BuildPlanRejectedAttachment(username string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color: ColorGrey,
		Title: fmt.Sprintf("Plan rejected by @%s -- workflow cancelled.", username),
	}
}

// BuildContextReviewAttachment creates an attachment for the context review HITL stage.
// It displays the enriched context and provides Accept/Reject buttons.
func BuildContextReviewAttachment(enrichedContext, repo, branch, modelName, workflowID, pluginURL, username string) *model.SlackAttachment {
	title := fmt.Sprintf("@%s, I've analyzed the thread context. Here's what I understand:", username)

	return &model.SlackAttachment{
		Color:    ColorYellow,
		Title:    title,
		Text:     enrichedContext,
		Fields:   metadataFields(repo, branch, modelName),
		Fallback: "Context review: " + enrichedContext,
		Actions: []*model.PostAction{
			{
				Id:    "acceptcontext",
				Type:  model.PostActionTypeButton,
				Name:  "Accept Context",
				Style: "good",
				Integration: &model.PostActionIntegration{
					URL: pluginURL + "/api/v1/actions/hitl-response",
					Context: map[string]any{
						"workflow_id": workflowID,
						"action":      "accept",
						"phase":       "context_review",
					},
				},
			},
			{
				Id:    "rejectcontext",
				Type:  model.PostActionTypeButton,
				Name:  "Reject",
				Style: "danger",
				Integration: &model.PostActionIntegration{
					URL: pluginURL + "/api/v1/actions/hitl-response",
					Context: map[string]any{
						"workflow_id": workflowID,
						"action":      "reject",
						"phase":       "context_review",
					},
				},
			},
		},
		Footer: "Reply in this thread to refine the context before accepting.",
	}
}

// BuildContextAcceptedAttachment creates an attachment shown after context is approved.
// It replaces the review attachment (buttons removed).
func BuildContextAcceptedAttachment(repo, branch, modelName, username string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color:  ColorGreen,
		Title:  fmt.Sprintf("Context approved by @%s", username),
		Fields: metadataFields(repo, branch, modelName),
	}
}

// BuildContextRejectedAttachment creates an attachment shown after context is rejected.
func BuildContextRejectedAttachment(username string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Color: ColorGrey,
		Title: fmt.Sprintf("Context rejected by @%s -- workflow cancelled.", username),
	}
}

// reviewStatusLine returns the status text for the review loop phase.
// Iteration count is shown only when > 1 for cleanliness.
func reviewStatusLine(phase string, iteration int) string {
	iterSuffix := ""
	if iteration > 1 {
		iterSuffix = fmt.Sprintf(" (iteration %d)", iteration)
	}

	switch phase {
	case "requesting_review":
		return "AI Review: Requesting reviewers..."
	case "awaiting_review":
		return fmt.Sprintf("AI Review: Waiting for CodeRabbit%s", iterSuffix)
	case "cursor_fixing":
		return fmt.Sprintf("AI Review: Cursor fixing feedback%s", iterSuffix)
	case "approved":
		return fmt.Sprintf("AI Review: Approved by CodeRabbit after %d iteration(s)", iteration)
	case "human_review":
		return "AI Review: Waiting for human reviewer"
	case "complete":
		return "AI Review: Complete"
	case "max_iterations":
		return "AI Review: Max iterations reached -- needs manual review"
	case "failed":
		return "AI Review: Error -- check logs"
	default:
		return fmt.Sprintf("AI Review: %s", phase)
	}
}

// BuildFinishedWithReviewStatusAttachment creates a finished attachment with an
// appended review loop status line. This is used to update the existing bot reply
// post in-place as the review loop progresses.
func BuildFinishedWithReviewStatusAttachment(
	agentID, repo, branch, modelName, summary, prURL string,
	reviewPhase string,
	iteration int,
) *model.SlackAttachment {
	links := agentLinks(agentID)
	if prURL != "" {
		links = fmt.Sprintf("[View PR](%s) | %s", prURL, links)
	}

	statusLine := reviewStatusLine(reviewPhase, iteration)

	var textParts []string
	if summary != "" {
		textParts = append(textParts, summary)
	}
	textParts = append(textParts, links)
	textParts = append(textParts, "---")
	textParts = append(textParts, statusLine)
	text := strings.Join(textParts, "\n\n")

	color := ColorGreen // default: finished card stays green
	switch reviewPhase {
	case "requesting_review", "awaiting_review", "cursor_fixing":
		color = ColorBlue
	case "max_iterations":
		color = ColorGrey
	case "failed":
		color = ColorRed
	}

	return &model.SlackAttachment{
		Color:  color,
		Title:  "Agent finished!",
		Text:   text,
		Fields: metadataFields(repo, branch, modelName),
	}
}

// BuildReviewApprovedAttachment creates a completion attachment for when
// CodeRabbit approves the PR. Posted as a new thread message.
func BuildReviewApprovedAttachment(prURL string, totalIterations int) *model.SlackAttachment {
	title := "CodeRabbit approved the PR!"
	if totalIterations > 1 {
		title = fmt.Sprintf("CodeRabbit approved the PR after %d iterations!", totalIterations)
	}

	text := ""
	if prURL != "" {
		text = fmt.Sprintf("[View PR](%s)", prURL)
	}

	return &model.SlackAttachment{
		Color: ColorGreen,
		Title: title,
		Text:  text,
	}
}

// BuildMaxIterationsAttachment creates a completion attachment for when
// the review loop hits the max iteration limit. Posted as a new thread message.
func BuildMaxIterationsAttachment(prURL string, maxIterations int) *model.SlackAttachment {
	title := fmt.Sprintf("AI review loop reached the maximum of %d iterations.", maxIterations)

	text := "Manual review is required."
	if prURL != "" {
		text = fmt.Sprintf("[View PR](%s) -- manual review is required.", prURL)
	}

	return &model.SlackAttachment{
		Color: ColorGrey,
		Title: title,
		Text:  text,
	}
}

// BuildReviewCompleteAttachment creates a completion attachment for when
// a human reviewer approves the PR. Posted as a new thread message.
func BuildReviewCompleteAttachment(prURL, reviewer string) *model.SlackAttachment {
	title := fmt.Sprintf("PR approved by %s! Review loop complete.", reviewer)

	text := ""
	if prURL != "" {
		text = fmt.Sprintf("[View PR](%s)", prURL)
	}

	return &model.SlackAttachment{
		Color: ColorGreen,
		Title: title,
		Text:  text,
	}
}

// BuildReviewFailedAttachment creates a completion attachment for when
// the review loop fails due to an error. Posted as a new thread message.
func BuildReviewFailedAttachment(detail string) *model.SlackAttachment {
	text := "Check plugin logs for details."
	if detail != "" {
		text = detail
	}

	return &model.SlackAttachment{
		Color: ColorRed,
		Title: "AI review loop failed.",
		Text:  text,
	}
}
