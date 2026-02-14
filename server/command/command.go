package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"

	"github.com/mattermost/mattermost-plugin-cursor/server/attachments"
	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/parser"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

const (
	CommandTrigger = "cursor"

	subcommandList     = "list"
	subcommandStatus   = "status"
	subcommandCancel   = "cancel"
	subcommandSettings = "settings"
	subcommandModels   = "models"
	subcommandHelp     = "help"

	errNoCursorClient = "Cursor API key is not configured. Please ask your system administrator to configure it in System Console > Plugins > Cursor Background Agents."
)

// Dependencies groups the external dependencies the command handler needs.
type Dependencies struct {
	Client         *pluginapi.Client
	CursorClientFn func() cursor.Client
	Store          kvstore.KVStore
	BotUserID      string
	SiteURL        string
	PluginID       string
}

// Handler processes /cursor slash commands.
type Handler struct {
	deps Dependencies
}

// Command is the interface exposed to plugin.go for ExecuteCommand dispatch.
type Command interface {
	Handle(args *model.CommandArgs) (*model.CommandResponse, error)
}

// NewHandler creates and registers the /cursor command handler.
func NewHandler(deps Dependencies) Command {
	if err := deps.Client.SlashCommand.Register(getCommand()); err != nil {
		deps.Client.Log.Error("Failed to register /cursor command", "error", err)
	}
	return &Handler{deps: deps}
}

func getCommand() *model.Command {
	return &model.Command{
		Trigger:          CommandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Launch and manage Cursor Background Agents",
		AutoCompleteHint: "[prompt] | list | status | cancel | settings | models | help",
		AutocompleteData: getAutocompleteData(),
	}
}

func getAutocompleteData() *model.AutocompleteData {
	ac := model.NewAutocompleteData(
		CommandTrigger,
		"[subcommand]",
		"Launch and manage Cursor Background Agents",
	)

	list := model.NewAutocompleteData(subcommandList, "", "List your active agents with status")
	ac.AddCommand(list)

	status := model.NewAutocompleteData(subcommandStatus, "<agentID>", "Show detailed status of a specific agent")
	status.AddTextArgument("Agent ID (from /cursor list)", "[agentID]", "")
	ac.AddCommand(status)

	cancel := model.NewAutocompleteData(subcommandCancel, "<agentID>", "Cancel a running agent")
	cancel.AddTextArgument("Agent ID to cancel", "[agentID]", "")
	ac.AddCommand(cancel)

	settings := model.NewAutocompleteData(subcommandSettings, "", "Configure channel and user defaults")
	ac.AddCommand(settings)

	models := model.NewAutocompleteData(subcommandModels, "", "List available Cursor AI models")
	ac.AddCommand(models)

	help := model.NewAutocompleteData(subcommandHelp, "", "Show help for /cursor commands")
	ac.AddCommand(help)

	return ac
}

// Handle dispatches /cursor subcommands.
func (h *Handler) Handle(args *model.CommandArgs) (*model.CommandResponse, error) {
	fields := strings.Fields(args.Command)

	if len(fields) < 2 {
		return h.executeHelp(), nil
	}

	subcommand := strings.ToLower(fields[1])

	switch subcommand {
	case subcommandList:
		return h.executeList(args)
	case subcommandStatus:
		return h.executeStatus(args, fields[2:])
	case subcommandCancel:
		return h.executeCancel(args, fields[2:])
	case subcommandSettings:
		return h.executeSettings(args)
	case subcommandModels:
		return h.executeModels(args)
	case subcommandHelp:
		return h.executeHelp(), nil
	default:
		return h.executeLaunch(args)
	}
}

func (h *Handler) executeLaunch(args *model.CommandArgs) (*model.CommandResponse, error) {
	if h.deps.CursorClientFn() == nil {
		return ephemeralResponse(errNoCursorClient), nil
	}

	prompt := strings.TrimPrefix(args.Command, "/"+CommandTrigger+" ")
	prompt = strings.TrimSpace(prompt)

	if prompt == "" {
		return ephemeralResponse("Please provide a prompt. Usage: `/cursor <prompt>`"), nil
	}

	parsed := parser.Parse(prompt, "")
	if parsed == nil {
		parsed = &parser.ParsedMention{Prompt: prompt}
	}
	if parsed.Prompt == "" {
		parsed.Prompt = prompt
	}

	channelSettings, _ := h.deps.Store.GetChannelSettings(args.ChannelId)
	userSettings, _ := h.deps.Store.GetUserSettings(args.UserId)

	repo := coalesce(
		parsed.Repository,
		safeChannelRepo(channelSettings),
		safeUserRepo(userSettings),
	)
	branch := coalesce(
		parsed.Branch,
		safeChannelBranch(channelSettings),
		safeUserBranch(userSettings),
		"main",
	)
	cursorModel := coalesce(
		parsed.Model,
		safeUserModel(userSettings),
		"auto",
	)
	autoCreatePR := true
	if parsed.AutoPR != nil {
		autoCreatePR = *parsed.AutoPR
	}

	if repo == "" {
		return ephemeralResponse("No repository specified. Use `repo=owner/repo` in your prompt or set a default with `/cursor settings`."), nil
	}

	repoURL := repo
	if !strings.Contains(repo, "://") {
		repoURL = "https://github.com/" + repo
	}

	launchReq := cursor.LaunchAgentRequest{
		Prompt: cursor.Prompt{Text: parsed.Prompt},
		Source: cursor.Source{
			Repository: repoURL,
			Ref:        branch,
		},
		Target: &cursor.Target{
			BranchName:   fmt.Sprintf("cursor/%s", sanitizeBranchName(parsed.Prompt)),
			AutoCreatePr: autoCreatePR,
		},
		Model: cursorModel,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agent, err := h.deps.CursorClientFn().LaunchAgent(ctx, launchReq)
	if err != nil {
		return ephemeralResponse(formatAPIError("Failed to launch agent", err)), nil
	}

	launchAttachment := attachments.BuildLaunchAttachment(agent.ID, repo, branch, cursorModel)
	botPost := &model.Post{
		UserId:    h.deps.BotUserID,
		ChannelId: args.ChannelId,
	}
	model.ParseSlackAttachment(botPost, []*model.SlackAttachment{launchAttachment})
	botPost.AddProp("cursor_agent_id", agent.ID)
	botPost.AddProp("cursor_agent_status", string(agent.Status))
	if err := h.deps.Client.Post.CreatePost(botPost); err != nil {
		return ephemeralResponse("Failed to post agent status message."), nil
	}

	_ = h.deps.Client.Post.AddReaction(&model.Reaction{
		UserId:    h.deps.BotUserID,
		PostId:    botPost.Id,
		EmojiName: "hourglass_flowing_sand",
	})

	now := time.Now().UnixMilli()
	_ = h.deps.Store.SaveAgent(&kvstore.AgentRecord{
		CursorAgentID:  agent.ID,
		PostID:         botPost.Id,
		TriggerPostID:  botPost.Id,
		ChannelID:      args.ChannelId,
		UserID:         args.UserId,
		Status:         string(agent.Status),
		Repository:     repo,
		Branch:         branch,
		TargetBranch:   launchReq.Target.BranchName,
		Prompt:         parsed.Prompt,
		Model:          cursorModel,
		BotReplyPostID: botPost.Id,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	_ = h.deps.Store.SetThreadAgent(botPost.Id, agent.ID)

	return &model.CommandResponse{}, nil
}

func (h *Handler) executeList(args *model.CommandArgs) (*model.CommandResponse, error) {
	if h.deps.CursorClientFn() == nil {
		return ephemeralResponse(errNoCursorClient), nil
	}

	localAgents, err := h.deps.Store.GetAgentsByUser(args.UserId)
	if err != nil {
		return ephemeralResponse("Failed to retrieve agents."), nil
	}

	if len(localAgents) == 0 {
		return ephemeralResponse("You have no agents. Launch one with `/cursor <prompt>` or `@cursor <prompt>`."), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	remoteResp, remoteErr := h.deps.CursorClientFn().ListAgents(ctx, 100, "")
	remoteMap := make(map[string]cursor.Agent)
	if remoteErr == nil && remoteResp != nil {
		for _, ra := range remoteResp.Agents {
			remoteMap[ra.ID] = ra
		}
	}

	var sb strings.Builder
	sb.WriteString("#### Your Cursor Agents\n\n")
	sb.WriteString("| ID | Repository | Status | Link |\n")
	sb.WriteString("|:---|:-----------|:-------|:-----|\n")

	for _, la := range localAgents {
		status := la.Status
		if ra, ok := remoteMap[la.CursorAgentID]; ok {
			if string(ra.Status) != la.Status {
				status = string(ra.Status)
				la.Status = status
				_ = h.deps.Store.SaveAgent(la)
			}
		}
		shortID := la.CursorAgentID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s %s | [View](https://cursor.com/agents/%s) |\n",
			shortID, la.Repository, statusToEmoji(status), status, la.CursorAgentID))
	}

	return ephemeralResponse(sb.String()), nil
}

func (h *Handler) executeStatus(args *model.CommandArgs, params []string) (*model.CommandResponse, error) {
	if len(params) == 0 {
		return ephemeralResponse("Usage: `/cursor status <agentID>`\nGet agent IDs from `/cursor list`."), nil
	}

	if h.deps.CursorClientFn() == nil {
		return ephemeralResponse(errNoCursorClient), nil
	}

	agentID := params[0]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	remoteAgent, err := h.deps.CursorClientFn().GetAgent(ctx, agentID)
	if err != nil {
		return ephemeralResponse(formatAPIError(fmt.Sprintf("Failed to fetch agent `%s`", agentID), err)), nil
	}

	localAgent, _ := h.deps.Store.GetAgent(agentID)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("#### Agent Details: `%s`\n\n", agentID))
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|:------|:------|\n")
	sb.WriteString(fmt.Sprintf("| **Status** | %s %s |\n", statusToEmoji(string(remoteAgent.Status)), remoteAgent.Status))
	sb.WriteString(fmt.Sprintf("| **Repository** | %s |\n", remoteAgent.Source.Repository))
	sb.WriteString(fmt.Sprintf("| **Branch** | %s |\n", remoteAgent.Source.Ref))
	sb.WriteString(fmt.Sprintf("| **Target Branch** | %s |\n", remoteAgent.Target.BranchName))

	if remoteAgent.Target.PrURL != "" {
		sb.WriteString(fmt.Sprintf("| **Pull Request** | [View PR](%s) |\n", remoteAgent.Target.PrURL))
	}
	if remoteAgent.Summary != "" {
		sb.WriteString(fmt.Sprintf("\n**Summary:**\n> %s\n", remoteAgent.Summary))
	}

	sb.WriteString(fmt.Sprintf("\n[Open in Cursor](https://cursor.com/agents/%s)", agentID))

	if localAgent != nil && localAgent.PostID != "" {
		sb.WriteString(fmt.Sprintf(" | [Go to thread](/%s/pl/%s)", localAgent.ChannelID, localAgent.PostID))
	}

	return ephemeralResponse(sb.String()), nil
}

func (h *Handler) executeCancel(args *model.CommandArgs, params []string) (*model.CommandResponse, error) {
	if len(params) == 0 {
		return ephemeralResponse("Usage: `/cursor cancel <agentID>`\nGet agent IDs from `/cursor list`."), nil
	}

	if h.deps.CursorClientFn() == nil {
		return ephemeralResponse(errNoCursorClient), nil
	}

	agentID := params[0]

	localAgent, err := h.deps.Store.GetAgent(agentID)
	if err != nil || localAgent == nil {
		return ephemeralResponse(fmt.Sprintf("Agent `%s` not found.", agentID)), nil
	}
	if localAgent.UserID != args.UserId {
		return ephemeralResponse("You can only cancel your own agents."), nil
	}

	if localAgent.Status == "FINISHED" || localAgent.Status == "FAILED" || localAgent.Status == "STOPPED" {
		return ephemeralResponse(fmt.Sprintf("Agent `%s` is already %s.", agentID, localAgent.Status)), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, stopErr := h.deps.CursorClientFn().StopAgent(ctx, agentID)
	if stopErr != nil {
		return ephemeralResponse(formatAPIError("Failed to cancel agent", stopErr)), nil
	}

	localAgent.Status = "STOPPED"
	_ = h.deps.Store.SaveAgent(localAgent)

	if localAgent.PostID != "" {
		cancelPost := &model.Post{
			UserId:    h.deps.BotUserID,
			ChannelId: localAgent.ChannelID,
			RootId:    localAgent.PostID,
			Message:   fmt.Sprintf(":no_entry_sign: Agent `%s` was cancelled by <@%s>.", agentID, args.UserId),
		}
		_ = h.deps.Client.Post.CreatePost(cancelPost)

		_ = h.deps.Client.Post.RemoveReaction(&model.Reaction{
			UserId:    h.deps.BotUserID,
			PostId:    localAgent.TriggerPostID,
			EmojiName: "hourglass_flowing_sand",
		})
		_ = h.deps.Client.Post.AddReaction(&model.Reaction{
			UserId:    h.deps.BotUserID,
			PostId:    localAgent.TriggerPostID,
			EmojiName: "no_entry_sign",
		})
	}

	return ephemeralResponse(fmt.Sprintf("Agent `%s` has been cancelled.", agentID)), nil
}

func (h *Handler) executeSettings(args *model.CommandArgs) (*model.CommandResponse, error) {
	channelSettings, _ := h.deps.Store.GetChannelSettings(args.ChannelId)
	userSettings, _ := h.deps.Store.GetUserSettings(args.UserId)

	dialogRequest := model.OpenDialogRequest{
		TriggerId: args.TriggerId,
		URL:       fmt.Sprintf("%s/plugins/%s/api/v1/dialog/settings", h.deps.SiteURL, h.deps.PluginID),
		Dialog: model.Dialog{
			CallbackId:       "cursor_settings",
			Title:            "Cursor Settings",
			IntroductionText: "Configure defaults for Cursor Background Agents. **Channel settings** apply to everyone in this channel. **User settings** are your personal defaults.",
			SubmitLabel:      "Save",
			Elements: []model.DialogElement{
				{
					DisplayName: "Channel Default Repo",
					Name:        "channel_default_repo",
					Type:        "text",
					SubType:     "text",
					Placeholder: "owner/repo",
					HelpText:    "Default GitHub repository for this channel (e.g., mattermost/mattermost)",
					Optional:    true,
					Default:     safeChannelRepo(channelSettings),
				},
				{
					DisplayName: "Channel Default Branch",
					Name:        "channel_default_branch",
					Type:        "text",
					SubType:     "text",
					Placeholder: "main",
					HelpText:    "Default base branch for this channel",
					Optional:    true,
					Default:     safeChannelBranch(channelSettings),
				},
				{
					DisplayName: "Your Default Repo",
					Name:        "user_default_repo",
					Type:        "text",
					SubType:     "text",
					Placeholder: "owner/repo",
					HelpText:    "Your personal default repository",
					Optional:    true,
					Default:     safeUserRepo(userSettings),
				},
				{
					DisplayName: "Your Default Branch",
					Name:        "user_default_branch",
					Type:        "text",
					SubType:     "text",
					Placeholder: "main",
					HelpText:    "Your personal default branch",
					Optional:    true,
					Default:     safeUserBranch(userSettings),
				},
				{
					DisplayName: "Your Default Model",
					Name:        "user_default_model",
					Type:        "text",
					SubType:     "text",
					Placeholder: "auto",
					HelpText:    "Default AI model (e.g., auto, claude-sonnet, gpt-4o). See /cursor models",
					Optional:    true,
					Default:     safeUserModel(userSettings),
				},
			},
			State: fmt.Sprintf("%s|%s", args.ChannelId, args.UserId),
		},
	}

	appErr := h.deps.Client.Frontend.OpenInteractiveDialog(dialogRequest)
	if appErr != nil {
		return ephemeralResponse("Failed to open settings dialog."), nil
	}

	return &model.CommandResponse{}, nil
}

func (h *Handler) executeModels(args *model.CommandArgs) (*model.CommandResponse, error) {
	if h.deps.CursorClientFn() == nil {
		return ephemeralResponse(errNoCursorClient), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := h.deps.CursorClientFn().ListModels(ctx)
	if err != nil {
		return ephemeralResponse(formatAPIError("Failed to fetch models", err)), nil
	}

	if resp == nil || len(resp.Models) == 0 {
		return ephemeralResponse("No models available."), nil
	}

	var sb strings.Builder
	sb.WriteString("#### Available Cursor Models\n\n")
	for _, m := range resp.Models {
		sb.WriteString(fmt.Sprintf("- `%s`\n", m))
	}
	sb.WriteString("\nUse a model with: `@cursor with <model>, <prompt>` or `model=<model>` in your prompt.")

	return ephemeralResponse(sb.String()), nil
}

func (h *Handler) executeHelp() *model.CommandResponse {
	helpText := `#### Cursor Background Agents - Help

**Launching Agents:**
` + "- `@cursor <prompt>` - Launch an agent via bot mention" + `
` + "- `/cursor <prompt>` - Launch an agent via slash command" + `
` + "- `@cursor in <repo>, <prompt>` - Specify repository" + `
` + "- `@cursor with <model>, <prompt>` - Specify AI model" + `
` + "- `@cursor [repo=org/repo, branch=dev, model=opus] <prompt>` - Inline options" + `

**Management:**
` + "- `/cursor list` - List your active agents with status" + `
` + "- `/cursor status <agentID>` - Detailed status of a specific agent" + `
` + "- `/cursor cancel <agentID>` - Cancel a running agent" + `

**Configuration:**
` + "- `/cursor settings` - Configure channel and user defaults" + `
` + "- `/cursor models` - List available AI models" + `

**In Threads:**
- Reply in an agent thread to send a follow-up to the running agent
` + "- Use `@cursor agent <prompt>` in a thread to force a new agent"

	return ephemeralResponse(helpText)
}

// ephemeralResponse returns a CommandResponse that only the invoking user sees.
func ephemeralResponse(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         text,
	}
}

func statusToEmoji(status string) string {
	switch strings.ToUpper(status) {
	case "CREATING":
		return ":arrows_counterclockwise:"
	case "RUNNING":
		return ":hourglass:"
	case "FINISHED":
		return ":white_check_mark:"
	case "FAILED":
		return ":x:"
	case "STOPPED":
		return ":no_entry_sign:"
	default:
		return ":grey_question:"
	}
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
	return s
}

// formatAPIError formats an error from the Cursor API into a user-friendly message.
// If the error is a cursor.APIError with a JSON RawBody, the JSON is pretty-printed inside a
// markdown code block so that Mattermost renders it cleanly (no emoji parsing, proper wrapping).
func formatAPIError(action string, err error) string {
	var apiErr *cursor.APIError
	if errors.As(err, &apiErr) && apiErr.RawBody != "" && strings.HasPrefix(strings.TrimSpace(apiErr.RawBody), "{") {
		var prettyJSON bytes.Buffer
		if jsonErr := json.Indent(&prettyJSON, []byte(apiErr.RawBody), "", "  "); jsonErr == nil {
			return fmt.Sprintf(":x: **%s**\n\nError details:\n```json\n%s\n```", action, prettyJSON.String())
		}
		return fmt.Sprintf(":x: **%s**\n\nError details:\n```\n%s\n```", action, apiErr.RawBody)
	}
	return fmt.Sprintf(":x: **%s**\n\n%s", action, err.Error())
}

// Safe accessors for nil settings.
func safeChannelRepo(s *kvstore.ChannelSettings) string {
	if s == nil {
		return ""
	}
	return s.DefaultRepository
}

func safeChannelBranch(s *kvstore.ChannelSettings) string {
	if s == nil {
		return ""
	}
	return s.DefaultBranch
}

func safeUserRepo(s *kvstore.UserSettings) string {
	if s == nil {
		return ""
	}
	return s.DefaultRepository
}

func safeUserBranch(s *kvstore.UserSettings) string {
	if s == nil {
		return ""
	}
	return s.DefaultBranch
}

func safeUserModel(s *kvstore.UserSettings) string {
	if s == nil {
		return ""
	}
	return s.DefaultModel
}
