/**
 * Execution status of a run, always uppercase. Agents themselves only carry a
 * lifecycle status (ACTIVE/ARCHIVED); execution status lives on runs, and the
 * plugin copies the latest run's status onto list items as `runStatus`.
 */
export type RunStatus = 'QUEUED' | 'CREATING' | 'RUNNING' | 'FINISHED' | 'ERROR' | 'CANCELLED' | 'EXPIRED';

export const RUN_STATUSES: RunStatus[] = ['QUEUED', 'CREATING', 'RUNNING', 'FINISHED', 'ERROR', 'CANCELLED', 'EXPIRED'];

export function isActiveRunStatus(status?: RunStatus): boolean {
    return status === 'QUEUED' || status === 'CREATING' || status === 'RUNNING';
}

export function isTerminalRunStatus(status?: RunStatus): boolean {
    return status === 'FINISHED' || status === 'ERROR' || status === 'CANCELLED' || status === 'EXPIRED';
}

export interface KeyStatus {
    configured: boolean;
    email: string;
}

/*
 * Raw (passthrough) shapes. Every field is optional: the Cursor API is in beta
 * and the plugin forwards responses verbatim, so rendering must never assume a
 * field is present.
 */

export interface RawRepoConfig {
    url?: string;
    startingRef?: string;
}

export interface RawGitBranch {
    repoUrl?: string;
    branch?: string;
    prUrl?: string;
}

export interface RawRun {
    id?: string;
    agentId?: string;
    status?: string;
    createdAt?: string;
    updatedAt?: string;
    durationMs?: number;
    result?: string;
    git?: {branches?: RawGitBranch[]};
}

export interface RawAgent {
    id?: string;
    name?: string;
    status?: string;
    env?: {type?: string};
    url?: string;
    createdAt?: string;
    updatedAt?: string;
    latestRunId?: string;
    repos?: RawRepoConfig[];
    workOnCurrentBranch?: boolean;
    autoCreatePR?: boolean;

    // Added by the plugin when enriching list items; absent on GET /agents/{id}.
    branch?: string;
    prUrl?: string;
    runStatus?: string;
}

export interface RawMessage {
    id?: string;
    type?: 'user_message' | 'assistant_message' | string;
    text?: string;
}

export interface CreateAgentRequest {
    prompt: string;
    repository: string;
    ref?: string;
    model?: string;
    autoCreatePr?: boolean;
}

/*
 * Normalized shapes used by the UI.
 */

export interface Agent {
    id: string;
    name: string;
    repositoryUrl: string;
    repository: string;
    startingRef: string;
    branch: string;
    prUrl: string;
    webUrl: string;
    envType: string;
    createdAt: number;
    updatedAt: number;
    activityAt: number;
    archived: boolean;
    runStatus?: RunStatus;
    latestRunId: string;
}

export interface RepoGroup {
    key: string;
    repository: string;
    repositoryUrl: string;
    activityAt: number;
    agents: Agent[];
}

export interface Run {
    id: string;
    status?: RunStatus;
    durationMs: number;

    // Final assistant text, populated only once the run is terminal.
    result: string;

    // `git` on a run is per-agent state: it is where the pushed branch and PR
    // live, since the agent payload itself carries neither.
    branch: string;
    prUrl: string;
}

export interface ConversationMessage {
    id: string;
    role: 'user' | 'assistant';
    text: string;
}

export interface ModelOption {
    id: string;
    displayName: string;
    description: string;
}

export interface RepositoryOption {
    url: string;
    label: string;
}
