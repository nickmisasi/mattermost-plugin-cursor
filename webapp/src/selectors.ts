import type {GlobalState} from '@mattermost/types/store';

import manifest from './manifest';
import type {Agent, PluginState, Workflow} from './types';

const getPluginState = (state: GlobalState): PluginState => {
    return (state as any)['plugins-' + manifest.id] || {agents: {}, workflows: {}, selectedAgentId: null, isLoading: false};
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

export const getWorkflows = (state: GlobalState): Record<string, Workflow> => {
    return getPluginState(state).workflows;
};

export const getWorkflowForAgent = (state: GlobalState, agentId: string): Workflow | undefined => {
    const workflows = getWorkflows(state);
    return Object.values(workflows).find(
        (w) => w.planner_agent_id === agentId || w.implementer_agent_id === agentId,
    );
};

export const getWorkflow = (state: GlobalState, workflowId: string): Workflow | undefined => {
    return getWorkflows(state)[workflowId];
};
