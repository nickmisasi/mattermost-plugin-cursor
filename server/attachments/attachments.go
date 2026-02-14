package attachments

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

// Color constants for agent status attachment cards.
const (
	ColorBlue  = "#2389D7" // CREATING, RUNNING
	ColorGreen = "#3DB887" // FINISHED
	ColorRed   = "#D24B4E" // FAILED
	ColorGrey  = "#8B8FA7" // STOPPED
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
func BuildFinishedAttachment(agentID, repo, branch, modelName, summary, prURL string) *model.SlackAttachment {
	links := agentLinks(agentID)
	if prURL != "" {
		links = fmt.Sprintf("[View PR](%s) | %s", prURL, links)
	}

	text := links
	if summary != "" {
		text = summary + "\n\n" + links
	}

	return &model.SlackAttachment{
		Color:  ColorGreen,
		Title:  "Agent finished!",
		Text:   text,
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
