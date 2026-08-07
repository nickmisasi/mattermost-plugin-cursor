# Cursor Cloud Agents Plugin for Mattermost

This plugin connects Mattermost to Cursor Cloud Agents in two ways:

- The right-hand panel lets each user manage their own agents through a backend proxy to the Cursor Cloud Agents v1 API.
- An MCP server exposes Cursor tools to the Mattermost Agents plugin.

For the right-hand panel, each user adds their own Cursor API key from Cursor Dashboard → Integrations in the plugin panel. Personal keys are validated, encrypted, stored per Mattermost user, and never returned to the browser.

## MCP tools

An administrator configures the Service Account API Key in the Mattermost System Console. This key powers every MCP tool call made through the Mattermost Agents plugin, so invoking users do not need personal Cursor access. Create the service account key in the Cursor Dashboard.

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
