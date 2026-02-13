# Add a New Feature to the Webapp RHS Panel

This skill walks you through adding a new feature to the right-hand sidebar (RHS) panel in the webapp.

## Overview

The webapp lives in `webapp/src/` and uses:
- React with functional components and hooks
- Redux for state management (custom reducer, no Redux Toolkit)
- CSS files with Mattermost CSS variables (no CSS modules, no styled-components)
- WebSocket events for real-time updates from the server

Key files:
- `webapp/src/index.tsx` -- Plugin registration (reducer, RHS, app bar, post actions, websocket)
- `webapp/src/reducer.ts` -- Redux reducer
- `webapp/src/actions.ts` -- Action type constants, action interfaces, sync/async action creators
- `webapp/src/selectors.ts` -- Redux selectors
- `webapp/src/types.ts` -- TypeScript interfaces
- `webapp/src/client.ts` -- HTTP client for plugin API calls
- `webapp/src/websocket.ts` -- WebSocket event handler registration
- `webapp/src/components/rhs/` -- RHS panel components
- `webapp/src/components/common/` -- Shared components and CSS

## Step 1: Define Types

In `webapp/src/types.ts`, add any new interfaces:

```typescript
export interface YourNewData {
    id: string;
    value: string;
}
```

If you are extending the plugin state, update `PluginState`:

```typescript
export interface PluginState {
    agents: Record<string, Agent>;
    selectedAgentId: string | null;
    isLoading: boolean;
    yourNewField: YourNewType;  // <-- ADD HERE
}
```

## Step 2: Add Actions

In `webapp/src/actions.ts`:

### 2a. Add the Action Type Constant

```typescript
export const YOUR_ACTION = 'com.mattermost.plugin-cursor/YOUR_ACTION';
```

The convention is `{plugin_id}/{ACTION_NAME}`.

### 2b. Add the Action Interface

```typescript
interface YourAction {
    type: typeof YOUR_ACTION;
    data: {
        field: string;
    };
}
```

### 2c. Add to the Union Type

```typescript
export type PluginAction =
    | AgentsReceivedAction
    | AgentReceivedAction
    // ... existing ...
    | YourAction;  // <-- ADD HERE
```

### 2d. Add an Action Creator

For sync actions:
```typescript
export const yourAction = (field: string): YourAction => ({
    type: YOUR_ACTION,
    data: {field},
});
```

For async actions (thunks):
```typescript
export function fetchYourData() {
    return async (dispatch: (action: PluginAction) => void) => {
        try {
            const response = await Client.yourMethod();
            dispatch({type: YOUR_ACTION, data: response});
        } catch (error) {
            console.error('Failed to fetch:', error); // eslint-disable-line no-console
        }
    };
}
```

## Step 3: Update the Reducer

In `webapp/src/reducer.ts`, add a case to the switch:

```typescript
export default function reducer(state: PluginState = initialState, action: PluginAction): PluginState {
    switch (action.type) {
    // ... existing cases ...
    case YOUR_ACTION:
        return {...state, yourNewField: action.data};
    default:
        return state;
    }
}
```

Update `initialState` if you added a new field to `PluginState`:

```typescript
const initialState: PluginState = {
    agents: {},
    selectedAgentId: null,
    isLoading: false,
    yourNewField: null,  // <-- ADD HERE
};
```

## Step 4: Add Selectors

In `webapp/src/selectors.ts`:

```typescript
export const getYourNewField = (state: GlobalState): YourNewType => {
    return getPluginState(state).yourNewField;
};
```

The `getPluginState` helper accesses the plugin's slice of the Redux store:

```typescript
const getPluginState = (state: GlobalState): PluginState => {
    return (state as any)['plugins-' + manifest.id] || {agents: {}, selectedAgentId: null, isLoading: false};
};
```

## Step 5: Create the Component

Create a new file in the appropriate directory:
- `webapp/src/components/rhs/` for RHS panel components
- `webapp/src/components/common/` for shared/reusable components

Follow the established component pattern:

```typescript
import React, {useState} from 'react';
import {useSelector, useDispatch} from 'react-redux';

import type {Agent} from '../../types';
import {yourAction} from '../../actions';
import {getYourNewField} from '../../selectors';

interface Props {
    agent: Agent;
    onSomeAction: () => void;
}

const YourComponent: React.FC<Props> = ({agent, onSomeAction}) => {
    const dispatch = useDispatch();
    const [localState, setLocalState] = useState('');
    const data = useSelector(getYourNewField);

    const handleClick = () => {
        dispatch(yourAction(localState) as any);
    };

    return (
        <div className='cursor-your-component'>
            <div className='cursor-your-component-label'>{'Label'}</div>
            <div className='cursor-your-component-value'>{agent.repository}</div>
            <button
                className='btn btn-primary'
                onClick={handleClick}
                disabled={!localState.trim()}
            >
                {'Button Text'}
            </button>
        </div>
    );
};

export default YourComponent;
```

### Key Conventions

- Use functional components with hooks, never class components
- Use `useSelector` and `useDispatch` for Redux
- Use `'string'` (single quotes) for JSX string children, wrapped in `{}`
- Cast dispatch calls to `any` for thunks: `dispatch(someThunk() as any)`
- Use `window.confirm()` / `window.prompt()` with `// eslint-disable-line no-alert`

## Step 6: Add CSS

Add styles to `webapp/src/components/common/styles.css`. Use ONLY Mattermost CSS variables for colors.

### Available Mattermost CSS Variables

**Primary colors:**
- `var(--center-channel-bg)` -- Background color
- `var(--center-channel-color)` -- Text color
- `var(--center-channel-color-rgb)` -- RGB values for use with `rgba()`
- `var(--link-color)` -- Link color
- `var(--button-bg)` -- Primary button background / focus color

**Status indicators:**
- `var(--online-indicator)` -- Green (used for RUNNING status)
- `var(--away-indicator)` -- Yellow (used for CREATING status)
- `var(--dnd-indicator)` -- Red (used for FAILED status)

**Common patterns from the codebase:**

```css
/* Subdued text */
color: rgba(var(--center-channel-color-rgb), 0.56);

/* Very subtle text */
color: rgba(var(--center-channel-color-rgb), 0.4);

/* Borders */
border-bottom: 1px solid rgba(var(--center-channel-color-rgb), 0.12);

/* Hover background */
background-color: rgba(var(--center-channel-color-rgb), 0.04);

/* Input borders */
border: 1px solid rgba(var(--center-channel-color-rgb), 0.2);
```

### CSS Class Naming

All classes are prefixed with `cursor-`:

```css
.cursor-your-component {
    padding: 12px 16px;
}

.cursor-your-component-label {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: rgba(var(--center-channel-color-rgb), 0.56);
    margin-bottom: 4px;
}

.cursor-your-component-value {
    font-size: 13px;
    color: var(--center-channel-color);
}
```

## Step 7: Wire into the RHS Panel

In `webapp/src/components/rhs/RHSPanel.tsx`, import and render your component:

```typescript
import YourComponent from './YourComponent';

const RHSPanel: React.FC = () => {
    // ... existing code ...

    if (selectedAgent) {
        return (
            <AgentDetail
                agent={selectedAgent}
                onBack={() => dispatch(selectAgent(null))}
            />
        );
    }

    return (
        <>
            <AgentList
                agents={agents}
                isLoading={isLoading}
                onSelectAgent={(id) => dispatch(selectAgent(id))}
            />
            <YourComponent />  {/* <-- ADD HERE if it's a panel-level component */}
        </>
    );
};
```

## Step 8: Handle WebSocket Events (if applicable)

If the server publishes a new WebSocket event for your feature:

### 8a. Add the Event Interface to types.ts

```typescript
export interface YourWebSocketEvent {
    field: string;
}
```

### 8b. Add the WebSocket Action Creator in actions.ts

```typescript
export const websocketYourEvent = (data: YourWebSocketEvent): YourAction => ({
    type: YOUR_ACTION,
    data: {
        field: data.field,
    },
});
```

### 8c. Register the Handler in websocket.ts

```typescript
import {websocketYourEvent} from './actions';
import type {YourWebSocketEvent} from './types';

export function registerWebSocketHandlers(
    registry: PluginRegistry,
    store: Store<GlobalState>,
): void {
    // ... existing handlers ...

    registry.registerWebSocketEventHandler(
        'custom_' + manifest.id + '_your_event_name',
        (msg: {data: YourWebSocketEvent}) => {
            store.dispatch(websocketYourEvent(msg.data) as any);
        },
    );
}
```

The event name format is `custom_{pluginId}_{eventName}` where the server publishes with just `eventName`.

## Step 9: Register in index.tsx (if needed)

If you are registering a new extension point (not just adding to the existing RHS), update `webapp/src/index.tsx`:

```typescript
public async initialize(registry: PluginRegistry, store: Store<GlobalState>) {
    // ... existing registrations ...

    // Register your new extension point
    registry.registerYourThing(...);
}
```

Available registration methods include:
- `registerRightHandSidebarComponent` -- RHS panel
- `registerAppBarComponent` -- App bar icon
- `registerPostDropdownMenuAction` -- Post context menu action
- `registerWebSocketEventHandler` -- WebSocket event handler
- `registerReconnectHandler` -- Reconnection handler
- `registerReducer` -- Redux reducer

## Step 10: Build and Test

```bash
# Build webapp
cd webapp && npm run build

# Run webapp tests
cd webapp && npm test

# Build entire plugin
make dist

# Deploy to local Mattermost
make deploy
```

## Checklist

- [ ] Added types in `webapp/src/types.ts`
- [ ] Added action type constant, interface, and creator in `webapp/src/actions.ts`
- [ ] Updated the PluginAction union type
- [ ] Added reducer case in `webapp/src/reducer.ts`
- [ ] Updated `initialState` if new fields were added
- [ ] Added selectors in `webapp/src/selectors.ts`
- [ ] Created component file(s) with functional component pattern
- [ ] Added CSS using ONLY Mattermost CSS variables in `webapp/src/components/common/styles.css`
- [ ] Wired component into `RHSPanel.tsx` or `index.tsx`
- [ ] Added WebSocket event handling if applicable
- [ ] Added API client method in `webapp/src/client.ts` if applicable
- [ ] Run `cd webapp && npm test` to verify
- [ ] Run `cd webapp && npm run build` to verify the build
