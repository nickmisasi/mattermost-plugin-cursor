# Server (Go Backend)

## Plugin Lifecycle

### OnActivate (`plugin.go`)
Runs when the plugin is enabled. Initialization order matters:
1. Create `pluginapi.Client` wrapper
2. `EnsureBot` for the `cursor` bot account
3. Get bot username for mention detection
4. Initialize KV store
5. Initialize bridge client (LLM enrichment)
6. Initialize Cursor API client (if API key is set)
7. Set up HTTP router (`initRouter()`)
8. Create command handler (`command.NewHandler()`)
9. Schedule background poller (`cluster.Schedule`)

### OnDeactivate (`plugin.go`)
Closes the background poller job.

### OnConfigurationChange (`configuration.go`)
Called whenever admin saves plugin settings. Does NOT block activation on invalid config -- logs warnings and runs in degraded mode. Re-initializes the Cursor client on API key change. Validates the API key asynchronously in a goroutine.

## Key Hooks

### MessageHasBeenPosted (`handlers.go`)
The core hook. Flow:
1. Skip bot's own posts and system messages (`ShouldProcessMessage`)
2. Check for `@cursor` mention (case-insensitive)
3. If no mention, check for thread follow-up (`handlePossibleFollowUp`)
4. Parse mention via `parser.Parse()`
5. If in thread with active agent and not `ForceNew`, send follow-up
6. Otherwise launch new agent

### ExecuteCommand (`plugin.go`)
Dispatches to `commandHandler.Handle()` which routes to subcommands.

### ServeHTTP (`plugin.go`)
Delegates to gorilla/mux router.

## Configuration (`configuration.go`)

```go
type configuration struct {
    CursorAPIKey             string
    DefaultRepository        string
    DefaultBranch            string   // default: "main"
    DefaultModel             string   // default: "auto"
    AutoCreatePR             bool     // default: true
    PollIntervalSeconds      int      // default: 30, minimum: 10
    GitHubWebhookSecret      string
    CursorAgentSystemPrompt  string
    EnableDebugLogging       bool
}
```

Access via `p.getConfiguration()` (read-locked). Never modify the returned struct. Use `setConfiguration()` with a new struct.

## Bot Account

- Created via `p.client.Bot.EnsureBot()` in OnActivate
- Username: `cursor`, Display: `Cursor`
- Access via `p.getBotUserID()` and `p.getBotUsername()`
- All chat messages and reactions are posted as the bot user

## Cursor Client (`cursor/`)

Interface-based (`cursor.Client`). Initialized on config change. `nil` when no API key is set. Every usage must nil-check first. See `server/cursor/CLAUDE.md`.

## HTTP Routing (`api.go`)

Three subrouter tiers via gorilla/mux:
1. **Unauthenticated**: GitHub webhook endpoint (`/api/v1/webhooks/github`) -- uses HMAC signature verification instead
2. **Authenticated** (`/api/v1/...`): Requires `Mattermost-User-ID` header (middleware: `MattermostAuthorizationRequired`)
3. **Admin-only** (`/api/v1/admin/...`): Additionally requires system admin role (middleware: `RequireSystemAdmin`)

Routes:
- `POST /api/v1/webhooks/github` -- GitHub PR lifecycle webhooks
- `POST /api/v1/dialog/settings` -- Settings dialog submission
- `GET /api/v1/agents` -- List user's agents
- `GET /api/v1/agents/{id}` -- Get single agent (refreshes from Cursor API)
- `POST /api/v1/agents/{id}/followup` -- Send follow-up
- `DELETE /api/v1/agents/{id}` -- Cancel agent
- `GET /api/v1/admin/health` -- Health check (admin only)

## Background Poller (`poller.go`)

- Scheduled via `cluster.Schedule` for HA-safe execution (runs on one node)
- Interval: configurable `PollIntervalSeconds` (default 30s)
- Polls all agents in CREATING or RUNNING status via `kvstore.ListActiveAgents()`
- On status change: updates reactions, posts thread messages, updates KV store, publishes WebSocket events
- On FINISHED: swaps hourglass for checkmark, posts PR link + summary
- On FAILED: swaps hourglass for X, posts error
- On STOPPED: swaps hourglass for no_entry_sign

## Bridge Client (LLM Enrichment)

- Import: `github.com/mattermost/mattermost-plugin-ai/public/bridgeclient`
- Used in `enrichPromptViaBridge()` to turn thread context into a focused task prompt
- Graceful degradation: if bridge client fails or Agents plugin is not installed, falls back to raw thread text
- Does not block any core functionality

## Debug Logging

- Toggled via `EnableDebugLogging` config setting
- Use `p.logDebug()` helper (wraps `p.API.LogDebug` with config check)
- The Cursor API client receives a `pluginLogger` adapter that respects this setting

## Error Formatting

- `formatAPIError()` in `handlers.go` and `command/command.go` (duplicated)
- Formats `cursor.APIError` with pretty-printed JSON in markdown code blocks
- Prevents emoji parsing of error body content in Mattermost

## Common Pitfalls

- **Do not return errors from OnConfigurationChange**: This prevents plugin activation entirely. Log warnings instead.
- **Cursor client nil**: Always check `getCursorClient() != nil` before use.
- **Thread root ID**: When replying in a thread, if the post has a `RootId`, use it. If not, the post's own ID is the root.
- **Reactions on trigger post**: Reactions go on `record.TriggerPostID` (the user's original @mention), not on the bot's reply post.
- **WebSocket broadcast scope**: Events are broadcast to `record.UserID` only, not to channels.
- **AI review-loop dispatch is direct-only**: Review-loop fix iterations must dispatch through `cursorClient.AddFollowup` only. Do not reintroduce `@cursor` PR-comment relay fallback; failures are fail-fast and visible through history/logging.
