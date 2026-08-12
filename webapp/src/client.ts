import manifest from './manifest';
import type {CreateAgentRequest, KeyStatus} from './types';

export const API_BASE = `/plugins/${manifest.id}/api/v1`;

export const API_KEY_NOT_CONFIGURED = 'api_key_not_configured';

interface ApiErrorBody {
    error?: string;
    message?: string;
    code?: string;
}

export class ClientError extends Error {
    readonly status: number;
    readonly code: string;

    constructor(status: number, message: string, code = '') {
        super(message);
        this.name = 'ClientError';
        this.status = status;
        this.code = code;

        // Required so `instanceof` keeps working after transpilation.
        Object.setPrototypeOf(this, ClientError.prototype);
    }
}

export function isNotConfiguredError(error: unknown): boolean {
    return error instanceof ClientError && error.code === API_KEY_NOT_CONFIGURED;
}

export function errorMessage(error: unknown, fallback = 'Something went wrong.'): string {
    if (error instanceof ClientError) {
        return error.message || fallback;
    }
    if (error instanceof Error && error.message) {
        return error.message;
    }
    return fallback;
}

async function toClientError(response: Response): Promise<ClientError> {
    let message = `Request failed with status ${response.status}.`;
    let code = '';

    const text = await response.text().catch(() => '');
    if (text) {
        try {
            const body = JSON.parse(text) as ApiErrorBody;
            const parsedMessage = body.error || body.message;
            if (parsedMessage) {
                message = parsedMessage;
            }
            if (body.code) {
                code = body.code;
            }
        } catch {
            // Non-JSON error body; keep the status-based message.
        }
    }

    return new ClientError(response.status, message, code);
}

interface RequestOptions {
    method?: string;
    body?: unknown;
    query?: Record<string, string | number | boolean | undefined>;
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
    if (!query) {
        return API_BASE + path;
    }

    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(query)) {
        if (value !== undefined) {
            params.set(key, String(value));
        }
    }

    const search = params.toString();
    return search ? `${API_BASE}${path}?${search}` : API_BASE + path;
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const method = options.method ?? 'GET';
    const headers: Record<string, string> = {Accept: 'application/json'};

    if (method !== 'GET') {
        headers['X-Requested-With'] = 'XMLHttpRequest';
    }
    if (options.body !== undefined) {
        headers['Content-Type'] = 'application/json';
    }

    const response = await fetch(buildUrl(path, options.query), {
        method,
        headers,
        credentials: 'same-origin',
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });

    if (!response.ok) {
        throw await toClientError(response);
    }

    if (response.status === 204) {
        return undefined as unknown as T;
    }

    const text = await response.text();
    if (!text) {
        return undefined as unknown as T;
    }

    try {
        return JSON.parse(text) as T;
    } catch {
        throw new ClientError(response.status, 'The server returned a response that could not be read.');
    }
}

export interface ListAgentsOptions {
    limit?: number;
    cursor?: string;

    // Required: upstream defaults this to true, so the caller always decides.
    includeArchived: boolean;
}

export const Client = {
    getKeyStatus(): Promise<KeyStatus> {
        return request<KeyStatus>('/key');
    },

    setKey(apiKey: string): Promise<KeyStatus> {
        return request<KeyStatus>('/key', {method: 'PUT', body: {apiKey}});
    },

    deleteKey(): Promise<void> {
        return request<void>('/key', {method: 'DELETE'});
    },

    listAgents(options: ListAgentsOptions): Promise<unknown> {
        return request<unknown>('/agents', {
            query: {
                limit: options.limit,
                cursor: options.cursor,
                includeArchived: options.includeArchived,
            },
        });
    },

    createAgent(body: CreateAgentRequest): Promise<unknown> {
        return request<unknown>('/agents', {method: 'POST', body});
    },

    getAgent(agentId: string): Promise<unknown> {
        return request<unknown>(`/agents/${encodeURIComponent(agentId)}`);
    },

    deleteAgent(agentId: string): Promise<void> {
        return request<void>(`/agents/${encodeURIComponent(agentId)}`, {method: 'DELETE'});
    },

    archiveAgent(agentId: string): Promise<void> {
        return request<void>(`/agents/${encodeURIComponent(agentId)}/archive`, {method: 'POST'});
    },

    unarchiveAgent(agentId: string): Promise<void> {
        return request<void>(`/agents/${encodeURIComponent(agentId)}/unarchive`, {method: 'POST'});
    },

    getMessages(agentId: string): Promise<unknown> {
        return request<unknown>(`/agents/${encodeURIComponent(agentId)}/messages`);
    },

    followup(agentId: string, prompt: string): Promise<unknown> {
        return request<unknown>(`/agents/${encodeURIComponent(agentId)}/followup`, {method: 'POST', body: {prompt}});
    },

    getRun(agentId: string, runId: string): Promise<unknown> {
        return request<unknown>(`/agents/${encodeURIComponent(agentId)}/runs/${encodeURIComponent(runId)}`);
    },

    cancelRun(agentId: string, runId: string): Promise<void> {
        return request<void>(
            `/agents/${encodeURIComponent(agentId)}/runs/${encodeURIComponent(runId)}/cancel`,
            {method: 'POST'},
        );
    },

    listModels(): Promise<unknown> {
        return request<unknown>('/models');
    },

    listRepositories(): Promise<unknown> {
        return request<unknown>('/repositories');
    },

    streamUrl(agentId: string, runId: string): string {
        return `${API_BASE}/agents/${encodeURIComponent(agentId)}/runs/${encodeURIComponent(runId)}/stream`;
    },
};

export default Client;
