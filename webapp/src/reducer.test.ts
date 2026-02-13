import {
    AGENTS_RECEIVED,
    AGENT_RECEIVED,
    AGENT_STATUS_CHANGED,
    AGENT_CREATED,
    AGENT_REMOVED,
    SELECT_AGENT,
    SET_LOADING,
} from './actions';
import reducer from './reducer';
import type {Agent, PluginState} from './types';

const makeAgent = (overrides: Partial<Agent> = {}): Agent => ({
    id: 'agent-1',
    status: 'RUNNING',
    repository: 'org/repo',
    branch: 'main',
    prompt: 'fix the bug',
    pr_url: '',
    cursor_url: 'https://cursor.com/agents/agent-1',
    channel_id: 'ch-1',
    post_id: 'post-1',
    root_post_id: '',
    summary: '',
    model: 'auto',
    created_at: 1000,
    updated_at: 1000,
    ...overrides,
});

const initialState: PluginState = {
    agents: {},
    selectedAgentId: null,
    isLoading: false,
};

describe('reducer', () => {
    it('returns initial state for unknown action', () => {
        const state = reducer(undefined, {type: 'UNKNOWN'} as any);
        expect(state).toEqual(initialState);
    });

    it('handles AGENTS_RECEIVED', () => {
        const agents = [makeAgent({id: 'a1'}), makeAgent({id: 'a2'})];
        const state = reducer(initialState, {
            type: AGENTS_RECEIVED,
            data: agents,
        });
        expect(Object.keys(state.agents)).toHaveLength(2);
        expect(state.agents.a1.id).toBe('a1');
        expect(state.agents.a2.id).toBe('a2');
    });

    it('AGENTS_RECEIVED replaces existing agents', () => {
        const prevState: PluginState = {
            ...initialState,
            agents: {old: makeAgent({id: 'old'})},
        };
        const state = reducer(prevState, {
            type: AGENTS_RECEIVED,
            data: [makeAgent({id: 'new'})],
        });
        expect(state.agents.old).toBeUndefined();
        expect(state.agents.new).toBeDefined();
    });

    it('handles AGENT_RECEIVED', () => {
        const agent = makeAgent({id: 'a1'});
        const state = reducer(initialState, {
            type: AGENT_RECEIVED,
            data: agent,
        });
        expect(state.agents.a1).toEqual(agent);
    });

    it('handles AGENT_STATUS_CHANGED for existing agent', () => {
        const prevState: PluginState = {
            ...initialState,
            agents: {a1: makeAgent({id: 'a1', status: 'RUNNING'})},
        };
        const state = reducer(prevState, {
            type: AGENT_STATUS_CHANGED,
            data: {
                agent_id: 'a1',
                status: 'FINISHED',
                pr_url: 'https://github.com/pr/1',
                summary: 'Done',
                updated_at: 2000,
            },
        });
        expect(state.agents.a1.status).toBe('FINISHED');
        expect(state.agents.a1.pr_url).toBe('https://github.com/pr/1');
        expect(state.agents.a1.summary).toBe('Done');
        expect(state.agents.a1.updated_at).toBe(2000);
    });

    it('AGENT_STATUS_CHANGED ignores unknown agent', () => {
        const state = reducer(initialState, {
            type: AGENT_STATUS_CHANGED,
            data: {
                agent_id: 'unknown',
                status: 'FINISHED',
                pr_url: '',
                summary: '',
                updated_at: 2000,
            },
        });
        expect(state).toEqual(initialState);
    });

    it('handles AGENT_CREATED', () => {
        const agent = makeAgent({id: 'new-agent', status: 'CREATING'});
        const state = reducer(initialState, {
            type: AGENT_CREATED,
            data: agent,
        });
        expect(state.agents['new-agent']).toBeDefined();
        expect(state.agents['new-agent'].status).toBe('CREATING');
    });

    it('handles AGENT_REMOVED', () => {
        const prevState: PluginState = {
            ...initialState,
            agents: {a1: makeAgent({id: 'a1'})},
            selectedAgentId: 'a1',
        };
        const state = reducer(prevState, {
            type: AGENT_REMOVED,
            data: {agent_id: 'a1'},
        });
        expect(state.agents.a1).toBeUndefined();
        expect(state.selectedAgentId).toBeNull();
    });

    it('AGENT_REMOVED does not clear selectedAgentId if different', () => {
        const prevState: PluginState = {
            ...initialState,
            agents: {
                a1: makeAgent({id: 'a1'}),
                a2: makeAgent({id: 'a2'}),
            },
            selectedAgentId: 'a2',
        };
        const state = reducer(prevState, {
            type: AGENT_REMOVED,
            data: {agent_id: 'a1'},
        });
        expect(state.selectedAgentId).toBe('a2');
    });

    it('handles SELECT_AGENT', () => {
        const state = reducer(initialState, {
            type: SELECT_AGENT,
            data: {agent_id: 'a1'},
        });
        expect(state.selectedAgentId).toBe('a1');
    });

    it('handles SELECT_AGENT with null', () => {
        const prevState: PluginState = {
            ...initialState,
            selectedAgentId: 'a1',
        };
        const state = reducer(prevState, {
            type: SELECT_AGENT,
            data: {agent_id: null},
        });
        expect(state.selectedAgentId).toBeNull();
    });

    it('handles SET_LOADING', () => {
        const state = reducer(initialState, {
            type: SET_LOADING,
            data: {isLoading: true},
        });
        expect(state.isLoading).toBe(true);

        const state2 = reducer(state, {
            type: SET_LOADING,
            data: {isLoading: false},
        });
        expect(state2.isLoading).toBe(false);
    });
});
