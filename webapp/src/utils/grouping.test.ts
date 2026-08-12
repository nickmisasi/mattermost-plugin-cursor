import {groupAgentsByRepository, matchesQuery} from './grouping';

import {makeAgent} from '../testing/fixtures';
import type {Agent} from '../types';

function agent(overrides: Partial<Agent>): Agent {
    return makeAgent({
        repositoryUrl: 'https://github.com/mattermost/factory',
        repository: 'mattermost/factory',
        ...overrides,
    });
}

describe('matchesQuery', () => {
    const subject = agent({
        name: 'Bedrock custom endpoint support',
        branch: 'cursor/bedrock-custom-endpoints-9284',
    });

    it('matches an empty query', () => {
        expect(matchesQuery(subject, '   ')).toBe(true);
    });

    it('matches name, branch and repository case-insensitively', () => {
        expect(matchesQuery(subject, 'BEDROCK')).toBe(true);
        expect(matchesQuery(subject, 'endpoints-9284')).toBe(true);
        expect(matchesQuery(subject, 'factory')).toBe(true);
    });

    it('rejects text that appears nowhere', () => {
        expect(matchesQuery(subject, 'kubernetes')).toBe(false);
    });
});

describe('groupAgentsByRepository', () => {
    const bifrostOld = agent({id: 'a', name: 'Old bifrost work', repositoryUrl: 'https://github.com/acme/bifrost', repository: 'acme/bifrost', activityAt: 100});
    const bifrostNew = agent({id: 'b', name: 'New bifrost work', repositoryUrl: 'https://github.com/acme/bifrost', repository: 'acme/bifrost', activityAt: 300});
    const factory = agent({id: 'c', name: 'Factory work', activityAt: 200});

    it('groups agents by repository', () => {
        const groups = groupAgentsByRepository([bifrostOld, factory, bifrostNew]);

        expect(groups.map((group) => group.repository)).toEqual(['acme/bifrost', 'mattermost/factory']);
        expect(groups[0].agents.map((item) => item.id)).toEqual(['b', 'a']);
    });

    it('orders groups by their most recent activity and agents newest first', () => {
        const groups = groupAgentsByRepository([factory, bifrostOld]);

        expect(groups.map((group) => group.repository)).toEqual(['mattermost/factory', 'acme/bifrost']);
    });

    it('treats repository URLs case-insensitively when grouping', () => {
        const groups = groupAgentsByRepository([
            bifrostNew,
            agent({id: 'd', repositoryUrl: 'https://github.com/ACME/Bifrost', repository: 'ACME/Bifrost', activityAt: 50}),
        ]);

        expect(groups).toHaveLength(1);
        expect(groups[0].agents).toHaveLength(2);
    });

    it('excludes archived agents unless asked for them', () => {
        const archived = agent({id: 'z', name: 'Archived', archived: true, activityAt: 999});

        expect(groupAgentsByRepository([factory, archived])).toHaveLength(1);

        const withArchived = groupAgentsByRepository([factory, archived], {includeArchived: true});
        expect(withArchived[0].agents.map((item) => item.id)).toEqual(['z', 'c']);
    });

    it('drops groups whose agents are all filtered out by the query', () => {
        const groups = groupAgentsByRepository([bifrostNew, factory], {query: 'factory'});

        expect(groups).toHaveLength(1);
        expect(groups[0].repository).toBe('mattermost/factory');
    });

    it('returns no groups for an empty list', () => {
        expect(groupAgentsByRepository([])).toEqual([]);
    });
});
