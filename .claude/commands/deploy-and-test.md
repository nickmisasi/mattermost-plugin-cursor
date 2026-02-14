---
description: "Full development workflow: run Go and webapp tests, build the plugin, deploy to a local Mattermost instance, and verify functionality."
---

# Deploy and Test the Plugin

This skill covers the full development workflow: running tests, building, deploying, and verifying.

## Running Tests

### Go Server Tests

```bash
go test ./server/...
```

Run with verbose output:

```bash
go test -v ./server/...
```

Run a specific test:

```bash
go test -v -run TestHelp_NoArgs ./server/command/...
```

Run with race detection (default in Makefile):

```bash
go test -race ./server/...
```

### Webapp Tests

```bash
cd webapp && npm test
```

Run a specific test file:

```bash
cd webapp && npm test -- --testPathPattern=reducer
```

Run in watch mode:

```bash
cd webapp && npm test -- --watch
```

## Building

### Full Build (creates distributable tar.gz)

```bash
make dist
```

This:
1. Runs `go generate` for manifest
2. Compiles Go binaries for all platforms (linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64)
3. Builds the webapp (`cd webapp && npm install && npm run build`)
4. Packages everything into `dist/com.mattermost.plugin-cursor-{version}.tar.gz`

### Server Only

```bash
make server
```

### Webapp Only

```bash
cd webapp && npm run build
```

## Deploying to Local Mattermost

### Prerequisites

You need a local Mattermost server running. The deploy commands use environment variables:

```bash
# In your .env or shell:
export MM_SERVICESETTINGS_SITEURL=http://localhost:8065
export MM_ADMIN_USERNAME=admin
export MM_ADMIN_PASSWORD=your-password
```

Or the plugin uses the `.env` file in the project root.

### Deploy

```bash
make deploy
```

This builds the plugin and uploads it to the Mattermost server via the API.

### Watch Mode

For rapid development, use watch mode which rebuilds and redeploys on file changes:

```bash
make watch
```

## Viewing Logs

### Tail Logs

```bash
make logs-watch
```

This runs the `pluginctl` tool to tail the Mattermost server log, filtered for plugin messages.

### Enable Debug Logging

In System Console > Plugins > Cursor Background Agents:
1. Set **Enable Debug Logging** to `true`
2. Save

This enables detailed logging of API requests/responses, parsed mentions, default resolution, and more.

## Verifying After Deploy

### 1. Check System Console

Navigate to **System Console > Plugins > Cursor Background Agents**:
- Verify the plugin is **Enabled**
- Verify all settings are correctly configured
- If the plugin failed to activate, the error will be shown here

### 2. Check Health Endpoint

As a system admin, verify the plugin health:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://localhost:8065/plugins/com.mattermost.plugin-cursor/api/v1/admin/health
```

Expected:
```json
{
  "healthy": true,
  "cursor_api": {"ok": true},
  "active_agent_count": 0,
  "configuration": {"ok": true},
  "plugin_version": "0.1.0"
}
```

### 3. Test Slash Commands

```
/cursor help
```
Should show the help text with all available commands.

```
/cursor models
```
Should list available Cursor AI models (requires valid API key).

```
/cursor list
```
Should show your agents (empty if none launched yet).

```
/cursor settings
```
Should open the settings dialog with channel and user default fields.

### 4. Test Bot Mention

In any channel:
```
@cursor fix the login bug repo=org/repo
```
Should:
- Add an hourglass reaction to your message
- Post a reply with "Starting a background agent..." and an "Open in Cursor" link
- The agent should appear in `/cursor list`

### 5. Test the RHS Panel

- Click the Cursor icon in the App Bar (top-right)
- The RHS panel should show your agents
- Click an agent to see details
- If an agent is RUNNING, the follow-up textarea should appear

### 6. Test WebSocket Updates

- Launch an agent
- Watch the RHS panel -- it should update in real-time as the agent status changes
- The status badge should change from CREATING -> RUNNING -> FINISHED

## The plugin.json Backtick Gotcha

The `plugin.json` file is parsed by `build/manifest/main.go` to generate `server/manifest.go`. A critical constraint:

**Do NOT use backtick characters (`) in `help_text` fields in `plugin.json`.**

Backticks in the `settings_schema` will break the Go manifest generation because the generated Go file uses backtick-delimited raw strings. If you need inline code formatting in help text, use single quotes or avoid code formatting entirely.

Bad:
```json
"help_text": "Use `auto` for the default model"
```

Good:
```json
"help_text": "Use 'auto' for the default model"
```

Or:
```json
"help_text": "Use auto for the default model. See /cursor models for options."
```

## Common Issues

### "Plugin failed to start"

- Check logs with `make logs-watch`
- Common cause: `OnActivate` returned an error (e.g., bot creation failed)
- The plugin is designed to NOT fail on invalid config -- it runs in "degraded mode" instead

### "Cannot find module" in webapp build

```bash
cd webapp && rm -rf node_modules && npm install
```

### Tests fail with "too many arguments in call to LogDebug"

The `plugintest.API` mocks need flexible argument matching for log calls. Ensure your test setup includes:

```go
api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
```

You may need to add more `mock.Anything` arguments if your log calls have more key-value pairs.

### Plugin deploys but slash command does not appear

- The slash command is registered in `OnActivate` via `command.NewHandler()` which calls `deps.Client.SlashCommand.Register(getCommand())`
- Check logs for "Failed to register /cursor command"
- Try disabling and re-enabling the plugin in System Console

### WebSocket events not reaching the webapp

- Event name format must be `custom_{pluginId}_{eventName}`
- The server publishes with just `eventName` (e.g., `"agent_status_change"`)
- The webapp registers for `'custom_' + manifest.id + '_agent_status_change'`
- Check browser devtools Network tab > WS for WebSocket frames

## CI/CD

The project has a GitHub Actions workflow at `.github/workflows/ci.yml`. It runs on push to any branch and on PRs. Make sure tests pass locally before pushing:

```bash
go test -race ./server/...
cd webapp && npm test
```

## Quick Development Loop

For the fastest iteration:

1. Make your change
2. Run the relevant tests:
   - Go: `go test ./server/path/to/package/...`
   - Webapp: `cd webapp && npm test`
3. Deploy: `make deploy`
4. Check logs: `make logs-watch`
5. Test in the Mattermost UI

For webapp-only changes, you can rebuild just the webapp:
```bash
cd webapp && npm run build && cd .. && make deploy
```
