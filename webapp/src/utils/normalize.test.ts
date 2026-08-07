import {
    normalizeAgent,
    normalizeAgentList,
    normalizeCreateAgentResponse,
    normalizeMessages,
    normalizeModels,
    normalizeRepositories,
    normalizeRun,
    repoDisplayName,
} from './normalize';

describe('repoDisplayName', () => {
    it('strips the scheme and the github.com prefix', () => {
        expect(repoDisplayName('https://github.com/mattermost/factory')).toBe('mattermost/factory');
        expect(repoDisplayName('github.com/acme/bifrost')).toBe('acme/bifrost');
        expect(repoDisplayName('git@github.com:acme/bifrost.git')).toBe('acme/bifrost');
    });

    it('falls back to a placeholder for empty input', () => {
        expect(repoDisplayName('')).toBe('No repository');
    });
});

describe('normalizeAgent', () => {
    it('reads an enriched list item', () => {
        const agent = normalizeAgent({
            id: 'bc-1',
            name: 'Add README',
            status: 'ACTIVE',
            env: {type: 'cloud'},
            url: 'https://cursor.com/agents/bc-1',
            createdAt: '2024-05-01T10:00:00.000Z',
            updatedAt: '2024-05-01T11:00:00.000Z',
            latestRunId: 'run-1',
            repos: [{url: 'https://github.com/acme/bifrost', startingRef: 'main'}],
            branch: 'cursor/add-readme-a1b2',
            prUrl: 'https://github.com/acme/bifrost/pull/1',
            runStatus: 'RUNNING',
        });

        expect(agent).toEqual({
            id: 'bc-1',
            name: 'Add README',
            repositoryUrl: 'https://github.com/acme/bifrost',
            repository: 'acme/bifrost',
            startingRef: 'main',
            branch: 'cursor/add-readme-a1b2',
            prUrl: 'https://github.com/acme/bifrost/pull/1',
            webUrl: 'https://cursor.com/agents/bc-1',
            envType: 'cloud',
            createdAt: Date.parse('2024-05-01T10:00:00.000Z'),
            updatedAt: Date.parse('2024-05-01T11:00:00.000Z'),
            activityAt: Date.parse('2024-05-01T11:00:00.000Z'),
            archived: false,
            runStatus: 'RUNNING',
            latestRunId: 'run-1',
        });
    });

    it('reads an unenriched GET /agents/{id} payload', () => {
        const agent = normalizeAgent({
            id: 'bc-2',
            name: 'Fix flake',
            status: 'ACTIVE',
            env: {type: 'cloud'},
            url: 'https://cursor.com/agents/bc-2',
            latestRunId: 'run-2',
            repos: [{url: 'https://github.com/acme/bifrost', startingRef: 'main'}],
            workOnCurrentBranch: false,
            autoCreatePR: true,
        });

        expect(agent).toMatchObject({
            repository: 'acme/bifrost',
            startingRef: 'main',
            branch: '',
            prUrl: '',
            archived: false,
            runStatus: undefined,
        });
    });

    it('treats the ARCHIVED lifecycle status as archived, not as a run status', () => {
        expect(normalizeAgent({id: 'a', status: 'ARCHIVED'})).toMatchObject({archived: true, runStatus: undefined});
        expect(normalizeAgent({id: 'b', status: 'ACTIVE'})).toMatchObject({archived: false, runStatus: undefined});
    });

    it('accepts every documented run status and ignores unknown ones', () => {
        const statuses = ['QUEUED', 'RUNNING', 'FINISHED', 'ERROR', 'CANCELLED', 'EXPIRED'];
        for (const runStatus of statuses) {
            expect(normalizeAgent({id: 'a', runStatus})?.runStatus).toBe(runStatus);
        }
        expect(normalizeAgent({id: 'a', runStatus: 'SOMETHING_NEW'})?.runStatus).toBeUndefined();
        expect(normalizeAgent({id: 'a', runStatus: 'running'})?.runStatus).toBe('RUNNING');
    });

    it('tolerates missing and malformed fields', () => {
        expect(normalizeAgent({id: 'bc-3'})).toMatchObject({
            name: 'Untitled agent',
            repository: 'No repository',
            branch: '',
            envType: '',
            runStatus: undefined,
            activityAt: 0,
        });
        expect(normalizeAgent({id: 'bc-4', repos: 'nope', status: 42, env: null})).not.toBeNull();
    });

    it('rejects payloads without an id', () => {
        expect(normalizeAgent({name: 'no id'})).toBeNull();
        expect(normalizeAgent(null)).toBeNull();
        expect(normalizeAgent('string')).toBeNull();
    });
});

describe('normalizeRun', () => {
    it('reads a terminal run including its branch and PR', () => {
        expect(normalizeRun({
            id: 'run-1',
            agentId: 'bc-1',
            status: 'FINISHED',
            createdAt: '2024-05-01T10:00:00.000Z',
            updatedAt: '2024-05-01T10:02:00.000Z',
            durationMs: 12357,
            result: 'Added README.md.',
            git: {branches: [{repoUrl: 'github.com/acme/bifrost', branch: 'cursor/add-readme', prUrl: 'https://github.com/acme/bifrost/pull/1'}]},
        })).toEqual({
            id: 'run-1',
            status: 'FINISHED',
            durationMs: 12357,
            result: 'Added README.md.',
            branch: 'cursor/add-readme',
            prUrl: 'https://github.com/acme/bifrost/pull/1',
        });
    });

    it('leaves the terminal-only fields empty on an in-flight run', () => {
        expect(normalizeRun({id: 'run-2', agentId: 'bc-1', status: 'RUNNING'})).toEqual({
            id: 'run-2',
            status: 'RUNNING',
            durationMs: 0,
            result: '',
            branch: '',
            prUrl: '',
        });
    });

    it('skips git entries that carry neither a branch nor a PR', () => {
        const run = normalizeRun({
            id: 'run-3',
            git: {branches: [{repoUrl: 'github.com/acme/bifrost'}, {repoUrl: 'github.com/acme/bifrost', branch: 'cursor/second'}]},
        });

        expect(run?.branch).toBe('cursor/second');
    });

    it('rejects payloads without an id', () => {
        expect(normalizeRun({status: 'RUNNING'})).toBeNull();
    });
});

describe('normalizeAgentList', () => {
    it('reads the items envelope and the pagination cursor', () => {
        const page = normalizeAgentList({items: [{id: 'a'}, {id: 'b'}], nextCursor: 'bc-9'});

        expect(page.agents.map((agent) => agent.id)).toEqual(['a', 'b']);
        expect(page.nextCursor).toBe('bc-9');
    });

    it('reports no cursor when nextCursor is omitted', () => {
        expect(normalizeAgentList({items: [{id: 'a'}]}).nextCursor).toBe('');
    });

    it('skips entries that cannot be normalized', () => {
        expect(normalizeAgentList({items: [{id: 'a'}, null, 7, {}]}).agents).toHaveLength(1);
    });

    it('returns an empty page for unexpected payloads', () => {
        expect(normalizeAgentList(undefined)).toEqual({agents: [], nextCursor: ''});
    });
});

describe('normalizeCreateAgentResponse', () => {
    it('unwraps the agent and run', () => {
        const {agent, run} = normalizeCreateAgentResponse({
            agent: {id: 'bc-1', name: 'Add README', repos: [{url: 'https://github.com/acme/bifrost'}]},
            run: {id: 'run-1', status: 'QUEUED'},
        });

        expect(agent?.id).toBe('bc-1');
        expect(run).toMatchObject({id: 'run-1', status: 'QUEUED'});
    });

    it('returns nulls when the envelope is missing', () => {
        expect(normalizeCreateAgentResponse(null)).toEqual({agent: null, run: null});
        expect(normalizeCreateAgentResponse({})).toEqual({agent: null, run: null});
    });
});

describe('normalizeMessages', () => {
    it('maps the v0 conversation shape and drops empty messages', () => {
        expect(normalizeMessages({
            id: 'bc-1',
            messages: [
                {id: 'm1', type: 'user_message', text: 'Add a README'},
                {id: 'm2', type: 'assistant_message', text: 'Done.'},
                {id: 'm3', type: 'assistant_message', text: '   '},
            ],
        })).toEqual([
            {id: 'm1', role: 'user', text: 'Add a README'},
            {id: 'm2', role: 'assistant', text: 'Done.'},
        ]);
    });

    it('treats any non-user type as assistant output and synthesizes missing ids', () => {
        expect(normalizeMessages({messages: [{type: 'tool_message', text: 'ran tests'}]})).toEqual([
            {id: 'message-0', role: 'assistant', text: 'ran tests'},
        ]);
    });

    it('returns nothing for a failed conversation fetch', () => {
        expect(normalizeMessages(null)).toEqual([]);
    });
});

describe('normalizeModels', () => {
    it('reads the items envelope, keeping id, display name and description', () => {
        expect(normalizeModels({
            items: [
                {id: 'composer-2', displayName: 'Composer 2', description: 'Fast'},
                {id: 'sonnet'},
                {},
            ],
        })).toEqual([
            {id: 'composer-2', displayName: 'Composer 2', description: 'Fast'},
            {id: 'sonnet', displayName: 'sonnet', description: ''},
        ]);
    });

    it('returns nothing when the models call failed', () => {
        expect(normalizeModels(null)).toEqual([]);
    });
});

describe('normalizeRepositories', () => {
    it('reads the items envelope, deduplicating and sorting by label', () => {
        expect(normalizeRepositories({
            items: [
                {url: 'https://github.com/acme/zulu'},
                {url: 'https://github.com/acme/alpha'},
                {url: 'https://github.com/acme/zulu'},
            ],
        })).toEqual([
            {url: 'https://github.com/acme/alpha', label: 'acme/alpha'},
            {url: 'https://github.com/acme/zulu', label: 'acme/zulu'},
        ]);
    });

    it('returns nothing when the repositories call failed', () => {
        expect(normalizeRepositories(null)).toEqual([]);
    });
});
