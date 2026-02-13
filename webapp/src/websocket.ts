import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import manifest from './manifest';
import {websocketAgentStatusChange, websocketAgentCreated} from './actions';
import type {AgentStatusChangeEvent, AgentCreatedEvent} from './types';
import type {PluginRegistry} from 'types/mattermost-webapp';

export function registerWebSocketHandlers(
    registry: PluginRegistry,
    store: Store<GlobalState>,
): void {
    registry.registerWebSocketEventHandler(
        'custom_' + manifest.id + '_agent_status_change',
        (msg: {data: AgentStatusChangeEvent}) => {
            store.dispatch(websocketAgentStatusChange(msg.data) as any);
        },
    );

    registry.registerWebSocketEventHandler(
        'custom_' + manifest.id + '_agent_created',
        (msg: {data: AgentCreatedEvent}) => {
            store.dispatch(websocketAgentCreated(msg.data) as any);
        },
    );
}
