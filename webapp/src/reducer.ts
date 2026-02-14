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
} from './actions';
import type {PluginAction} from './actions';
import type {Agent, PluginState} from './types';

const initialState: PluginState = {
    agents: {},
    workflows: {},
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
        const existing = state.workflows[action.data.workflow_id];
        if (!existing) {
            return state;
        }
        return {
            ...state,
            workflows: {
                ...state.workflows,
                [action.data.workflow_id]: {
                    ...existing,
                    phase: action.data.phase,
                    planner_agent_id: action.data.planner_agent_id || existing.planner_agent_id,
                    implementer_agent_id: action.data.implementer_agent_id || existing.implementer_agent_id,
                    plan_iteration_count: action.data.plan_iteration_count,
                    updated_at: action.data.updated_at,
                },
            },
        };
    }
    default:
        return state;
    }
}
