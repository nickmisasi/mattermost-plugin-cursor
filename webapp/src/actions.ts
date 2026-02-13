import Client from './client';
import type {Agent, AgentStatus, AgentStatusChangeEvent, AgentCreatedEvent} from './types';

// Action type constants
export const AGENTS_RECEIVED = 'com.mattermost.plugin-cursor/AGENTS_RECEIVED';
export const AGENT_RECEIVED = 'com.mattermost.plugin-cursor/AGENT_RECEIVED';
export const AGENT_STATUS_CHANGED = 'com.mattermost.plugin-cursor/AGENT_STATUS_CHANGED';
export const AGENT_CREATED = 'com.mattermost.plugin-cursor/AGENT_CREATED';
export const AGENT_REMOVED = 'com.mattermost.plugin-cursor/AGENT_REMOVED';
export const SELECT_AGENT = 'com.mattermost.plugin-cursor/SELECT_AGENT';
export const SET_LOADING = 'com.mattermost.plugin-cursor/SET_LOADING';

// Action interfaces
interface AgentsReceivedAction {
    type: typeof AGENTS_RECEIVED;
    data: Agent[];
}

interface AgentReceivedAction {
    type: typeof AGENT_RECEIVED;
    data: Agent;
}

interface AgentStatusChangedAction {
    type: typeof AGENT_STATUS_CHANGED;
    data: {
        agent_id: string;
        status: AgentStatus;
        pr_url: string;
        summary: string;
        updated_at: number;
    };
}

interface AgentCreatedAction {
    type: typeof AGENT_CREATED;
    data: Agent;
}

interface AgentRemovedAction {
    type: typeof AGENT_REMOVED;
    data: {agent_id: string};
}

interface SelectAgentAction {
    type: typeof SELECT_AGENT;
    data: {agent_id: string | null};
}

interface SetLoadingAction {
    type: typeof SET_LOADING;
    data: {isLoading: boolean};
}

export type PluginAction =
    | AgentsReceivedAction
    | AgentReceivedAction
    | AgentStatusChangedAction
    | AgentCreatedAction
    | AgentRemovedAction
    | SelectAgentAction
    | SetLoadingAction;

// --- Sync action creators ---

export const selectAgent = (agentId: string | null): SelectAgentAction => ({
    type: SELECT_AGENT,
    data: {agent_id: agentId},
});

// --- Async action creators (thunks) ---

export function fetchAgents() {
    return async (dispatch: (action: PluginAction) => void) => {
        dispatch({type: SET_LOADING, data: {isLoading: true}});
        try {
            const response = await Client.getAgents();
            dispatch({type: AGENTS_RECEIVED, data: response.agents});
        } catch (error) {
            console.error('Failed to fetch agents:', error); // eslint-disable-line no-console
        } finally {
            dispatch({type: SET_LOADING, data: {isLoading: false}});
        }
    };
}

export function fetchAgent(agentId: string) {
    return async (dispatch: (action: PluginAction) => void) => {
        try {
            const agent = await Client.getAgent(agentId);
            dispatch({type: AGENT_RECEIVED, data: agent});
        } catch (error) {
            console.error('Failed to fetch agent:', error); // eslint-disable-line no-console
        }
    };
}

export function addFollowup(agentId: string, message: string) {
    return async () => {
        try {
            await Client.addFollowup(agentId, message);
        } catch (error) {
            console.error('Failed to add followup:', error); // eslint-disable-line no-console
        }
    };
}

export function cancelAgent(agentId: string) {
    return async () => {
        try {
            await Client.cancelAgent(agentId);
        } catch (error) {
            console.error('Failed to cancel agent:', error); // eslint-disable-line no-console
        }
    };
}

// --- WebSocket event handlers ---

export const websocketAgentStatusChange = (data: AgentStatusChangeEvent): AgentStatusChangedAction => ({
    type: AGENT_STATUS_CHANGED,
    data: {
        agent_id: data.agent_id,
        status: data.status,
        pr_url: data.pr_url,
        summary: data.summary,
        updated_at: parseInt(data.updated_at, 10),
    },
});

export const websocketAgentCreated = (data: AgentCreatedEvent): AgentCreatedAction => ({
    type: AGENT_CREATED,
    data: {
        id: data.agent_id,
        status: data.status,
        repository: data.repository,
        branch: data.branch,
        prompt: data.prompt,
        pr_url: '',
        cursor_url: data.cursor_url,
        channel_id: data.channel_id,
        post_id: data.post_id,
        root_post_id: '',
        summary: '',
        model: '',
        created_at: parseInt(data.created_at, 10),
        updated_at: parseInt(data.created_at, 10),
    },
});
