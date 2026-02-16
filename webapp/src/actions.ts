import {Client4} from 'mattermost-redux/client';

import Client from './client';
import type {Agent, AgentStatus, AgentStatusChangeEvent, AgentCreatedEvent, ReviewLoop, ReviewLoopPhase, ReviewLoopChangeEvent, Workflow, WorkflowPhase, WorkflowPhaseChangeEvent} from './types';

// Action type constants
export const AGENTS_RECEIVED = 'com.mattermost.plugin-cursor/AGENTS_RECEIVED';
export const AGENT_RECEIVED = 'com.mattermost.plugin-cursor/AGENT_RECEIVED';
export const AGENT_STATUS_CHANGED = 'com.mattermost.plugin-cursor/AGENT_STATUS_CHANGED';
export const AGENT_CREATED = 'com.mattermost.plugin-cursor/AGENT_CREATED';
export const AGENT_REMOVED = 'com.mattermost.plugin-cursor/AGENT_REMOVED';
export const SELECT_AGENT = 'com.mattermost.plugin-cursor/SELECT_AGENT';
export const SET_LOADING = 'com.mattermost.plugin-cursor/SET_LOADING';
export const WORKFLOW_RECEIVED = 'com.mattermost.plugin-cursor/WORKFLOW_RECEIVED';
export const WORKFLOW_PHASE_CHANGED = 'com.mattermost.plugin-cursor/WORKFLOW_PHASE_CHANGED';
export const REVIEW_LOOP_CHANGED = 'com.mattermost.plugin-cursor/REVIEW_LOOP_CHANGED';
export const REVIEW_LOOP_RECEIVED = 'com.mattermost.plugin-cursor/REVIEW_LOOP_RECEIVED';

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

interface WorkflowReceivedAction {
    type: typeof WORKFLOW_RECEIVED;
    data: Workflow;
}

interface WorkflowPhaseChangedAction {
    type: typeof WORKFLOW_PHASE_CHANGED;
    data: {
        workflow_id: string;
        phase: WorkflowPhase;
        planner_agent_id: string;
        implementer_agent_id: string;
        plan_iteration_count: number;
        updated_at: number;
    };
}

interface ReviewLoopChangedAction {
    type: typeof REVIEW_LOOP_CHANGED;
    data: {
        review_loop_id: string;
        agent_record_id: string;
        phase: ReviewLoopPhase;
        iteration: number;
        pr_url: string;
        updated_at: number;
    };
}

interface ReviewLoopReceivedAction {
    type: typeof REVIEW_LOOP_RECEIVED;
    data: ReviewLoop;
}

export type PluginAction =
    | AgentsReceivedAction
    | AgentReceivedAction
    | AgentStatusChangedAction
    | AgentCreatedAction
    | AgentRemovedAction
    | SelectAgentAction
    | SetLoadingAction
    | WorkflowReceivedAction
    | WorkflowPhaseChangedAction
    | ReviewLoopChangedAction
    | ReviewLoopReceivedAction;

// --- Sync action creators ---

export const selectAgent = (agentId: string | null): SelectAgentAction => ({
    type: SELECT_AGENT,
    data: {agent_id: agentId},
});

// --- Async action creators (thunks) ---

export function fetchAgents(archived?: boolean) {
    return async (dispatch: (action: PluginAction) => void) => {
        dispatch({type: SET_LOADING, data: {isLoading: true}});
        try {
            const response = await Client.getAgents(archived);
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

export function archiveAgent(agentId: string) {
    return async (dispatch: (action: PluginAction) => void) => {
        try {
            await Client.archiveAgent(agentId);
            dispatch({type: AGENT_REMOVED, data: {agent_id: agentId}});
        } catch (error) {
            console.error('Failed to archive agent:', error); // eslint-disable-line no-console
        }
    };
}

export function unarchiveAgent(agentId: string) {
    return async (dispatch: (action: PluginAction) => void) => {
        try {
            await Client.unarchiveAgent(agentId);
            dispatch({type: AGENT_REMOVED, data: {agent_id: agentId}});
        } catch (error) {
            console.error('Failed to unarchive agent:', error); // eslint-disable-line no-console
        }
    };
}

export function openSettings() {
    return async (_dispatch: any, getState: any) => { // eslint-disable-line @typescript-eslint/no-explicit-any
        const state = getState();
        const channelId = state.entities?.channels?.currentChannelId;
        const teamId = state.entities?.teams?.currentTeamId;
        if (!channelId) {
            return;
        }
        try {
            const result = await Client4.executeCommand('/cursor settings', {channel_id: channelId, team_id: teamId || '', root_id: ''});
            if (result?.trigger_id) {
                _dispatch({type: 'RECEIVED_DIALOG_TRIGGER_ID', data: result.trigger_id});
            }
        } catch (error) {
            console.error('Failed to open settings dialog:', error); // eslint-disable-line no-console
        }
    };
}

export function fetchWorkflow(workflowId: string) {
    return async (dispatch: (action: PluginAction) => void) => {
        try {
            const workflow = await Client.getWorkflow(workflowId);
            dispatch({type: WORKFLOW_RECEIVED, data: workflow});
        } catch (error) {
            console.error('Failed to fetch workflow:', error); // eslint-disable-line no-console
        }
    };
}

export function fetchReviewLoop(reviewLoopId: string) {
    return async (dispatch: (action: PluginAction) => void) => {
        try {
            const reviewLoop = await Client.getReviewLoop(reviewLoopId);
            dispatch({type: REVIEW_LOOP_RECEIVED, data: reviewLoop});
        } catch (error) {
            console.error('Failed to fetch review loop:', error); // eslint-disable-line no-console
        }
    };
}

// --- WebSocket event handlers ---

const parseTimestamp = (value: string): number => {
    const parsed = parseInt(value, 10);
    return Number.isNaN(parsed) ? Date.now() : parsed;
};

export const websocketAgentStatusChange = (data: AgentStatusChangeEvent): AgentStatusChangedAction => ({
    type: AGENT_STATUS_CHANGED,
    data: {
        agent_id: data.agent_id,
        status: data.status,
        pr_url: data.pr_url,
        summary: data.summary,
        updated_at: parseTimestamp(data.updated_at),
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
        description: data.description || '',
        pr_url: '',
        cursor_url: data.cursor_url,
        channel_id: data.channel_id,
        post_id: data.post_id,
        root_post_id: '',
        summary: '',
        model: '',
        created_at: parseTimestamp(data.created_at),
        updated_at: parseTimestamp(data.created_at),
    },
});

export const websocketWorkflowPhaseChange = (data: WorkflowPhaseChangeEvent): WorkflowPhaseChangedAction => ({
    type: WORKFLOW_PHASE_CHANGED,
    data: {
        workflow_id: data.workflow_id,
        phase: data.phase,
        planner_agent_id: data.planner_agent_id,
        implementer_agent_id: data.implementer_agent_id,
        plan_iteration_count: parseInt(data.plan_iteration_count, 10) || 0,
        updated_at: parseTimestamp(data.updated_at),
    },
});

export const websocketReviewLoopChanged = (data: ReviewLoopChangeEvent): ReviewLoopChangedAction => ({
    type: REVIEW_LOOP_CHANGED,
    data: {
        review_loop_id: data.review_loop_id,
        agent_record_id: data.agent_record_id,
        phase: data.phase,
        iteration: parseInt(data.iteration, 10) || 0,
        pr_url: data.pr_url,
        updated_at: parseTimestamp(data.updated_at),
    },
});
