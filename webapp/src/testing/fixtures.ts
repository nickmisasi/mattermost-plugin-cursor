import type {Agent} from '../types';

const BASE_AGENT: Agent = {
    id: 'bc-0',
    name: 'Agent',
    repositoryUrl: 'https://github.com/acme/bifrost',
    repository: 'acme/bifrost',
    startingRef: 'main',
    branch: '',
    prUrl: '',
    webUrl: 'https://cursor.com/agents/bc-0',
    envType: 'cloud',
    createdAt: 0,
    updatedAt: 0,
    activityAt: 0,
    archived: false,
    runStatus: 'FINISHED',
    latestRunId: 'run-0',
};

export function makeAgent(overrides: Partial<Agent> = {}): Agent {
    return {...BASE_AGENT, ...overrides};
}
