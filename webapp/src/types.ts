// Agent status as returned by Cursor API
export type AgentStatus = 'CREATING' | 'RUNNING' | 'FINISHED' | 'FAILED' | 'STOPPED';

// Agent data as stored/returned by the plugin backend
export interface Agent {
    id: string;
    status: AgentStatus;
    repository: string;
    branch: string;
    prompt: string;
    pr_url: string;
    cursor_url: string;
    channel_id: string;
    post_id: string;
    root_post_id: string;
    summary: string;
    model: string;
    created_at: number;
    updated_at: number;
}

// Response from GET /api/v1/agents
export interface AgentsResponse {
    agents: Agent[];
}

// Request body for POST /api/v1/agents/{id}/followup
export interface FollowupRequest {
    message: string;
}

// Generic status response
export interface StatusResponse {
    status: string;
}

// WebSocket event data for agent_status_change
export interface AgentStatusChangeEvent {
    agent_id: string;
    status: AgentStatus;
    pr_url: string;
    summary: string;
    repository: string;
    updated_at: string;
}

// WebSocket event data for agent_created
export interface AgentCreatedEvent {
    agent_id: string;
    status: AgentStatus;
    repository: string;
    branch: string;
    prompt: string;
    channel_id: string;
    post_id: string;
    cursor_url: string;
    created_at: string;
}

// Plugin Redux state shape
export interface PluginState {
    agents: Record<string, Agent>;
    selectedAgentId: string | null;
    isLoading: boolean;
}
