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
  hitl.go            # HITL workflow orchestration functions
  store/kvstore/     # KV store interface + implementation (includes HITL workflow storage)
  attachments/       # Slack-style attachment builders (includes HITL context/plan review)

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

## HITL Workflow

### Overview
The HITL (Human-In-The-Loop) feature adds two optional verification stages before agent implementation:
1. **Context Review** (`context_review`): Bot posts enriched context for user approval
2. **Plan Loop** (`planning` -> `plan_review`): Planner agent produces implementation plan for user approval

Both stages are independently togglable via global config (`EnableContextReview`, `EnablePlanLoop`), user settings, or per-mention flags (`--no-review`, `--no-plan`, `--direct`).

### Phase Constants
- `context_review` -- Waiting for user to approve enriched context
- `planning` -- Planner Cursor agent is running
- `plan_review` -- Waiting for user to approve plan
- `implementing` -- Implementation Cursor agent is running
- `rejected` -- User rejected at any stage
- `complete` -- Implementation finished

### Key Files
- `server/hitl.go` -- Workflow orchestration functions
- `server/store/kvstore/kvstore.go` -- `HITLWorkflow` struct, phase constants, KV interface
- `server/attachments/attachments.go` -- HITL-specific attachment builders
- `server/api.go` -- `POST /api/v1/actions/hitl-response` button handler

### Thread Mapping Convention
The `thread:` KV prefix stores either:
- A bare Cursor agent ID (legacy fire-and-forget flow)
- A value starting with `hitl:` prefix (HITL workflow ID)

Code that reads `GetAgentIDByThread` must check for the `hitl:` prefix to distinguish workflows from direct agents.

### Configuration Resolution
HITL flags resolve in priority order: per-mention flags > user settings > global config.
- `--direct` skips both HITL stages
- `--no-review` skips context review only
- `--no-plan` skips plan loop only

### Planner Agent Constraints
- `autoCreatePr: false` -- planner should NOT create PRs
- `autoBranch: false` -- prevents orphan branch creation per iteration
- Strong "DO NOT MODIFY CODE" system prompt
- Plan extracted from LAST `assistant_message` in conversation API response

## Common Pitfalls

- **No backticks in plugin.json help_text**: Mattermost's System Console chokes on backtick characters in settings help text. Use plain descriptions instead.
- **Always nil-check CursorClient**: It's nil when the API key is empty.
- **Use native Mattermost button classes**: `btn btn-primary`, `btn btn-danger`, `btn btn-link`, `btn btn-tertiary`. Do NOT create custom button CSS.
- **CSS variables only**: Never hardcode colors. Use `var(--center-channel-color)`, `var(--button-bg)`, etc.
- **setConfiguration panic**: Calling `setConfiguration` with the same pointer panics. Always pass a new/cloned struct.
- **Mock interface updates**: When the `kvstore.KVStore` interface changes, mock implementations must be updated in ALL test files that use them (see `server/store/kvstore/CLAUDE.md`).
- **Webpack externals**: React, Redux, ReactRedux, ReactDOM are provided by the Mattermost host app. Do not bundle them.
- **Thread mapping prefix**: Values from `GetAgentIDByThread` starting with `hitl:` are workflow IDs, not agent IDs. Always check the prefix before using as an agent ID.
- **Plan iteration creates NEW agents**: Follow-ups only work on RUNNING agents. Since planners FINISH, iteration requires creating a new planner agent with accumulated context.
- **autoBranch: false for planners**: The Cursor API defaults `autoBranch: true`, creating orphan branches. Always set `autoBranch: false` in planner launch requests.
- **PendingFeedback field**: Thread replies during `planning` phase are queued in `HITLWorkflow.PendingFeedback`. They auto-trigger a new planner iteration when the current planner finishes.
- **AddReaction mock returns**: When mocking `AddReaction` in command tests (which use `pluginapi.Client`), always return `&model.Reaction{}` not `nil` -- `pluginapi.PostService.AddReaction` dereferences the result.

## Skills

See `.claude/commands/` for step-by-step guides on common tasks.

## Planning Reference

See `.planning/PLAN.md` for the master implementation plan (6 phases, all implemented). Phase subdirectories contain detailed implementation plans.
