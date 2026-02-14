package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattermost/mattermost-plugin-ai/public/bridgeclient"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"

	"github.com/mattermost/mattermost-plugin-cursor/server/attachments"
	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/parser"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// MessageHasBeenPosted is invoked after a message is posted.
func (p *Plugin) MessageHasBeenPosted(_ *plugin.Context, post *model.Post) {
	// 1. Fast-reject: skip posts from the bot itself to prevent loops.
	botUserID := p.getBotUserID()
	if post.UserId == botUserID {
		return
	}

	// 2. Use pluginapi ShouldProcessMessage to skip system messages, webhooks, and other bots.
	shouldProcess, err := p.client.Post.ShouldProcessMessage(post, pluginapi.BotID(botUserID))
	if err != nil {
		p.API.LogError("ShouldProcessMessage failed", "error", err.Error())
		return
	}
	if !shouldProcess {
		return
	}

	// 3. Detect bot mention in the message.
	botMention := "@" + p.getBotUsername()
	if !containsMention(post.Message, botMention) {
		// Not a direct mention. Check if this is a thread reply for follow-up.
		p.handlePossibleFollowUp(post)
		return
	}

	// Acknowledge the mention immediately with :eyes: reaction.
	p.addReaction(post.Id, "eyes")

	p.logDebug("Bot mention detected",
		"post_id", post.Id,
		"channel_id", post.ChannelId,
		"user_id", post.UserId,
		"root_id", post.RootId,
		"message_length", len(post.Message),
	)

	// 4. Parse the mention message.
	parsed := parser.Parse(post.Message, botMention)
	if parsed == nil {
		p.removeReaction(post.Id, "eyes")
		// User just typed "@cursor" with no prompt -- post help text.
		p.postBotReply(post, "Please provide a prompt. Example: `@cursor fix the login bug`")
		return
	}

	p.logDebug("Parsed mention",
		"post_id", post.Id,
		"prompt_length", len(parsed.Prompt),
		"repository", parsed.Repository,
		"branch", parsed.Branch,
		"model", parsed.Model,
		"force_new", parsed.ForceNew,
	)

	// 5. Determine if this is a follow-up or a new agent.
	if post.RootId != "" && !parsed.ForceNew {
		// This is a reply in a thread. Check if there's an active agent.
		p.handleMentionInThread(post, parsed)
		return
	}

	// 6. Launch a new agent.
	p.launchNewAgent(post, parsed)
}

// containsMention checks if the message contains the bot mention.
// Uses case-insensitive matching.
func containsMention(message, botMention string) bool {
	return strings.Contains(strings.ToLower(message), strings.ToLower(botMention))
}

// handlePossibleFollowUp handles non-mention thread replies that might be follow-ups
// or HITL workflow iterations.
func (p *Plugin) handlePossibleFollowUp(post *model.Post) {
	// Only process thread replies (posts with a RootId).
	if post.RootId == "" {
		return
	}

	// Check for HITL workflow first.
	if p.handlePossibleWorkflowReply(post) {
		return // Handled as a workflow reply.
	}

	// Fall through to agent follow-up.
	agentRecord, err := p.getThreadAgentRecord(post.RootId)
	if err != nil || agentRecord == nil {
		return // Not an agent thread; ignore.
	}

	// If agent is still RUNNING, send follow-up.
	if agentRecord.Status == string(cursor.AgentStatusRunning) {
		p.sendFollowUp(post, agentRecord)
	}

	// If agent is FINISHED/FAILED/STOPPED, do nothing for non-mention replies.
}

// handleMentionInThread handles @cursor mentions within an existing thread.
func (p *Plugin) handleMentionInThread(post *model.Post, parsed *parser.ParsedMention) {
	// Check for active HITL workflow first.
	workflow, _ := p.kvstore.GetWorkflowByThread(post.RootId)
	if workflow != nil && workflow.Phase != kvstore.PhaseRejected && workflow.Phase != kvstore.PhaseComplete {
		// Active workflow exists. If in a review phase, treat the mention as iteration feedback.
		if workflow.Phase == kvstore.PhaseContextReview && workflow.UserID == post.UserId {
			p.iterateContext(workflow, parsed.Prompt, post)
			return
		}
		if workflow.Phase == kvstore.PhasePlanReview && workflow.UserID == post.UserId {
			p.iteratePlan(workflow, parsed.Prompt)
			return
		}
		// If planning, check if the planner agent is actually still running.
		if workflow.Phase == kvstore.PhasePlanning {
			if p.isPlannerStale(workflow) {
				// Planner is no longer active -- clean up the stuck workflow.
				p.rejectWorkflowForAgent(workflow.PlannerAgentID)
			} else if workflow.UserID == post.UserId {
				// Enqueue the parsed prompt as pending feedback for when the planner finishes.
				feedbackText := strings.TrimSpace(parsed.Prompt)
				if feedbackText != "" {
					if workflow.PendingFeedback != "" {
						workflow.PendingFeedback += "\n\n" + feedbackText
					} else {
						workflow.PendingFeedback = feedbackText
					}
					workflow.UpdatedAt = time.Now().UnixMilli()
					if err := p.kvstore.SaveWorkflow(workflow); err != nil {
						p.API.LogError("Failed to save pending feedback from mention",
							"workflow_id", workflow.ID,
							"error", err.Error(),
						)
					}
					p.postBotReply(post, "Got it. I'll apply your feedback when the current planning pass finishes.")
				}
				return
			} else {
				p.postBotReply(post, "A planning agent is currently running. Please wait for the plan to be ready for review.")
				return
			}
		}
		// If implementing, fall through to the agent check below.
	}

	agentRecord, err := p.getThreadAgentRecord(post.RootId)
	if err != nil || agentRecord == nil {
		// No agent in this thread -- launch a new one.
		p.launchNewAgent(post, parsed)
		return
	}

	if agentRecord.Status == string(cursor.AgentStatusRunning) {
		// Agent is running -- send the parsed prompt as a follow-up.
		p.sendFollowUp(post, agentRecord)
		return
	}

	// Agent is finished/failed -- launch a new agent with the same repo/branch defaults.
	if parsed.Repository == "" {
		parsed.Repository = agentRecord.Repository
	}
	if parsed.Branch == "" {
		parsed.Branch = agentRecord.Branch
	}
	p.launchNewAgent(post, parsed)
}

// launchNewAgent handles the full agent launch flow.
func (p *Plugin) launchNewAgent(post *model.Post, parsed *parser.ParsedMention) {
	// Step 1: Resolve defaults (channel -> user -> global config).
	repo, branch, modelName, autoCreatePR := p.resolveDefaults(post, parsed)

	p.logDebug("Resolved defaults for agent launch",
		"post_id", post.Id,
		"repo", repo,
		"branch", branch,
		"model", modelName,
		"auto_create_pr", autoCreatePR,
	)

	// Step 2: Validate -- repo is required.
	if repo == "" {
		p.removeReaction(post.Id, "eyes")
		p.postBotReply(post, "No repository specified. Set a default with `/cursor settings` or specify one: `@cursor in org/repo, fix the bug`")
		return
	}

	// Step 3: Swap :eyes: -> :hourglass_flowing_sand: to indicate launch in progress.
	p.removeReaction(post.Id, "eyes")
	p.addReaction(post.Id, "hourglass_flowing_sand")

	// Step 4: Enrich the prompt with thread context if this is a thread reply.
	promptText := parsed.Prompt
	var promptImages []cursor.Image
	if post.RootId != "" {
		if tc := p.enrichFromThread(post); tc != nil {
			promptText = tc.Prompt
			promptImages = tc.Images
		}
	}

	// Log the enriched prompt (truncate if very long to avoid log spam).
	debugPrompt := promptText
	if len(debugPrompt) > 500 {
		debugPrompt = debugPrompt[:500] + "... (truncated)"
	}
	p.logDebug("Agent launch prompt prepared",
		"post_id", post.Id,
		"prompt_length", len(promptText),
		"prompt_preview", debugPrompt,
		"image_count", len(promptImages),
	)

	// Step 4b: Check if HITL context review is enabled.
	skipReview, skipPlan := p.resolveHITLFlags(parsed, post.UserId)
	if !skipReview {
		// Build image references for KV storage (not full base64).
		imageRefs := p.buildImageRefs(post)

		// Start context review flow. This posts the review attachment and returns.
		// The agent will not be launched until the user approves.
		p.startContextReview(post, parsed, repo, branch, modelName, autoCreatePR, promptText, imageRefs, skipPlan)
		return
	}

	// If context review is skipped but plan loop is enabled, create a workflow for the plan loop.
	if !skipPlan {
		rootID := post.Id
		if post.RootId != "" {
			rootID = post.RootId
		}

		now := time.Now().UnixMilli()
		workflow := &kvstore.HITLWorkflow{
			ID:                uuid.New().String(),
			UserID:            post.UserId,
			ChannelID:         post.ChannelId,
			RootPostID:        rootID,
			TriggerPostID:     post.Id,
			Phase:             kvstore.PhasePlanning,
			Repository:        repo,
			Branch:            branch,
			Model:             modelName,
			AutoCreatePR:      autoCreatePR,
			OriginalPrompt:    parsed.Prompt,
			ApprovedContext:   promptText, // Use enriched prompt as approved context
			SkipContextReview: true,
			SkipPlanLoop:      false,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := p.kvstore.SaveWorkflow(workflow); err != nil {
			p.API.LogError("Failed to save HITL workflow for plan loop", "error", err.Error())
			// Fall through to direct launch.
		} else {
			if err := p.kvstore.SetThreadWorkflow(rootID, workflow.ID); err != nil {
				p.API.LogError("Failed to set thread workflow mapping", "error", err.Error())
			}
			p.startPlanLoop(workflow)
			return
		}
	}

	// Step 5: Wrap prompt with system instructions for the Cursor agent.
	promptText = p.wrapPromptWithSystemInstructions(promptText)

	// Step 6: Build the Cursor API request.
	repoURL := repo
	if !strings.Contains(repo, "://") {
		repoURL = "https://github.com/" + repo
	}
	launchReq := cursor.LaunchAgentRequest{
		Prompt: cursor.Prompt{Text: promptText, Images: promptImages},
		Source: cursor.Source{Repository: repoURL, Ref: branch},
		Target: &cursor.Target{
			BranchName:   sanitizeBranchName(parsed.Prompt),
			AutoCreatePr: autoCreatePR,
		},
		Model: modelName,
	}

	p.logDebug("LaunchAgent request",
		"post_id", post.Id,
		"source_repository", launchReq.Source.Repository,
		"source_ref", launchReq.Source.Ref,
		"target_branch", launchReq.Target.BranchName,
		"target_auto_create_pr", launchReq.Target.AutoCreatePr,
		"model", launchReq.Model,
	)

	// Step 7: Call Cursor API to launch the agent.
	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		p.removeReaction(post.Id, "hourglass_flowing_sand")
		p.addReaction(post.Id, "x")
		p.postBotReply(post, "Cursor API key is not configured. Ask your admin to configure the plugin.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agent, err := cursorClient.LaunchAgent(ctx, launchReq)
	if err != nil {
		p.API.LogError("Failed to launch Cursor agent", "error", err.Error())
		p.removeReaction(post.Id, "hourglass_flowing_sand")
		p.addReaction(post.Id, "x")
		p.postBotReply(post, formatAPIError("Failed to launch agent", err))
		return
	}

	// Step 8: Post in-thread reply with "Open in Cursor" link.
	rootID := post.Id
	if post.RootId != "" {
		rootID = post.RootId
	}

	attachment := attachments.BuildLaunchAttachment(agent.ID, repo, branch, modelName)
	replyPost := &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: post.ChannelId,
		RootId:    rootID,
	}
	model.ParseSlackAttachment(replyPost, []*model.SlackAttachment{attachment})
	replyPost.AddProp("cursor_agent_id", agent.ID)
	replyPost.AddProp("cursor_agent_status", string(agent.Status))
	createdReply, appErr := p.API.CreatePost(replyPost)
	if appErr != nil {
		p.API.LogError("Failed to create bot reply", "error", appErr.Error())
	}

	// Step 9: Store agent record in KV store.
	botReplyID := ""
	if createdReply != nil {
		botReplyID = createdReply.Id
	}
	now := time.Now().UnixMilli()
	agentRecord := &kvstore.AgentRecord{
		CursorAgentID:  agent.ID,
		Status:         string(agent.Status),
		TriggerPostID:  post.Id,
		PostID:         rootID,
		ChannelID:      post.ChannelId,
		UserID:         post.UserId,
		Repository:     repo,
		Branch:         branch,
		TargetBranch:   launchReq.Target.BranchName,
		Prompt:         parsed.Prompt,
		Model:          modelName,
		BotReplyPostID: botReplyID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	agentRecord.Description = p.generateDescription(promptText)

	if err := p.kvstore.SaveAgent(agentRecord); err != nil {
		p.API.LogError("Failed to save agent record", "error", err.Error())
	}

	// Step 10: Store thread-to-agent mapping using the rootID.
	if err := p.kvstore.SetThreadAgent(rootID, agent.ID); err != nil {
		p.API.LogError("Failed to save thread mapping", "error", err.Error())
	}

	// Step 11: Publish WebSocket event for real-time frontend updates.
	p.publishAgentCreated(agentRecord)
}

// resolveDefaults resolves repo, branch, model, and autoCreatePR from the cascade:
// parsed mention > channel settings > user settings > global config.
func (p *Plugin) resolveDefaults(post *model.Post, parsed *parser.ParsedMention) (repo, branch, modelName string, autoCreatePR bool) {
	config := p.getConfiguration()

	// Start with global defaults from plugin config.
	repo = config.DefaultRepository
	branch = config.DefaultBranch
	modelName = config.DefaultModel
	autoCreatePR = config.AutoCreatePR

	// Override with user-level settings (if set).
	userSettings, _ := p.kvstore.GetUserSettings(post.UserId)
	if userSettings != nil {
		if userSettings.DefaultRepository != "" {
			repo = userSettings.DefaultRepository
		}
		if userSettings.DefaultBranch != "" {
			branch = userSettings.DefaultBranch
		}
		if userSettings.DefaultModel != "" {
			modelName = userSettings.DefaultModel
		}
	}

	// Override with channel-level settings (if set).
	channelSettings, _ := p.kvstore.GetChannelSettings(post.ChannelId)
	if channelSettings != nil {
		if channelSettings.DefaultRepository != "" {
			repo = channelSettings.DefaultRepository
		}
		if channelSettings.DefaultBranch != "" {
			branch = channelSettings.DefaultBranch
		}
	}

	// Override with explicit values from the parsed mention (highest priority).
	if parsed.Repository != "" {
		repo = parsed.Repository
	}
	if parsed.Branch != "" {
		branch = parsed.Branch
	}
	if parsed.Model != "" {
		modelName = parsed.Model
	}
	if parsed.AutoPR != nil {
		autoCreatePR = *parsed.AutoPR
	}

	return repo, branch, modelName, autoCreatePR
}

// sendFollowUp sends a follow-up message to a running agent.
func (p *Plugin) sendFollowUp(post *model.Post, agentRecord *kvstore.AgentRecord) {
	p.logDebug("Sending follow-up to agent",
		"post_id", post.Id,
		"agent_id", agentRecord.CursorAgentID,
		"agent_status", agentRecord.Status,
	)

	// Step 1: Add eyes reaction to acknowledge the follow-up.
	p.addReaction(post.Id, "eyes")

	// Step 2: Extract the follow-up text.
	followUpText := post.Message
	botMention := "@" + p.getBotUsername()
	if idx := strings.Index(strings.ToLower(followUpText), strings.ToLower(botMention)); idx >= 0 {
		followUpText = followUpText[:idx] + followUpText[idx+len(botMention):]
	}
	followUpText = strings.TrimSpace(followUpText)

	if followUpText == "" {
		return
	}

	p.logDebug("Follow-up text prepared",
		"post_id", post.Id,
		"agent_id", agentRecord.CursorAgentID,
		"follow_up_text", followUpText,
	)

	// Step 3: Call Cursor API to send follow-up.
	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		p.removeReaction(post.Id, "eyes")
		p.addReaction(post.Id, "x")
		p.postBotReply(post, "Cursor API key is not configured.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := cursorClient.AddFollowup(ctx, agentRecord.CursorAgentID, cursor.FollowupRequest{
		Prompt: cursor.Prompt{Text: followUpText},
	})
	if err != nil {
		p.API.LogError("Failed to send follow-up", "agentID", agentRecord.CursorAgentID, "error", err.Error())
		p.removeReaction(post.Id, "eyes")
		p.addReaction(post.Id, "x")
		p.postBotReply(post, formatAPIError("Failed to send follow-up", err))
		return
	}

	// Step 4: Post confirmation in thread.
	p.postBotReply(post, ":speech_balloon: Follow-up sent to the running agent.")
}

// getThreadAgentRecord looks up the agent record for a thread root post.
func (p *Plugin) getThreadAgentRecord(rootPostID string) (*kvstore.AgentRecord, error) {
	agentID, err := p.kvstore.GetAgentIDByThread(rootPostID)
	if err != nil {
		return nil, err
	}
	if agentID == "" {
		return nil, nil
	}
	return p.kvstore.GetAgent(agentID)
}

// addReaction adds an emoji reaction to a post as the bot user.
func (p *Plugin) addReaction(postID, emojiName string) {
	_, appErr := p.API.AddReaction(&model.Reaction{
		UserId:    p.getBotUserID(),
		PostId:    postID,
		EmojiName: emojiName,
	})
	if appErr != nil {
		p.API.LogError("Failed to add reaction", "postID", postID, "emoji", emojiName, "error", appErr.Error())
	}
}

// removeReaction removes an emoji reaction from a post.
func (p *Plugin) removeReaction(postID, emojiName string) {
	appErr := p.API.RemoveReaction(&model.Reaction{
		UserId:    p.getBotUserID(),
		PostId:    postID,
		EmojiName: emojiName,
	})
	if appErr != nil {
		p.API.LogError("Failed to remove reaction", "postID", postID, "emoji", emojiName, "error", appErr.Error())
	}
}

// postBotReply posts a message as the bot in the thread of the given post.
func (p *Plugin) postBotReply(post *model.Post, message string) {
	rootID := post.Id
	if post.RootId != "" {
		rootID = post.RootId
	}
	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: post.ChannelId,
		RootId:    rootID,
		Message:   message,
	})
	if appErr != nil {
		p.API.LogError("Failed to create bot reply", "error", appErr.Error())
	}
}

const (
	maxThreadImages    = 5
	maxThreadImageSize = 10 * 1024 * 1024 // 10MB total

	descriptionPrompt = `You are a ticket title generator. Your ONLY output is a single short noun phrase (5-10 words) summarizing the coding task. No explanation, no reasoning, no quotes, no punctuation at the end. Just the title.

Examples of correct output:
Fix back button overlap on settings page
Add dark mode toggle to sidebar
Refactor user authentication middleware
Update payment processing error handling`

	enrichmentPrompt = `You are a context formatter. Given a Mattermost thread conversation, extract and clearly describe the task or issue being discussed. Your output will be given to a coding AI agent that has full access to the codebase.

Your output should:
- Clearly describe WHAT the issue or request is
- Include relevant details like URLs, page names, component names, and reproduction steps mentioned in the thread
- Include any screenshots or visual descriptions mentioned
- Preserve the user's original intent without adding assumptions
- NOT prescribe specific technical solutions, code changes, or implementation details
- NOT suggest specific files, CSS values, function names, or architectures
- NOT add requirements beyond what was discussed in the thread

The coding agent has the full codebase and will investigate the right approach itself. Your job is just to clearly communicate what needs to be done, not how to do it.

Format your output as a concise task description.`

	defaultSystemPrompt = `## Development Guidelines

Before making any changes:
1. Run ` + "`./enable-claude-docs.sh`" + ` if it exists in the repository root
2. Read any CLAUDE.md files in the repository for project-specific instructions
3. Read webapp/STYLE_GUIDE.md (if present) for frontend coding standards
4. Investigate the issue thoroughly before proposing or implementing a fix

When working on the task:
- ONLY make changes that directly solve the task at hand
- Do NOT make changes to irrelevant code, even if you notice other issues
- Do NOT refactor, clean up, or "improve" code outside the scope of the task
- Follow existing code patterns and conventions in the repository
- If the task involves UI/frontend work, ensure changes are accessible and theme-compatible`
)

// getSystemPrompt returns the configured system prompt, or the built-in default
// if the admin has not set one.
func (p *Plugin) getSystemPrompt() string {
	config := p.getConfiguration()
	if config.CursorAgentSystemPrompt != "" {
		return config.CursorAgentSystemPrompt
	}
	return defaultSystemPrompt
}

// wrapPromptWithSystemInstructions wraps the task prompt with system instructions
// so the Cursor agent receives both development guidelines and the actual task.
func (p *Plugin) wrapPromptWithSystemInstructions(taskPrompt string) string {
	systemPrompt := p.getSystemPrompt()
	return fmt.Sprintf("<system-instructions>\n%s\n</system-instructions>\n\n<task>\n%s\n</task>", systemPrompt, taskPrompt)
}

// threadContext holds the enriched prompt and images extracted from a thread.
type threadContext struct {
	Prompt string
	Images []cursor.Image
}

// enrichFromThread gathers thread context and uses the bridge client to create
// an enriched prompt. Falls back to raw thread text if the bridge client fails.
func (p *Plugin) enrichFromThread(post *model.Post) *threadContext {
	if post.RootId == "" {
		return nil
	}

	postList, appErr := p.API.GetPostThread(post.RootId)
	if appErr != nil {
		p.API.LogWarn("Failed to get post thread for context enrichment", "rootId", post.RootId, "error", appErr.Error())
		return nil
	}

	// Format the thread as context text and extract images.
	formattedThread, images := p.formatThread(postList)
	if formattedThread == "" {
		return nil
	}

	// Try using the bridge client to generate an enriched prompt.
	enrichedPrompt := p.enrichPromptViaBridge(formattedThread)
	if enrichedPrompt != "" {
		return &threadContext{Prompt: enrichedPrompt, Images: images}
	}

	// Fallback: use raw thread text as context prefix.
	return &threadContext{
		Prompt: "--- Thread Context ---\n" + formattedThread + "\n--- End Thread Context ---",
		Images: images,
	}
}

// formatThread formats a PostList into a readable thread and extracts images.
func (p *Plugin) formatThread(postList *model.PostList) (string, []cursor.Image) {
	order := postList.Order
	posts := postList.Posts

	// Sort by CreateAt for chronological order.
	sort.Slice(order, func(i, j int) bool {
		return posts[order[i]].CreateAt < posts[order[j]].CreateAt
	})

	var sb strings.Builder
	var images []cursor.Image
	var totalImageSize int

	for _, postID := range order {
		threadPost := posts[postID]

		// Get display name for the author.
		displayName := p.getDisplayName(threadPost.UserId)
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", displayName, threadPost.Message))

		// Extract images from file attachments.
		if len(threadPost.FileIds) > 0 && len(images) < maxThreadImages {
			for _, fileID := range threadPost.FileIds {
				if len(images) >= maxThreadImages {
					break
				}

				fileInfo, appErr := p.API.GetFileInfo(fileID)
				if appErr != nil {
					continue
				}

				if !strings.HasPrefix(fileInfo.MimeType, "image/") {
					continue
				}

				if totalImageSize+int(fileInfo.Size) > maxThreadImageSize {
					continue
				}

				fileData, appErr := p.API.GetFile(fileID)
				if appErr != nil {
					continue
				}

				// Decode image dimensions; skip the image if decoding fails.
				imgConfig, _, imgErr := image.DecodeConfig(bytes.NewReader(fileData))
				if imgErr != nil {
					p.API.LogWarn("Failed to decode image dimensions, skipping",
						"file_id", fileID,
						"error", imgErr.Error(),
					)
					continue
				}

				totalImageSize += len(fileData)
				images = append(images, cursor.Image{
					Data: base64.StdEncoding.EncodeToString(fileData),
					Dimension: cursor.ImageDimension{
						Width:  imgConfig.Width,
						Height: imgConfig.Height,
					},
				})
			}
		}
	}

	return sb.String(), images
}

// getDisplayName returns the display name for a user, falling back to "unknown".
func (p *Plugin) getDisplayName(userID string) string {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil || user == nil {
		return "unknown"
	}
	if user.GetDisplayName(model.ShowNicknameFullName) != "" {
		return user.GetDisplayName(model.ShowNicknameFullName)
	}
	return user.Username
}

// enrichPromptViaBridge uses the bridge client LLM to turn raw thread context
// into an actionable coding prompt. Returns empty string on failure.
func (p *Plugin) enrichPromptViaBridge(threadText string) string {
	if p.bridgeClient == nil {
		return ""
	}

	// Discover the default agent to use for completion.
	agents, err := p.bridgeClient.GetAgents("")
	if err != nil {
		p.API.LogWarn("Bridge client: failed to discover agents", "error", err.Error())
		return ""
	}

	if len(agents) == 0 {
		p.API.LogWarn("Bridge client: no agents available")
		return ""
	}

	// Use the default agent, or the first available one.
	var agentID string
	for _, agent := range agents {
		if agent.IsDefault {
			agentID = agent.ID
			break
		}
	}
	if agentID == "" {
		agentID = agents[0].ID
	}

	result, err := p.bridgeClient.AgentCompletion(agentID, bridgeclient.CompletionRequest{
		Posts: []bridgeclient.Post{
			{Role: "system", Message: enrichmentPrompt},
			{Role: "user", Message: threadText},
		},
		MaxGeneratedTokens: 2048,
	})
	if err != nil {
		p.API.LogWarn("Bridge client: completion failed, falling back to raw thread text", "error", err.Error())
		return ""
	}

	result = strings.TrimSpace(result)
	if result == "" {
		p.API.LogWarn("Bridge client: empty completion result")
		return ""
	}

	return result
}

// generateDescription uses the bridge client to create a short AI-generated task title.
// Returns empty string on any failure (graceful degradation).
func (p *Plugin) generateDescription(contextText string) string {
	if p.bridgeClient == nil {
		return ""
	}

	agents, err := p.bridgeClient.GetAgents("")
	if err != nil {
		p.API.LogWarn("Bridge client: failed to discover agents for description", "error", err.Error())
		return ""
	}
	if len(agents) == 0 {
		return ""
	}

	var agentID string
	for _, agent := range agents {
		if agent.IsDefault {
			agentID = agent.ID
			break
		}
	}
	if agentID == "" {
		agentID = agents[0].ID
	}

	result, err := p.bridgeClient.AgentCompletion(agentID, bridgeclient.CompletionRequest{
		Posts: []bridgeclient.Post{
			{Role: "system", Message: descriptionPrompt},
			{Role: "user", Message: contextText},
		},
		MaxGeneratedTokens: 2048,
	})
	if err != nil {
		p.API.LogWarn("Bridge client: description generation failed", "error", err.Error())
		return ""
	}

	result = strings.TrimSpace(result)
	runes := []rune(result)
	if len(runes) > 100 {
		result = string(runes[:100])
	}
	return result
}

// formatAPIError formats an error from the Cursor API into a user-friendly Mattermost message.
// If the error is a cursor.APIError with a JSON RawBody, the JSON is pretty-printed inside a
// markdown code block so that Mattermost renders it cleanly (no emoji parsing, proper wrapping).
func formatAPIError(action string, err error) string {
	var apiErr *cursor.APIError
	if errors.As(err, &apiErr) && apiErr.RawBody != "" && strings.HasPrefix(strings.TrimSpace(apiErr.RawBody), "{") {
		var prettyJSON bytes.Buffer
		if jsonErr := json.Indent(&prettyJSON, []byte(apiErr.RawBody), "", "  "); jsonErr == nil {
			return fmt.Sprintf(":x: **%s**\n\nError details:\n```json\n%s\n```", action, prettyJSON.String())
		}
		// json.Indent failed -- still wrap the raw body in a code block.
		return fmt.Sprintf(":x: **%s**\n\nError details:\n```\n%s\n```", action, apiErr.RawBody)
	}
	return fmt.Sprintf(":x: **%s**\n\n%s", action, err.Error())
}

// sanitizeBranchName creates a branch-name-safe slug from a prompt.
// Takes the first ~50 chars, lowercases, replaces non-alphanumeric with hyphens.
// Falls back to a timestamp-based name if the slug is empty (e.g. all-emoji prompt).
func sanitizeBranchName(prompt string) string {
	s := strings.ToLower(prompt)
	if len(s) > 50 {
		s = s[:50]
	}
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = fmt.Sprintf("agent-%d", time.Now().Unix())
	}
	return "cursor/" + s
}
