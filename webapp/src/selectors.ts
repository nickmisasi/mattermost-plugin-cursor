import type {GlobalState} from '@mattermost/types/store';

import manifest from './manifest';
import type {Agent, PluginState} from './types';

const getPluginState = (state: GlobalState): PluginState => {
    return (state as any)['plugins-' + manifest.id] || {agents: {}, selectedAgentId: null, isLoading: false};
};

export const getAgents = (state: GlobalState): Record<string, Agent> => {
    return getPluginState(state).agents;
};

export const getAgentsList = (state: GlobalState): Agent[] => {
    return Object.values(getAgents(state));
};

export const getActiveAgents = (state: GlobalState): Agent[] => {
    return getAgentsList(state).filter(
        (a) => a.status === 'CREATING' || a.status === 'RUNNING',
    );
};

export const getSelectedAgentId = (state: GlobalState): string | null => {
    return getPluginState(state).selectedAgentId;
};

export const getSelectedAgent = (state: GlobalState): Agent | null => {
    const id = getSelectedAgentId(state);
    if (!id) {
        return null;
    }
    return getAgents(state)[id] || null;
};

export const getIsLoading = (state: GlobalState): boolean => {
    return getPluginState(state).isLoading;
};

export const getAgentByPostId = (state: GlobalState, postId: string): Agent | undefined => {
    const agents = getAgents(state);
    return Object.values(agents).find((a) => a.post_id === postId);
};
