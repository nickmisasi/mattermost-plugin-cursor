import type {Agent, RepoGroup} from '../types';

export const COLLAPSED_GROUP_SIZE = 3;

export interface GroupOptions {
    includeArchived?: boolean;
    query?: string;
}

export function matchesQuery(agent: Agent, query: string): boolean {
    const needle = query.trim().toLowerCase();
    if (!needle) {
        return true;
    }

    return [agent.name, agent.branch, agent.repository, agent.repositoryUrl, agent.startingRef].
        some((field) => field.toLowerCase().includes(needle));
}

/**
 * Groups agents by their repository for the sidebar. Groups are ordered by the
 * most recent activity they contain, and agents within a group are newest
 * first. Pure so it can be unit tested without rendering.
 */
export function groupAgentsByRepository(agents: Agent[], options: GroupOptions = {}): RepoGroup[] {
    const {includeArchived = false, query = ''} = options;
    const groups = new Map<string, RepoGroup>();

    for (const agent of agents) {
        if (agent.archived && !includeArchived) {
            continue;
        }
        if (!matchesQuery(agent, query)) {
            continue;
        }

        const key = (agent.repositoryUrl || agent.repository).toLowerCase();
        const existing = groups.get(key);
        if (existing) {
            existing.agents.push(agent);
            existing.activityAt = Math.max(existing.activityAt, agent.activityAt);
        } else {
            groups.set(key, {
                key,
                repository: agent.repository,
                repositoryUrl: agent.repositoryUrl,
                activityAt: agent.activityAt,
                agents: [agent],
            });
        }
    }

    const result = Array.from(groups.values());
    for (const group of result) {
        group.agents.sort((a, b) => b.activityAt - a.activityAt || a.name.localeCompare(b.name));
    }
    result.sort((a, b) => b.activityAt - a.activityAt || a.repository.localeCompare(b.repository));

    return result;
}
