import {
    AGENTS_RECEIVED,
    AGENT_RECEIVED,
    AGENT_STATUS_CHANGED,
    AGENT_CREATED,
    AGENT_REMOVED,
    SELECT_AGENT,
    SET_LOADING,
} from './actions';
import type {PluginAction} from './actions';
import type {Agent, PluginState} from './types';

const initialState: PluginState = {
    agents: {},
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
        const {[action.data.agent_id]: _, ...remaining} = state.agents; // eslint-disable-line @typescript-eslint/no-unused-vars
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
    default:
        return state;
    }
}
