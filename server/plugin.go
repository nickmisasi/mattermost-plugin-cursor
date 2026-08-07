package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
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

	client       *pluginapi.Client
	router       *mux.Router
	mcpServer    *pluginmcp.Server
	keyStore     *KeyStore
	httpClient   *http.Client
	cache        responseCache
	hydration    hydrationCache
	getMCPUserID func(context.Context) string
}

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)
	if err := p.OnConfigurationChange(); err != nil {
		return err
	}

	keyStore, err := NewKeyStore(p.API)
	if err != nil {
		return err
	}
	p.keyStore = keyStore
	if p.httpClient == nil {
		p.httpClient = http.DefaultClient
	}
	p.cache.initialize(5 * time.Minute)
	p.hydration.initialize(hydrationCacheCapacity)
	p.initRouter()
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
	if p.router == nil {
		http.Error(w, "plugin is not initialized", http.StatusServiceUnavailable)
		return
	}
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) cursorClient(apiKey string) *cursorapi.Client {
	return cursorapi.NewClient(p.getConfiguration().CursorAPIBaseURL, apiKey, p.httpClient)
}
