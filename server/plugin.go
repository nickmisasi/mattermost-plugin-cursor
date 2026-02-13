package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost-plugin-ai/public/bridgeclient"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/pkg/errors"

	"github.com/mattermost/mattermost-plugin-cursor/server/command"
	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// client is the Mattermost server API client.
	client *pluginapi.Client

	// cursorClient is the Cursor Background Agents API client.
	cursorClient cursor.Client

	// kvstore is the KV store abstraction for plugin state.
	kvstore kvstore.KVStore

	// botUserID is the user ID of the bot created by this plugin.
	botUserID string

	// botUsername is the username of the bot (e.g., "cursor"), used for mention detection.
	botUsername string

	// backgroundJob is the scheduled background poller for agent statuses.
	backgroundJob io.Closer

	// bridgeClient is the LLM bridge client for prompt enrichment via the Agents plugin.
	bridgeClient *bridgeclient.Client

	// commandHandler handles /cursor slash commands.
	commandHandler command.Command

	// router is the HTTP router for handling API requests.
	router *mux.Router

	// configurationLock synchronizes access to the configuration, cursorClient, and botUserID.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration.
	configuration *configuration
}

// logDebug logs a debug message only when EnableDebugLogging is true.
func (p *Plugin) logDebug(msg string, keyValuePairs ...any) {
	if p.getConfiguration().EnableDebugLogging {
		p.API.LogDebug(msg, keyValuePairs...)
	}
}

// pluginLogger adapts the Plugin's conditional debug logging to the cursor.Logger interface.
type pluginLogger struct {
	plugin *Plugin
}

func (l *pluginLogger) LogDebug(msg string, keyValuePairs ...any) {
	l.plugin.logDebug(msg, keyValuePairs...)
}

// getCursorClient returns the Cursor API client under read lock.
func (p *Plugin) getCursorClient() cursor.Client {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	return p.cursorClient
}

// setCursorClient sets the Cursor API client under write lock.
func (p *Plugin) setCursorClient(client cursor.Client) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()
	p.cursorClient = client
}

// getBotUserID returns the bot user ID under read lock.
func (p *Plugin) getBotUserID() string {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	return p.botUserID
}

// setBotUserID sets the bot user ID under write lock.
func (p *Plugin) setBotUserID(id string) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()
	p.botUserID = id
}

// getBotUsername returns the bot username under read lock.
func (p *Plugin) getBotUsername() string {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	return p.botUsername
}

// setBotUsername sets the bot username under write lock.
func (p *Plugin) setBotUsername(username string) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()
	p.botUsername = username
}

const (
	botUsername    = "cursor"
	botDisplayName = "Cursor"
	botDescription = "Cursor Background Agents bot for launching and managing AI coding agents."
)

// OnActivate is invoked when the plugin is activated.
func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	// Ensure the bot account exists.
	botUserID, err := p.client.Bot.EnsureBot(&model.Bot{
		Username:    botUsername,
		DisplayName: botDisplayName,
		Description: botDescription,
	})
	if err != nil {
		return errors.Wrap(err, "failed to ensure bot account")
	}
	p.setBotUserID(botUserID)

	// Get bot username for mention detection.
	botUser, appErr := p.API.GetUser(botUserID)
	if appErr != nil {
		return errors.New("failed to get bot user: " + appErr.Error())
	}
	p.setBotUsername(botUser.Username)

	// Initialize the KV store.
	p.kvstore = kvstore.NewKVStore(p.client)

	// Initialize the bridge client for LLM-based prompt enrichment.
	p.bridgeClient = bridgeclient.NewClient(p.API)

	// Initialize the Cursor API client (may be nil if API key not configured yet).
	cfg := p.getConfiguration()
	if cfg.CursorAPIKey != "" {
		p.setCursorClient(cursor.NewClient(cfg.CursorAPIKey, cursor.WithLogger(&pluginLogger{plugin: p})))
	}

	// Set up the HTTP router.
	p.router = p.initRouter()

	// Get site URL for dialog callback URLs.
	siteURL := ""
	if p.client.Configuration.GetConfig().ServiceSettings.SiteURL != nil {
		siteURL = *p.client.Configuration.GetConfig().ServiceSettings.SiteURL
	}

	// Register slash commands (Phase 3).
	p.commandHandler = command.NewHandler(command.Dependencies{
		Client:       p.client,
		CursorClient: p.cursorClient,
		Store:        p.kvstore,
		BotUserID:    botUserID,
		SiteURL:      siteURL,
		PluginID:     "com.mattermost.plugin-cursor",
	})

	// Schedule background poller for agent status updates.
	pollInterval := time.Duration(cfg.GetPollInterval()) * time.Second
	job, cronErr := cluster.Schedule(
		p.API,
		"CursorAgentStatusPoller",
		cluster.MakeWaitForRoundedInterval(pollInterval),
		p.pollAgentStatuses,
	)
	if cronErr != nil {
		return errors.Wrap(cronErr, "failed to schedule agent status poller")
	}
	p.backgroundJob = job

	return nil
}

// OnDeactivate is invoked when the plugin is deactivated.
func (p *Plugin) OnDeactivate() error {
	if p.backgroundJob != nil {
		if err := p.backgroundJob.Close(); err != nil {
			p.API.LogError("Failed to close background job", "error", err.Error())
		}
	}
	return nil
}

// ServeHTTP handles HTTP requests routed to the plugin.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

// ExecuteCommand handles slash command execution.
func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	if p.commandHandler == nil {
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         "Plugin is not fully initialized.",
		}, nil
	}

	resp, err := p.commandHandler.Handle(args)
	if err != nil {
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         fmt.Sprintf("Error: %s", err.Error()),
		}, nil
	}
	return resp, nil
}
