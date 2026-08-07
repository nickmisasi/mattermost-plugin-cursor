# Cursor Cloud Agents Plugin for Mattermost

This plugin connects Mattermost to Cursor Cloud Agents in two ways:

- The right-hand panel lets each user manage their own agents through a backend proxy to the Cursor Cloud Agents v1 API.
- An MCP server exposes Cursor tools to the Mattermost Agents plugin.

Each user adds their own Cursor API key from Cursor Dashboard → Integrations in the plugin panel. Keys are validated, encrypted, and stored per Mattermost user. They are never returned to the browser.

## MCP tools

- `create_agent`
- `get_agent`
- `list_agents`
- `add_followup`
- `get_agent_conversation`

## Build and develop

```bash
make dist
go test ./server/...
make deploy
```

The REST API is served under `/plugins/com.mattermost.plugin-cursor/api/v1`. The plugin requires Mattermost 11.3 or newer.
