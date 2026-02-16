import {
    AGENTS_RECEIVED,
    AGENT_RECEIVED,
    AGENT_STATUS_CHANGED,
    AGENT_CREATED,
    AGENT_REMOVED,
    SELECT_AGENT,
    SET_LOADING,
    WORKFLOW_RECEIVED,
    WORKFLOW_PHASE_CHANGED,
    REVIEW_LOOP_CHANGED,
    REVIEW_LOOP_RECEIVED,
} from './actions';
import type {PluginAction} from './actions';
import type {Agent, PluginState} from './types';

const initialState: PluginState = {
    agents: {},
    workflows: {},
    reviewLoops: {},
    selectedAgentId: null,
    isLoading: false,
};

export default function reducer(state: PluginState = initialState, action: PluginAction): PluginState {
    switch (action.type) {
    case AGENTS_RECEIVED: {
        const agents: Record<string, Agent> = {};
        for (const agent of action.data) {
            agents[agent.id] = agent;
        }
        return {...state, agents};
    }
    case AGENT_RECEIVED:
        return {
            ...state,
            agents: {...state.agents, [action.data.id]: action.data},
        };
    case AGENT_STATUS_CHANGED: {
        const existing = state.agents[action.data.agent_id];
        if (!existing) {
            return state;
        }
        return {
            ...state,
            agents: {
                ...state.agents,
                [action.data.agent_id]: {
                    ...existing,
                    status: action.data.status,
                    pr_url: action.data.pr_url || existing.pr_url,
                    summary: action.data.summary || existing.summary,
                    updated_at: action.data.updated_at,
                },
            },
        };
    }
    case AGENT_CREATED:
        return {
            ...state,
            agents: {...state.agents, [action.data.id]: action.data},
        };
    case AGENT_REMOVED: {
        const {[action.data.agent_id]: removed, ...remaining} = state.agents; // eslint-disable-line @typescript-eslint/no-unused-vars
        return {
            ...state,
            agents: remaining,
            selectedAgentId: state.selectedAgentId === action.data.agent_id ? null : state.selectedAgentId,
        };
    }
    case SELECT_AGENT:
        return {...state, selectedAgentId: action.data.agent_id};
    case SET_LOADING:
        return {...state, isLoading: action.data.isLoading};
    case WORKFLOW_RECEIVED:
        return {
            ...state,
            workflows: {...state.workflows, [action.data.id]: action.data},
        };
    case WORKFLOW_PHASE_CHANGED: {
        const existingWf = state.workflows[action.data.workflow_id];

        // Update the workflow object if it exists in state.
        const updatedWorkflows = existingWf ? {
            ...state.workflows,
            [action.data.workflow_id]: {
                ...existingWf,
                phase: action.data.phase,
                planner_agent_id: action.data.planner_agent_id || existingWf.planner_agent_id,
                implementer_agent_id: action.data.implementer_agent_id || existingWf.implementer_agent_id,
                plan_iteration_count: action.data.plan_iteration_count,
                updated_at: action.data.updated_at,
            },
        } : state.workflows;

        // Also propagate the phase to any agents that belong to this workflow,
        // so the AgentCard PhaseBadge updates without a full refetch.
        // Match by workflow_id OR by the planner/implementer agent IDs from
        // the event (handles agents that arrived via agent_created without workflow_id).
        const {planner_agent_id: plannerID, implementer_agent_id: implID, workflow_id: wfID} = action.data;
        const updatedAgents = {...state.agents};
        let agentsChanged = false;
        for (const [id, agent] of Object.entries(updatedAgents)) {
            if (agent.workflow_id === wfID || id === plannerID || id === implID) {
                updatedAgents[id] = {
                    ...agent,
                    workflow_id: wfID,
                    workflow_phase: action.data.phase,
                    plan_iteration_count: action.data.plan_iteration_count,
                    updated_at: action.data.updated_at,
                };
                agentsChanged = true;
            }
        }

        return {
            ...state,
            workflows: updatedWorkflows,
            agents: agentsChanged ? updatedAgents : state.agents,
        };
    }
    case REVIEW_LOOP_RECEIVED:
        return {
            ...state,
            reviewLoops: {...state.reviewLoops, [action.data.id]: action.data},
        };
    case REVIEW_LOOP_CHANGED: {
        const existingRL = state.reviewLoops[action.data.review_loop_id];

        // Update the review loop object if it exists in state.
        const updatedReviewLoops = existingRL ? {
            ...state.reviewLoops,
            [action.data.review_loop_id]: {
                ...existingRL,
                phase: action.data.phase,
                iteration: action.data.iteration,
                pr_url: action.data.pr_url || existingRL.pr_url,
                updated_at: action.data.updated_at,
            },
        } : state.reviewLoops;

        // Propagate review loop phase to the associated agent so AgentCard
        // updates without a full refetch.
        const rlAgentID = action.data.agent_record_id;
        const rlAgent = state.agents[rlAgentID];
        const updatedRLAgents = rlAgent ? {
            ...state.agents,
            [rlAgentID]: {
                ...rlAgent,
                review_loop_id: action.data.review_loop_id,
                review_loop_phase: action.data.phase,
                review_loop_iteration: action.data.iteration,
                updated_at: action.data.updated_at,
            },
        } : state.agents;

        return {
            ...state,
            reviewLoops: updatedReviewLoops,
            agents: rlAgent ? updatedRLAgents : state.agents,
        };
    }
    default:
        return state;
    }
}
