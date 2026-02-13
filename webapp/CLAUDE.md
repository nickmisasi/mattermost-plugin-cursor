# Webapp (React/TypeScript Frontend)

## Stack

- React 18 + TypeScript 4.9
- Redux 5 with react-redux 9
- Webpack 5
- No external component libraries (no Material UI, etc.)

## Plugin Registration (`index.tsx`)

The `Plugin` class registers everything in `initialize()`:

1. `registerReducer(reducer)` -- Redux store for agent state
2. `registerRightHandSidebarComponent(RHSPanel)` -- RHS panel
3. `registerAppBarComponent(icon, toggleAction)` -- App Bar icon that toggles RHS
4. `registerPostDropdownMenuAction(...)` -- Three post menu actions:
   - "Add Follow-up to Cursor Agent" (visible when agent RUNNING)
   - "Cancel Cursor Agent" (visible when agent RUNNING or CREATING)
   - "View Agent Details" (visible when any cursor_agent_id prop exists)
5. `registerWebSocketEventHandler(...)` -- Two WS event handlers
6. `registerReconnectHandler(...)` -- Refetches agents on reconnect
7. Initial `fetchAgents()` dispatch

## Redux State

### State Shape (`types.ts`)
```typescript
interface PluginState {
    agents: Record<string, Agent>;    // keyed by agent ID
    selectedAgentId: string | null;   // for RHS detail view
    isLoading: boolean;
}
```

### Reducer (`reducer.ts`)
Handles: `AGENTS_RECEIVED`, `AGENT_RECEIVED`, `AGENT_STATUS_CHANGED`, `AGENT_CREATED`, `AGENT_REMOVED`, `SELECT_AGENT`, `SET_LOADING`

### Actions (`actions.ts`)
- Sync: `selectAgent(id)`
- Thunks: `fetchAgents()`, `fetchAgent(id)`, `addFollowup(id, msg)`, `cancelAgent(id)`
- WebSocket: `websocketAgentStatusChange(data)`, `websocketAgentCreated(data)`

All action types are namespaced: `com.mattermost.plugin-cursor/ACTION_NAME`

### Selectors (`selectors.ts`)
- `getAgents(state)` -- all agents as Record
- `getAgentsList(state)` -- all agents as array
- `getActiveAgents(state)` -- CREATING or RUNNING only
- `getSelectedAgent(state)` -- currently selected agent for detail view
- `getIsLoading(state)` -- loading state
- `getAgentByPostId(state, postId)` -- find agent by its Mattermost post ID

Plugin state key: `plugins-com.mattermost.plugin-cursor`

## Components

All in `src/components/`:

- **`rhs/RHSPanel.tsx`**: Root RHS component. Shows AgentList or AgentDetail based on selection.
- **`rhs/AgentList.tsx`**: Sorted list of all agents (newest first). Shows empty state with instructions.
- **`rhs/AgentCard.tsx`**: Card for a single agent showing status badge, repo, elapsed time, prompt preview, PR link.
- **`rhs/AgentDetail.tsx`**: Expanded view of selected agent. Shows all fields, follow-up textarea (RUNNING only), cancel button (active only), external links.
- **`common/StatusBadge.tsx`**: Colored dot indicator for agent status.
- **`common/styles.css`**: All plugin CSS.

## Styling Rules

**MUST use Mattermost CSS variables. NEVER hardcode colors.**

Key variables:
- Text: `var(--center-channel-color)`, `rgba(var(--center-channel-color-rgb), 0.56)` for secondary
- Background: `var(--center-channel-bg)`
- Borders: `rgba(var(--center-channel-color-rgb), 0.12)`
- Links: `var(--link-color)`
- Buttons: `var(--button-bg)`, `var(--button-color)`
- Status dots: `var(--online-indicator)` (running), `var(--away-indicator)` (creating), `var(--dnd-indicator)` (failed)
- Muted text: `rgba(var(--center-channel-color-rgb), 0.4)`

**Use native Mattermost button classes**, not custom ones:
- `btn btn-primary` -- main actions
- `btn btn-danger` -- destructive actions (cancel)
- `btn btn-link` -- text-style buttons (back)
- `btn btn-tertiary` -- secondary actions

Spacing follows 4px grid: 4, 8, 12, 16, 20, 24, 32, 40px.

## REST Client (`client.ts`)

Uses `fetch()` with `Client4.getOptions()` for Mattermost auth headers:

```typescript
const response = await fetch(url, Client4.getOptions({method: 'GET'}));
```

Base path: `/plugins/com.mattermost.plugin-cursor/api/v1`

Endpoints:
- `GET /agents` -- list user's agents
- `GET /agents/{id}` -- get single agent
- `POST /agents/{id}/followup` -- send follow-up (body: `{message}`)
- `DELETE /agents/{id}` -- cancel agent

## WebSocket Events (`websocket.ts`)

Two registered handlers:
- `custom_com.mattermost.plugin-cursor_agent_status_change` -- dispatches `AGENT_STATUS_CHANGED`
- `custom_com.mattermost.plugin-cursor_agent_created` -- dispatches `AGENT_CREATED`

## Build Commands

```bash
cd webapp
npm run build              # Production build
npm run debug              # Development build (no minification)
npm test                   # Run tests
npm test -- --watchAll=false  # CI mode
npm run lint               # ESLint
npm run check-types        # TypeScript type check
```

## Webpack Externals

React, ReactDOM, Redux, ReactRedux, PropTypes, ReactBootstrap, and ReactRouterDom are **provided by the Mattermost host app**. They are declared as webpack externals and must NOT be bundled.

This means:
- Do not import them in a way that assumes they're bundled
- They are available as globals at runtime
- Adding new external dependencies to the bundle is fine, but these specific ones must remain external

## Post Dropdown Menu Actions

Post actions use `post.props.cursor_agent_id` and `post.props.cursor_agent_status` to determine visibility. These props are set by the server on bot posts when an agent is created, and updated by the poller when status changes.

The filter function receives a `postId` and must return `true`/`false`. It reads post data from the Redux store via `state.entities.posts.posts[postId]`.

## Common Pitfalls

- **Webpack externals**: Do NOT try to bundle React, Redux, or mattermost-redux. They are host-provided.
- **No hardcoded colors**: Every color must come from a CSS variable. This ensures dark mode compatibility.
- **No custom button styles**: Use `btn btn-primary` etc. from Mattermost's CSS.
- **Plugin state key**: The Redux state lives at `plugins-com.mattermost.plugin-cursor`, not `plugins/com.mattermost.plugin-cursor`.
- **WS event naming**: Events are `custom_` + plugin ID + `_` + event name. The plugin ID uses dots, not hyphens.
- **Type casting**: The store state is cast to `any` in several places due to plugin registry type limitations. This is expected.
- **Client4.getOptions**: Always wrap fetch options with `Client4.getOptions()` to include auth headers. Do not use raw fetch.
