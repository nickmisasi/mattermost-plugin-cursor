# Cursor Background Agents Plugin for Mattermost

`mattermost-plugin-cursor` brings Cursor Background Agents into Mattermost so users can launch, monitor, and iterate on coding tasks directly from chat threads.

## What this plugin does

- Launches Cursor background agents from `@cursor <prompt>` mentions.
- Keeps all bot interactions thread-first for clear task history.
- Supports follow-up prompts, cancellation, and status tracking.
- Publishes status updates to the Mattermost UI (thread messages + websocket events).
- Optionally runs Human-in-the-Loop (HITL) review stages:
  - Context review before implementation
  - Planning loop with user approval before coding

## Project structure

- `server/` - Go backend plugin code (hooks, API routes, Cursor client, poller, KV store, HITL workflow)
- `webapp/` - React/TypeScript UI (RHS panel, Redux state, websocket handlers, post actions)
- `assets/` - Plugin assets (including icon)
- `public/` - Public bundle assets for the webapp

## Requirements

- Mattermost server `>= 9.6.0`
- Go `1.24.11`
- Node.js and npm (for webapp build/test)
- Cursor API key (configured in plugin settings)

## Build and test

```bash
# Build distributable plugin bundle
make dist

# Run all server tests
go test ./server/...

# Run webapp tests
cd webapp && npm test -- --watchAll=false

# Lint and type checks
make check-style
```

## Development workflow

```bash
# Build and deploy to a local Mattermost instance
make deploy
```

The plugin ID is `com.mattermost.plugin-cursor`, and API endpoints are served under:

`/plugins/com.mattermost.plugin-cursor/api/v1`

## License

See [LICENSE](./LICENSE).
