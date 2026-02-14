package cursor

import (
	"fmt"
	"time"
)

// AgentStatus represents the lifecycle state of a Cursor background agent.
type AgentStatus string

const (
	AgentStatusCreating AgentStatus = "CREATING"
	AgentStatusRunning  AgentStatus = "RUNNING"
	AgentStatusFinished AgentStatus = "FINISHED"
	AgentStatusFailed   AgentStatus = "FAILED"
	AgentStatusStopped  AgentStatus = "STOPPED"
)

// IsTerminal returns true if the agent has reached a final state.
func (s AgentStatus) IsTerminal() bool {
	return s == AgentStatusFinished || s == AgentStatusFailed || s == AgentStatusStopped
}

// --- Launch Agent ---

// LaunchAgentRequest is the POST /v0/agents request body.
type LaunchAgentRequest struct {
	Prompt  Prompt   `json:"prompt"`
	Source  Source   `json:"source"`
	Target  *Target  `json:"target,omitempty"`
	Model   string   `json:"model,omitempty"`
	Webhook *Webhook `json:"webhook,omitempty"`
}

type Prompt struct {
	Text   string  `json:"text"`
	Images []Image `json:"images,omitempty"`
}

type Image struct {
	Data      string         `json:"data"`
	Dimension ImageDimension `json:"dimension"`
}

type ImageDimension struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Source struct {
	Repository string `json:"repository"`
	Ref        string `json:"ref,omitempty"`
	PrURL      string `json:"prUrl,omitempty"`
}

type Target struct {
	BranchName            string `json:"branchName,omitempty"`
	AutoCreatePr          bool   `json:"autoCreatePr,omitempty"`
	AutoBranch            bool   `json:"autoBranch"`
	OpenAsCursorGithubApp bool   `json:"openAsCursorGithubApp,omitempty"`
	SkipReviewerRequest   bool   `json:"skipReviewerRequest,omitempty"`
}

type Webhook struct {
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

// --- Agent (response object) ---

// Agent is the response object from GET /v0/agents/{id} and POST /v0/agents.
type Agent struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Status    AgentStatus `json:"status"`
	Source    Source      `json:"source"`
	Target    AgentTarget `json:"target"`
	Summary   string      `json:"summary"`
	CreatedAt time.Time   `json:"createdAt"`
}

// AgentTarget is the target field in Agent responses (differs from request Target).
type AgentTarget struct {
	BranchName            string `json:"branchName"`
	URL                   string `json:"url"`
	PrURL                 string `json:"prUrl"`
	AutoCreatePr          bool   `json:"autoCreatePr"`
	OpenAsCursorGithubApp bool   `json:"openAsCursorGithubApp"`
	SkipReviewerRequest   bool   `json:"skipReviewerRequest"`
}

// --- List Agents ---

// ListAgentsResponse is the GET /v0/agents response.
type ListAgentsResponse struct {
	Agents     []Agent `json:"agents"`
	NextCursor string  `json:"nextCursor"`
}

// --- Follow-up ---

// FollowupRequest is the POST /v0/agents/{id}/followup request body.
type FollowupRequest struct {
	Prompt Prompt `json:"prompt"`
}

// FollowupResponse is the POST /v0/agents/{id}/followup response.
type FollowupResponse struct {
	ID string `json:"id"`
}

// --- Stop Agent ---

// StopResponse is the POST /v0/agents/{id}/stop response.
type StopResponse struct {
	ID string `json:"id"`
}

// --- Delete Agent ---

// DeleteResponse is the DELETE /v0/agents/{id} response.
type DeleteResponse struct {
	ID string `json:"id"`
}

// --- Conversation ---

// Conversation is the GET /v0/agents/{id}/conversation response.
type Conversation struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
}

type Message struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "user_message" or "assistant_message"
	Text string `json:"text"`
}

// --- Models ---

// ListModelsResponse is the GET /v0/models response.
type ListModelsResponse struct {
	Models []string `json:"models"`
}

// --- API Key Info ---

// APIKeyInfo is the GET /v0/me response.
type APIKeyInfo struct {
	APIKeyName string `json:"apiKeyName"`
	CreatedAt  string `json:"createdAt"`
	UserEmail  string `json:"userEmail"`
}

// --- Errors ---

// APIError represents an error response from the Cursor API.
type APIError struct {
	StatusCode int    `json:"-"`
	Message    string `json:"message"`
	RawBody    string `json:"-"` // The raw response body, useful for debugging when Message is empty.
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" && e.RawBody != "" {
		msg = e.RawBody
	}
	return fmt.Sprintf("cursor API error (HTTP %d): %s", e.StatusCode, msg)
}
