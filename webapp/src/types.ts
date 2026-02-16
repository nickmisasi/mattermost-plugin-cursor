// Agent status as returned by Cursor API
export type AgentStatus = 'CREATING' | 'RUNNING' | 'FINISHED' | 'FAILED' | 'STOPPED';

// Workflow phase as tracked by the HITL system
export type WorkflowPhase =
    | 'context_review'
    | 'planning'
    | 'plan_review'
    | 'implementing'
    | 'rejected'
    | 'complete';

// Review loop phase as tracked by the AI review system
export type ReviewLoopPhase =
    | 'requesting_review'
    | 'awaiting_review'
    | 'cursor_fixing'
    | 'approved'
    | 'human_review'
    | 'complete'
    | 'max_iterations'
    | 'failed';

// Agent data as stored/returned by the plugin backend
export interface Agent {
    id: string;
    status: AgentStatus;
    repository: string;
    branch: string;
    prompt: string;
    description?: string;
    pr_url: string;
    cursor_url: string;
    channel_id: string;
    post_id: string;
    root_post_id: string;
    summary: string;
    model: string;
    created_at: number;
    updated_at: number;

    // Archive flag
    archived?: boolean;

    // HITL workflow fields (populated when agent is part of a workflow)
    workflow_id?: string;
    workflow_phase?: WorkflowPhase;
    plan_iteration_count?: number;

    // Review loop fields (populated when agent has an active review loop)
    review_loop_id?: string;
    review_loop_phase?: ReviewLoopPhase;
    review_loop_iteration?: number;
}

// Response from GET /api/v1/agents
export interface AgentsResponse {
    agents: Agent[];
}

// HITL Workflow data as returned by the plugin backend
export interface Workflow {
    id: string;
    user_id: string;
    channel_id: string;
    root_post_id: string;
    phase: WorkflowPhase;
    repository: string;
    branch: string;
    model: string;
    original_prompt: string;
    enriched_context: string;
    approved_context: string;
    planner_agent_id: string;
    retrieved_plan: string;
    approved_plan: string;
    plan_iteration_count: number;
    implementer_agent_id: string;
    skip_context_review: boolean;
    skip_plan_loop: boolean;
    created_at: number;
    updated_at: number;
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
    description?: string;
    channel_id: string;
    post_id: string;
    cursor_url: string;
    created_at: string;
}

// Timeline event for a review loop
export interface ReviewLoopEvent {
    phase: ReviewLoopPhase;
    timestamp: number;
    detail?: string;
}

// ReviewLoop data as returned by the plugin backend
export interface ReviewLoop {
    id: string;
    agent_record_id: string;
    workflow_id?: string;
    user_id: string;
    channel_id: string;
    root_post_id: string;
    trigger_post_id: string;
    pr_url: string;
    pr_number: number;
    repository: string;
    phase: ReviewLoopPhase;
    iteration: number;
    last_commit_sha?: string;
    history: ReviewLoopEvent[];
    created_at: number;
    updated_at: number;
}

// WebSocket event data for workflow_phase_change
export interface WorkflowPhaseChangeEvent {
    workflow_id: string;
    phase: WorkflowPhase;
    planner_agent_id: string;
    implementer_agent_id: string;
    plan_iteration_count: string; // comes as string over WebSocket
    updated_at: string; // comes as string over WebSocket
}

// WebSocket event data for review_loop_changed
export interface ReviewLoopChangeEvent {
    review_loop_id: string;
    agent_record_id: string;
    phase: ReviewLoopPhase;
    iteration: string; // comes as string over WebSocket
    pr_url: string;
    updated_at: string; // comes as string over WebSocket
}

// Plugin Redux state shape
export interface PluginState {
    agents: Record<string, Agent>;
    workflows: Record<string, Workflow>;
    reviewLoops: Record<string, ReviewLoop>;
    selectedAgentId: string | null;
    isLoading: boolean;
}
