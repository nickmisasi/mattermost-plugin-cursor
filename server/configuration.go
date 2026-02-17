package main

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/ghclient"
)

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration, as well as values computed from the configuration. Any public fields will be
// deserialized from the Mattermost server configuration in OnConfigurationChange.
type configuration struct {
	CursorAPIKey            string `json:"CursorAPIKey"`
	DefaultRepository       string `json:"DefaultRepository"`
	DefaultBranch           string `json:"DefaultBranch"`
	DefaultModel            string `json:"DefaultModel"`
	AutoCreatePR            string `json:"AutoCreatePR"`
	PollIntervalSeconds     int    `json:"PollIntervalSeconds"`
	GitHubWebhookSecret     string `json:"GitHubWebhookSecret"`
	CursorAgentSystemPrompt string `json:"CursorAgentSystemPrompt"`
	EnableDebugLogging      string `json:"EnableDebugLogging"`
	EnableContextReview     string `json:"EnableContextReview"`
	EnablePlanLoop          string `json:"EnablePlanLoop"`
	PlannerSystemPrompt     string `json:"PlannerSystemPrompt"`

	// --- AI Review Loop settings ---
	GitHubPAT           string `json:"GitHubPAT"`
	EnableAIReviewLoop  string `json:"EnableAIReviewLoop"`
	MaxReviewIterations int    `json:"MaxReviewIterations"`
	AIReviewerBots      string `json:"AIReviewerBots"`
	HumanReviewTeam     string `json:"HumanReviewTeam"`
}

// boolFromStr converts a Mattermost plugin config string ("true"/"false") to bool.
// Mattermost stores "type": "bool" plugin settings as string values.
func boolFromStr(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "true")
}

// Clone shallow copies the configuration.
func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

// IsValid checks that required configuration is present and well-formed.
func (c *configuration) IsValid() error {
	if c.CursorAPIKey == "" {
		return fmt.Errorf("cursor API Key is required. Get one from cursor.com/dashboard -> Integrations")
	}

	if c.PollIntervalSeconds < 10 {
		return fmt.Errorf("poll interval must be at least 10 seconds, got %d", c.PollIntervalSeconds)
	}

	// Validate DefaultRepository format if set.
	if c.DefaultRepository != "" {
		parts := strings.Split(c.DefaultRepository, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("default Repository must be in 'owner/repo' format, got %q", c.DefaultRepository)
		}
	}

	return nil
}

// GetPollInterval returns the poll interval, defaulting to 30 if unset or below minimum.
func (c *configuration) GetPollInterval() int {
	if c.PollIntervalSeconds < 10 {
		return 30
	}
	return c.PollIntervalSeconds
}

// ParseAIReviewerBots splits the AIReviewerBots config string into individual
// bot usernames, trimming whitespace and filtering empties.
func (c *configuration) ParseAIReviewerBots() []string {
	if c.AIReviewerBots == "" {
		return nil
	}
	parts := strings.Split(c.AIReviewerBots, ",")
	var bots []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			bots = append(bots, trimmed)
		}
	}
	return bots
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	cfg := new(configuration)

	if err := p.API.LoadPluginConfiguration(cfg); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	// Apply defaults for fields that have default values in plugin.json
	// but may not be set yet (e.g., fresh install).
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "auto"
	}
	if cfg.PollIntervalSeconds == 0 {
		cfg.PollIntervalSeconds = 30
	}
	if cfg.MaxReviewIterations == 0 {
		cfg.MaxReviewIterations = 5
	}
	if cfg.MaxReviewIterations < 1 {
		cfg.MaxReviewIterations = 1
	}
	if cfg.MaxReviewIterations > 20 {
		cfg.MaxReviewIterations = 20
	}
	if cfg.AIReviewerBots == "" {
		cfg.AIReviewerBots = "coderabbitai[bot],copilot-pull-request-reviewer"
	}

	// Validate the configuration.
	if err := cfg.IsValid(); err != nil {
		p.API.LogWarn("Invalid plugin configuration", "error", err.Error())
		// Do NOT return an error here. Returning an error from OnConfigurationChange
		// would prevent the plugin from activating at all. Instead, log the warning
		// and let the plugin run in a degraded state. The health endpoint will
		// report the specific issue.
	}

	if boolFromStr(cfg.EnableAIReviewLoop) && cfg.GitHubPAT == "" {
		p.API.LogWarn("EnableAIReviewLoop is enabled but GitHubPAT is not set; review loop will not activate")
	}
	if boolFromStr(cfg.EnableAIReviewLoop) && cfg.CursorAPIKey == "" {
		p.API.LogWarn("EnableAIReviewLoop is enabled but CursorAPIKey is not set; review loop will not activate")
	}

	// Validate the Cursor API key by making a lightweight API call.
	// Only do this if the API key is non-empty and has changed.
	oldConfig := p.getConfiguration()
	if cfg.CursorAPIKey != "" && (oldConfig == nil || cfg.CursorAPIKey != oldConfig.CursorAPIKey) {
		go p.validateAPIKey(cfg.CursorAPIKey)
	}

	p.setConfiguration(cfg)

	// Re-initialize the Cursor client with the new API key if the plugin is activated.
	if cfg.CursorAPIKey != "" && p.client != nil {
		p.setCursorClient(cursor.NewClient(cfg.CursorAPIKey, cursor.WithLogger(&pluginLogger{plugin: p})))
	} else {
		p.setCursorClient(nil)
	}

	// Re-initialize the GitHub client with the new PAT.
	if cfg.GitHubPAT != "" {
		p.setGitHubClient(ghclient.NewClient(cfg.GitHubPAT))
	} else {
		p.setGitHubClient(nil)
	}

	return nil
}

// validateAPIKey checks if the Cursor API key is valid by calling GET /v0/me.
// This is done in a goroutine to avoid blocking OnConfigurationChange.
func (p *Plugin) validateAPIKey(apiKey string) {
	client := cursor.NewClient(apiKey, cursor.WithLogger(&pluginLogger{plugin: p}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.GetMe(ctx)
	if err != nil {
		p.API.LogWarn("Cursor API key validation failed",
			"error", err.Error(),
			"hint", "Check that your API key is correct at cursor.com/dashboard -> Integrations",
		)
		return
	}
	p.API.LogInfo("Cursor API key validated successfully")
}
