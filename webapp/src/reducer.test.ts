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
import reducer from './reducer';
import type {Agent, PluginState, Workflow} from './types';

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

const makeWorkflow = (overrides: Partial<Workflow> = {}): Workflow => ({
    id: 'wf-1',
    user_id: 'user-1',
    channel_id: 'ch-1',
    root_post_id: 'post-1',
    phase: 'context_review',
    repository: 'org/repo',
    branch: 'main',
    model: 'auto',
    original_prompt: 'fix the bug',
    enriched_context: 'The user reported...',
    approved_context: '',
    planner_agent_id: '',
    retrieved_plan: '',
    approved_plan: '',
    plan_iteration_count: 0,
    implementer_agent_id: '',
    skip_context_review: false,
    skip_plan_loop: false,
    created_at: 1000,
    updated_at: 1000,
    ...overrides,
});

const initialState: PluginState = {
    agents: {},
    workflows: {},
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

    it('handles WORKFLOW_RECEIVED', () => {
        const workflow = makeWorkflow({id: 'wf-1'});
        const state = reducer(initialState, {
            type: WORKFLOW_RECEIVED,
            data: workflow,
        });
        expect(state.workflows['wf-1']).toEqual(workflow);
    });

    it('WORKFLOW_RECEIVED adds to existing workflows', () => {
        const prevState: PluginState = {
            ...initialState,
            workflows: {'wf-1': makeWorkflow({id: 'wf-1'})},
        };
        const state = reducer(prevState, {
            type: WORKFLOW_RECEIVED,
            data: makeWorkflow({id: 'wf-2'}),
        });
        expect(Object.keys(state.workflows)).toHaveLength(2);
    });

    it('handles WORKFLOW_PHASE_CHANGED for existing workflow', () => {
        const prevState: PluginState = {
            ...initialState,
            workflows: {'wf-1': makeWorkflow({id: 'wf-1', phase: 'context_review'})},
        };
        const state = reducer(prevState, {
            type: WORKFLOW_PHASE_CHANGED,
            data: {
                workflow_id: 'wf-1',
                phase: 'planning',
                planner_agent_id: 'agent-p1',
                implementer_agent_id: '',
                plan_iteration_count: 1,
                updated_at: 2000,
            },
        });
        expect(state.workflows['wf-1'].phase).toBe('planning');
        expect(state.workflows['wf-1'].planner_agent_id).toBe('agent-p1');
        expect(state.workflows['wf-1'].plan_iteration_count).toBe(1);
        expect(state.workflows['wf-1'].updated_at).toBe(2000);
    });

    it('WORKFLOW_PHASE_CHANGED ignores unknown workflow', () => {
        const state = reducer(initialState, {
            type: WORKFLOW_PHASE_CHANGED,
            data: {
                workflow_id: 'unknown',
                phase: 'planning',
                planner_agent_id: '',
                implementer_agent_id: '',
                plan_iteration_count: 0,
                updated_at: 2000,
            },
        });
        expect(state).toEqual(initialState);
    });

    it('WORKFLOW_PHASE_CHANGED preserves existing planner_agent_id when new value is empty', () => {
        const prevState: PluginState = {
            ...initialState,
            workflows: {'wf-1': makeWorkflow({id: 'wf-1', phase: 'planning', planner_agent_id: 'existing-agent'})},
        };
        const state = reducer(prevState, {
            type: WORKFLOW_PHASE_CHANGED,
            data: {
                workflow_id: 'wf-1',
                phase: 'plan_review',
                planner_agent_id: '',
                implementer_agent_id: '',
                plan_iteration_count: 1,
                updated_at: 3000,
            },
        });
        expect(state.workflows['wf-1'].planner_agent_id).toBe('existing-agent');
        expect(state.workflows['wf-1'].phase).toBe('plan_review');
    });

    it('WORKFLOW_PHASE_CHANGED sets implementer_agent_id on implementing phase', () => {
        const prevState: PluginState = {
            ...initialState,
            workflows: {'wf-1': makeWorkflow({id: 'wf-1', phase: 'plan_review'})},
        };
        const state = reducer(prevState, {
            type: WORKFLOW_PHASE_CHANGED,
            data: {
                workflow_id: 'wf-1',
                phase: 'implementing',
                planner_agent_id: '',
                implementer_agent_id: 'impl-agent-1',
                plan_iteration_count: 1,
                updated_at: 4000,
            },
        });
        expect(state.workflows['wf-1'].implementer_agent_id).toBe('impl-agent-1');
        expect(state.workflows['wf-1'].phase).toBe('implementing');
    });

    it('WORKFLOW_PHASE_CHANGED propagates phase to agents with matching workflow_id', () => {
        const prevState: PluginState = {
            ...initialState,
            agents: {
                'planner-1': makeAgent({id: 'planner-1', workflow_id: 'wf-1', workflow_phase: 'plan_review'}),
                'other-agent': makeAgent({id: 'other-agent'}),
            },
            workflows: {'wf-1': makeWorkflow({id: 'wf-1', phase: 'plan_review'})},
        };
        const state = reducer(prevState, {
            type: WORKFLOW_PHASE_CHANGED,
            data: {
                workflow_id: 'wf-1',
                phase: 'implementing',
                planner_agent_id: 'planner-1',
                implementer_agent_id: 'impl-1',
                plan_iteration_count: 1,
                updated_at: 5000,
            },
        });
        expect(state.agents['planner-1'].workflow_phase).toBe('implementing');
        expect(state.agents['planner-1'].plan_iteration_count).toBe(1);

        // Unrelated agent should not be touched.
        expect(state.agents['other-agent'].workflow_phase).toBeUndefined();
    });

    it('WORKFLOW_PHASE_CHANGED associates agents by planner/implementer ID even without workflow_id', () => {
        const prevState: PluginState = {
            ...initialState,
            agents: {
                'impl-1': makeAgent({id: 'impl-1'}), // no workflow_id
            },
            workflows: {'wf-1': makeWorkflow({id: 'wf-1', phase: 'plan_review'})},
        };
        const state = reducer(prevState, {
            type: WORKFLOW_PHASE_CHANGED,
            data: {
                workflow_id: 'wf-1',
                phase: 'implementing',
                planner_agent_id: '',
                implementer_agent_id: 'impl-1',
                plan_iteration_count: 1,
                updated_at: 6000,
            },
        });
        expect(state.agents['impl-1'].workflow_id).toBe('wf-1');
        expect(state.agents['impl-1'].workflow_phase).toBe('implementing');
    });

    it('WORKFLOW_PHASE_CHANGED does not mutate state when no agents match', () => {
        const prevState: PluginState = {
            ...initialState,
            agents: {
                a1: makeAgent({id: 'a1', workflow_id: 'wf-other'}),
            },
            workflows: {'wf-1': makeWorkflow({id: 'wf-1', phase: 'planning'})},
        };
        const state = reducer(prevState, {
            type: WORKFLOW_PHASE_CHANGED,
            data: {
                workflow_id: 'wf-1',
                phase: 'plan_review',
                planner_agent_id: 'nonexistent',
                implementer_agent_id: '',
                plan_iteration_count: 1,
                updated_at: 7000,
            },
        });

        // agents ref should be preserved (no unnecessary copy).
        expect(state.agents).toBe(prevState.agents);
    });
});
