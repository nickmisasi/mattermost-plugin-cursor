import {asArray, asRunStatus, asString, isRecord} from './guards';
import {toTimestamp} from './time';

import type {
    Agent,
    ConversationMessage,
    ModelOption,
    RawAgent,
    RawGitBranch,
    RawMessage,
    RawRun,
    RepositoryOption,
    Run,
} from '../types';

export const NO_REPOSITORY = 'No repository';

function listItems(payload: unknown, key: string): unknown[] {
    if (Array.isArray(payload)) {
        return payload;
    }
    if (isRecord(payload) && Array.isArray(payload[key])) {
        return payload[key] as unknown[];
    }
    return [];
}

/**
 * Turns a repository URL into the short "org/repo" label used in the sidebar.
 */
export function repoDisplayName(url: string): string {
    if (!url) {
        return NO_REPOSITORY;
    }

    const trimmed = url.
        trim().
        replace(/^[a-z][a-z0-9+.-]*:\/\//i, '').
        replace(/^git@([^:]+):/i, '$1/').
        replace(/^www\./i, '').
        replace(/\.git$/i, '').
        replace(/\/+$/, '').
        replace(/^github\.com\//i, '');

    return trimmed || NO_REPOSITORY;
}

/**
 * `run.git.branches` holds one entry per pushed branch. Stacked agents push
 * several; the first entry that actually carries a branch or PR is the one the
 * UI shows.
 */
function pickBranch(branches: unknown): RawGitBranch | undefined {
    for (const entry of asArray(branches)) {
        if (isRecord(entry) && (entry.branch || entry.prUrl)) {
            return entry as RawGitBranch;
        }
    }
    return undefined;
}

export function normalizeRun(input: unknown): Run | null {
    if (!isRecord(input)) {
        return null;
    }

    const raw = input as RawRun;
    const id = asString(raw.id);
    if (!id) {
        return null;
    }

    const branch = pickBranch(raw.git?.branches);

    return {
        id,
        status: asRunStatus(raw.status),
        durationMs: typeof raw.durationMs === 'number' ? raw.durationMs : 0,
        result: asString(raw.result),
        branch: asString(branch?.branch),
        prUrl: asString(branch?.prUrl),
    };
}

/**
 * An agent is archived when its lifecycle status says so. The list endpoint
 * documents its items as carrying "only the durable identity fields", so the
 * lifecycle is not guaranteed to be spelled out there and the archived filter
 * cannot rely on this alone — the request also asks the server to exclude
 * archived agents. See `useAgents`.
 */
function isArchived(raw: RawAgent): boolean {
    if (typeof raw.archived === 'boolean') {
        return raw.archived;
    }
    return asString(raw.status).toUpperCase() === 'ARCHIVED';
}

/**
 * Flattens an agent payload for rendering. `branch`, `prUrl` and `runStatus`
 * are added by the plugin when it enriches list items; `GET /agents/{id}` has
 * none of them, so the detail view fills those in from the latest run.
 */
export function normalizeAgent(input: unknown): Agent | null {
    if (!isRecord(input)) {
        return null;
    }

    const raw = input as RawAgent;
    const id = asString(raw.id);
    if (!id) {
        return null;
    }

    const primaryRepo = asArray(raw.repos).filter(isRecord).find((repo) => asString(repo.url));
    const repositoryUrl = asString(primaryRepo?.url);

    const createdAt = toTimestamp(raw.createdAt);
    const updatedAt = toTimestamp(raw.updatedAt);

    return {
        id,
        name: asString(raw.name).trim() || 'Untitled agent',
        repositoryUrl,
        repository: repoDisplayName(repositoryUrl),
        startingRef: asString(primaryRepo?.startingRef),
        branch: asString(raw.branch),
        prUrl: asString(raw.prUrl),
        webUrl: asString(raw.url),
        envType: asString(raw.env?.type),
        createdAt,
        updatedAt,
        activityAt: Math.max(createdAt, updatedAt),
        archived: isArchived(raw),
        runStatus: asRunStatus(raw.runStatus),
        latestRunId: asString(raw.latestRunId),
    };
}

export interface AgentListPage {
    agents: Agent[];
    nextCursor: string;
}

export function normalizeAgentList(payload: unknown): AgentListPage {
    const agents: Agent[] = [];
    for (const item of listItems(payload, 'items')) {
        const agent = normalizeAgent(item);
        if (agent) {
            agents.push(agent);
        }
    }

    // `nextCursor` is omitted rather than nulled when there are no more pages.
    const nextCursor = isRecord(payload) ? asString(payload.nextCursor) : '';
    return {agents, nextCursor};
}

/**
 * The create-agent response wraps both the durable agent and its initial run.
 */
export function normalizeCreateAgentResponse(payload: unknown): {agent: Agent | null; run: Run | null} {
    if (!isRecord(payload)) {
        return {agent: null, run: null};
    }
    return {agent: normalizeAgent(payload.agent), run: normalizeRun(payload.run)};
}

export function normalizeMessages(payload: unknown): ConversationMessage[] {
    const messages: ConversationMessage[] = [];

    listItems(payload, 'messages').forEach((item, index) => {
        if (!isRecord(item)) {
            return;
        }

        const raw = item as RawMessage;
        const text = asString(raw.text);
        if (!text.trim()) {
            return;
        }

        messages.push({
            id: asString(raw.id) || `message-${index}`,
            role: asString(raw.type) === 'user_message' ? 'user' : 'assistant',
            text,
        });
    });

    return messages;
}

export function normalizeModels(payload: unknown): ModelOption[] {
    const models: ModelOption[] = [];

    for (const item of listItems(payload, 'items')) {
        if (!isRecord(item)) {
            continue;
        }
        const id = asString(item.id);
        if (id) {
            models.push({id, displayName: asString(item.displayName) || id, description: asString(item.description)});
        }
    }

    return models;
}

export function normalizeRepositories(payload: unknown): RepositoryOption[] {
    const repositories: RepositoryOption[] = [];
    const seen = new Set<string>();

    for (const item of listItems(payload, 'items')) {
        const url = isRecord(item) ? asString(item.url) : '';
        if (!url || seen.has(url)) {
            continue;
        }
        seen.add(url);
        repositories.push({url, label: repoDisplayName(url)});
    }

    repositories.sort((a, b) => a.label.localeCompare(b.label));
    return repositories;
}
