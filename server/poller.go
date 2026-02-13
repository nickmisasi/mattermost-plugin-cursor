package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// pollAgentStatuses is the background job callback that checks active agents for status changes.
func (p *Plugin) pollAgentStatuses() {
	// Step 1: Get all active agents from KV store.
	activeAgents, err := p.kvstore.ListActiveAgents()
	if err != nil {
		p.API.LogError("Failed to list active agents", "error", err.Error())
		return
	}

	if len(activeAgents) == 0 {
		return
	}

	p.API.LogDebug("Polling agent statuses", "count", len(activeAgents))

	for _, record := range activeAgents {
		p.pollSingleAgent(record)
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

	// Step 3: Handle status transitions.
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
	p.postBotReplyToThread(record, ":gear: Agent is now running...")
}

func (p *Plugin) handleAgentFinished(record *kvstore.AgentRecord, agent *cursor.Agent) {
	// Step 1: Swap reactions on the TRIGGER post.
	p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
	p.addReaction(record.TriggerPostID, "white_check_mark")

	// Step 2: Build completion message.
	message := ":white_check_mark: **Agent finished!**"

	// Include summary if available.
	if agent.Summary != "" {
		message += "\n\n> " + agent.Summary
	}

	// Include PR link if available.
	if agent.Target.PrURL != "" {
		message += fmt.Sprintf("\n\n:link: [View Pull Request](%s)", agent.Target.PrURL)
	}

	// Always include Cursor link.
	message += fmt.Sprintf("\n\n[Open in Cursor](https://cursor.com/agents/%s)", record.CursorAgentID)

	// Step 3: Post completion message in thread.
	p.postBotReplyToThread(record, message)

	// Step 4: Update record with PR URL.
	if agent.Target.PrURL != "" {
		record.PrURL = agent.Target.PrURL
	}
}

func (p *Plugin) handleAgentFailed(record *kvstore.AgentRecord, agent *cursor.Agent) {
	// Step 1: Swap reactions.
	p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
	p.addReaction(record.TriggerPostID, "x")

	// Step 2: Post failure message.
	message := ":x: **Agent failed.**"
	if agent.Summary != "" {
		message += "\n\n> " + agent.Summary
	}
	message += fmt.Sprintf("\n\n[Open in Cursor](https://cursor.com/agents/%s)", record.CursorAgentID)

	p.postBotReplyToThread(record, message)
}

func (p *Plugin) handleAgentStopped(record *kvstore.AgentRecord) {
	// Step 1: Swap reactions.
	p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
	p.addReaction(record.TriggerPostID, "no_entry_sign")

	// Step 2: Post stopped message.
	message := fmt.Sprintf(":no_entry_sign: **Agent was stopped.**\n\n[Open in Cursor](https://cursor.com/agents/%s)", record.CursorAgentID)
	p.postBotReplyToThread(record, message)
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
		map[string]interface{}{
			"agent_id":   record.CursorAgentID,
			"status":     record.Status,
			"pr_url":     record.PrURL,
			"summary":    record.Summary,
			"repository": record.Repository,
			"updated_at": fmt.Sprintf("%d", record.UpdatedAt),
		},
		&model.WebsocketBroadcast{UserId: record.UserID},
	)
}

// publishAgentCreated publishes a WebSocket event when a new agent is created.
func (p *Plugin) publishAgentCreated(record *kvstore.AgentRecord) {
	p.API.PublishWebSocketEvent(
		"agent_created",
		map[string]interface{}{
			"agent_id":   record.CursorAgentID,
			"status":     record.Status,
			"repository": record.Repository,
			"branch":     record.Branch,
			"prompt":     record.Prompt,
			"channel_id": record.ChannelID,
			"post_id":    record.PostID,
			"cursor_url": fmt.Sprintf("https://cursor.com/agents/%s", record.CursorAgentID),
			"created_at": fmt.Sprintf("%d", record.CreatedAt),
		},
		&model.WebsocketBroadcast{UserId: record.UserID},
	)
}
