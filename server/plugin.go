package main

import (
	"net/http"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/external/pluginmcp"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursorapi"
)

const (
	pluginID    = "com.mattermost.plugin-cursor"
	mcpBasePath = "/mcp"
)

type Plugin struct {
	plugin.MattermostPlugin
	configurationState

	client     *pluginapi.Client
	mcpServer  *pluginmcp.Server
	httpClient *http.Client
	hydration  hydrationCache
}

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)
	if err := p.OnConfigurationChange(); err != nil {
		return err
	}

	if p.httpClient == nil {
		p.httpClient = http.DefaultClient
	}
	p.hydration.initialize(hydrationCacheCapacity)
	p.initMCPServer()
	return p.mcpServer.Register()
}

func (p *Plugin) OnDeactivate() error {
	if p.mcpServer == nil {
		return nil
	}
	return p.mcpServer.Unregister()
}

func (p *Plugin) ServeHTTP(_ *plugin.Context, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == mcpBasePath || strings.HasPrefix(r.URL.Path, mcpBasePath+"/") {
		if p.mcpServer == nil {
			http.Error(w, "MCP server is not initialized", http.StatusServiceUnavailable)
			return
		}
		p.mcpServer.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (p *Plugin) cursorClient(apiKey string) *cursorapi.Client {
	return cursorapi.NewClient(p.getConfiguration().CursorAPIBaseURL, apiKey, p.httpClient)
}

func (p *Plugin) logError(message string, err error) {
	if p.client != nil {
		p.client.Log.Error(message, "error", err.Error())
	}
}

func (p *Plugin) logDebug(message string, keyValuePairs ...any) {
	if p.client != nil {
		p.client.Log.Debug(message, keyValuePairs...)
	}
}
