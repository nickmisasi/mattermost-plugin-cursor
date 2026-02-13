# Cursor Background Agents Plugin for Mattermost

## Project Overview

Mattermost plugin that mirrors the Cursor Slack integration, allowing users to launch, manage, and interact with Cursor Background Agents from chat.

- **Plugin ID**: `com.mattermost.plugin-cursor`
- **Module**: `github.com/mattermost/mattermost-plugin-cursor`
- **Go version**: 1.24.11
- **Min Mattermost version**: 9.6.0

## Architecture

- **Go backend** (`server/`): Plugin hooks, HTTP API, Cursor API client, KV store, background poller
- **React/TypeScript webapp** (`webapp/`): RHS panel, post dropdown actions, WebSocket handlers, Redux state
- **Bot account**: username `cursor`, drives all chat interactions

Primary interface: `@cursor <prompt>` bot mention (mirrors Slack). Slash commands (`/cursor`) for management. All responses in threads.

## Key File Structure

```
server/
  plugin.go          # Plugin struct, OnActivate, ServeHTTP, ExecuteCommand
  configuration.go   # Config struct, OnConfigurationChange, validation
  handlers.go        # MessageHasBeenPosted, agent launch flow, follow-ups, thread enrichment
  api.go             # HTTP router (gorilla/mux), REST endpoints, middleware
  poller.go          # Background agent status polling via cluster.Schedule
  dialog.go          # Settings dialog submission handler
  webhook.go         # GitHub webhook receiver (HMAC verification, PR events)
  command/command.go  # /cursor slash command handler (list, status, cancel, settings, models, help)
  cursor/client.go   # Cursor API HTTP client (interface-based)
  cursor/types.go    # Cursor API request/response types
  parser/parser.go   # @cursor mention parser (repo, branch, model, prompt extraction)
  store/kvstore/     # KV store interface + implementation

webapp/src/
  index.tsx           # Plugin registration (reducer, RHS, app bar, post actions, WS handlers)
  reducer.ts          # Redux reducer for plugin state
  actions.ts          # Action creators (sync + thunks)
  selectors.ts        # Redux selectors
  types.ts            # TypeScript interfaces
  client.ts           # REST client for plugin API
  websocket.ts        # WebSocket event handler registration
  components/rhs/     # RHS panel components (RHSPanel, AgentList, AgentCard, AgentDetail)
  components/common/  # StatusBadge, styles.css
```

## Build & Test Commands

```bash
# Full build + deploy to local Mattermost
make deploy

# Run all Go tests
go test ./server/...

# Run webapp tests
cd webapp && npm test -- --watchAll=false

# Lint
make check-style

# Build without deploying
make dist
```

## Critical Patterns

### Synchronized Accessors
All shared state on the Plugin struct is accessed through getter/setter methods that use `configurationLock`:
- `getCursorClient()` / `setCursorClient()`
- `getBotUserID()` / `setBotUserID()`
- `getBotUsername()` / `setBotUsername()`
- `getConfiguration()` / `setConfiguration()`

Always use these accessors, never access fields directly from concurrent code.

### Nil-Check CursorClient
The Cursor client is nil when no API key is configured. Every code path that uses it must check:
```go
cursorClient := p.getCursorClient()
if cursorClient == nil {
    // handle gracefully
}
```

### KV Store Prefix Strategy
All KV keys use a prefix convention. See `server/store/kvstore/CLAUDE.md` for the full prefix list. Never create KV keys without prefixes.

### Thread-First Messaging
All bot responses go in threads (using `RootId`). Never post to channels directly. The bot's first reply uses the user's post ID (or its RootId) as the thread root.

### Default Resolution Cascade
Settings resolve in priority order: parsed mention > channel settings > user settings > global config.

### WebSocket Events
Server publishes `agent_status_change` and `agent_created` events. The full event name is `custom_com.mattermost.plugin-cursor_<event_name>`.

## Common Pitfalls

- **No backticks in plugin.json help_text**: Mattermost's System Console chokes on backtick characters in settings help text. Use plain descriptions instead.
- **Always nil-check CursorClient**: It's nil when the API key is empty.
- **Use native Mattermost button classes**: `btn btn-primary`, `btn btn-danger`, `btn btn-link`, `btn btn-tertiary`. Do NOT create custom button CSS.
- **CSS variables only**: Never hardcode colors. Use `var(--center-channel-color)`, `var(--button-bg)`, etc.
- **setConfiguration panic**: Calling `setConfiguration` with the same pointer panics. Always pass a new/cloned struct.
- **Mock interface updates**: When the `kvstore.KVStore` interface changes, mock implementations must be updated in ALL test files that use them (see `server/store/kvstore/CLAUDE.md`).
- **Webpack externals**: React, Redux, ReactRedux, ReactDOM are provided by the Mattermost host app. Do not bundle them.

## Skills

See `.claude/commands/` for step-by-step guides on common tasks.

## Planning Reference

See `.planning/PLAN.md` for the master implementation plan (6 phases, all implemented). Phase subdirectories contain detailed implementation plans.
