import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import type {PluginRegistry} from 'types/mattermost-webapp';

import {fetchAgents, selectAgent, addFollowup, cancelAgent} from './actions';
import RHSPanel from './components/rhs/RHSPanel';
import manifest from './manifest';
import reducer from './reducer';
import {registerWebSocketHandlers} from './websocket';

export default class Plugin {
    private rhsToggleAction: object | null = null;
    private rhsShowAction: object | null = null;

    public async initialize(registry: PluginRegistry, store: Store<GlobalState>) {
        // 1. Register Redux reducer
        registry.registerReducer(reducer);

        // 2. Register RHS component
        const {toggleRHSPlugin, showRHSPlugin} = registry.registerRightHandSidebarComponent(
            RHSPanel,
            'Cursor Agents',
        );
        this.rhsToggleAction = toggleRHSPlugin;
        this.rhsShowAction = showRHSPlugin;

        // 3. Register App Bar icon
        registry.registerAppBarComponent(
            `/plugins/${manifest.id}/public/app-bar-icon.png`,
            () => store.dispatch(toggleRHSPlugin as any),
            'Cursor Agents',
            null,
        );

        // 4. Register post dropdown menu actions
        this.registerPostActions(registry, store);

        // 5. Register WebSocket event handlers
        registerWebSocketHandlers(registry, store);

        // 6. Register reconnect handler to refetch agents on reconnect
        registry.registerReconnectHandler(() => {
            store.dispatch(fetchAgents() as any);
        });

        // 7. Initial fetch of agents
        store.dispatch(fetchAgents() as any);
    }

    private registerPostActions(registry: PluginRegistry, store: Store<GlobalState>) {
        // "Add Follow-up" action -- only on posts from the Cursor bot that have an agent
        registry.registerPostDropdownMenuAction(
            'Add Follow-up to Cursor Agent',
            (postId: string) => {
                const message = window.prompt('Enter follow-up instructions:'); // eslint-disable-line no-alert
                if (message && message.trim()) {
                    const state = store.getState() as any;
                    const post = state.entities?.posts?.posts?.[postId];
                    const agentId = post?.props?.cursor_agent_id;
                    if (agentId) {
                        store.dispatch(addFollowup(agentId, message.trim()) as any);
                    }
                }
            },
            (postId: string) => {
                const state = store.getState() as any;
                const post = state.entities?.posts?.posts?.[postId];
                if (!post?.props?.cursor_agent_id) {
                    return false;
                }
                const status = post.props.cursor_agent_status;
                return status === 'RUNNING';
            },
        );

        // "Cancel Cursor Agent" action
        registry.registerPostDropdownMenuAction(
            'Cancel Cursor Agent',
            (postId: string) => {
                if (!window.confirm('Are you sure you want to cancel this Cursor agent?')) { // eslint-disable-line no-alert
                    return;
                }
                const state = store.getState() as any;
                const post = state.entities?.posts?.posts?.[postId];
                const agentId = post?.props?.cursor_agent_id;
                if (agentId) {
                    store.dispatch(cancelAgent(agentId) as any);
                }
            },
            (postId: string) => {
                const state = store.getState() as any;
                const post = state.entities?.posts?.posts?.[postId];
                if (!post?.props?.cursor_agent_id) {
                    return false;
                }
                const status = post.props.cursor_agent_status;
                return status === 'RUNNING' || status === 'CREATING';
            },
        );

        // "View Agent Details" action
        registry.registerPostDropdownMenuAction(
            'View Agent Details',
            (postId: string) => {
                const state = store.getState() as any;
                const post = state.entities?.posts?.posts?.[postId];
                const agentId = post?.props?.cursor_agent_id;
                if (agentId) {
                    store.dispatch(selectAgent(agentId));
                    if (this.rhsShowAction) {
                        store.dispatch(this.rhsShowAction as any);
                    }
                }
            },
            (postId: string) => {
                const state = store.getState() as any;
                const post = state.entities?.posts?.posts?.[postId];
                return Boolean(post?.props?.cursor_agent_id);
            },
        );
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
