package kvstore

// AgentRecord stores the plugin's state for a tracked Cursor agent.
type AgentRecord struct {
	CursorAgentID  string `json:"cursorAgentId"`
	PostID         string `json:"postId"`                   // The bot's reply post (thread root)
	TriggerPostID  string `json:"triggerPostId"`            // The user's original @mention post
	BotReplyPostID string `json:"botReplyPostId,omitempty"` // The bot's initial reply (for updating on terminal status)
	ChannelID      string `json:"channelId"`
	UserID         string `json:"userId"`
	Status         string `json:"status"`
	Repository     string `json:"repository"`
	Branch         string `json:"branch"`
	TargetBranch   string `json:"targetBranch,omitempty"` // Cursor-created branch (e.g., "cursor/fix-login")
	PrURL          string `json:"prUrl"`
	Prompt         string `json:"prompt"`
	Description    string `json:"description,omitempty"` // AI-generated short task summary
	Model          string `json:"model"`
	Summary        string `json:"summary"`
	CreatedAt      int64  `json:"createdAt"`          // Unix millis
	UpdatedAt      int64  `json:"updatedAt"`          // Unix millis
	Archived       bool   `json:"archived,omitempty"` // Soft-archived by user
}

// ChannelSettings stores per-channel defaults.
type ChannelSettings struct {
	DefaultRepository string `json:"defaultRepository"`
	DefaultBranch     string `json:"defaultBranch"`
}

// UserSettings stores per-user defaults.
type UserSettings struct {
	DefaultRepository   string `json:"defaultRepository"`
	DefaultBranch       string `json:"defaultBranch"`
	DefaultModel        string `json:"defaultModel"`
	EnableContextReview *bool  `json:"enableContextReview,omitempty"` // nil = use global config
	EnablePlanLoop      *bool  `json:"enablePlanLoop,omitempty"`      // nil = use global config
}

// HITLWorkflow tracks the full lifecycle of a Human-In-The-Loop verification
// pipeline from @mention through implementation. Exists alongside AgentRecords.
type HITLWorkflow struct {
	ID            string `json:"id"`            // UUID, primary key
	UserID        string `json:"userId"`        // Initiating Mattermost user
	ChannelID     string `json:"channelId"`     // Mattermost channel
	RootPostID    string `json:"rootPostId"`    // Thread root post ID
	TriggerPostID string `json:"triggerPostId"` // The @mention post that started this workflow

	// Phase tracks where we are in the HITL pipeline.
	Phase string `json:"phase"`

	// Resolved parameters (from parse + defaults cascade).
	Repository     string `json:"repository"`
	Branch         string `json:"branch"`
	Model          string `json:"model"`
	AutoCreatePR   bool   `json:"autoCreatePr"`
	OriginalPrompt string `json:"originalPrompt"` // Raw user prompt text

	// Context review state.
	EnrichedContext string     `json:"enrichedContext,omitempty"` // Bridge client output
	ApprovedContext string     `json:"approvedContext,omitempty"` // Finalized after user approval
	ContextPostID   string     `json:"contextPostId,omitempty"`   // Post with Accept/Reject buttons
	ContextImages   []ImageRef `json:"contextImages,omitempty"`   // Serializable image file references

	// Plan loop state.
	PlannerAgentID     string `json:"plannerAgentId,omitempty"`     // Current planner Cursor agent ID
	RetrievedPlan      string `json:"retrievedPlan,omitempty"`      // Plan text from conversation API
	ApprovedPlan       string `json:"approvedPlan,omitempty"`       // Finalized after user approval
	PlanPostID         string `json:"planPostId,omitempty"`         // Post with Accept/Reject buttons
	PlanIterationCount int    `json:"planIterationCount,omitempty"` // Number of plan iterations
	PlanFeedback       string `json:"planFeedback,omitempty"`       // User's feedback for the next planning iteration

	// PendingFeedback stores user feedback submitted while a planner agent is running.
	// When the planner finishes and transitions to plan_review, this feedback is
	// automatically applied as an iteration (the plan is not shown for review;
	// instead a new planner is launched with the feedback).
	PendingFeedback string `json:"pendingFeedback,omitempty"`

	// Implementation state.
	ImplementerAgentID string `json:"implementerAgentId,omitempty"` // Implementation Cursor agent ID

	// Per-workflow overrides (resolved at creation time from flags + settings cascade).
	SkipContextReview bool `json:"skipContextReview,omitempty"`
	SkipPlanLoop      bool `json:"skipPlanLoop,omitempty"`

	CreatedAt int64 `json:"createdAt"` // Unix milliseconds
	UpdatedAt int64 `json:"updatedAt"` // Unix milliseconds
}

// ImageRef is a serializable reference to a prompt image. Full image data
// is stored in Mattermost file storage and re-fetched by file ID when needed.
type ImageRef struct {
	FileID string `json:"fileId"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ReviewLoop tracks the automated AI review cycle for a Cursor-created PR.
// Separate from AgentRecord and HITLWorkflow. Linked back via AgentRecordID.
type ReviewFinding struct {
	Key                string `json:"key,omitempty"`                // Stable fingerprint for dedupe tracking
	Status             string `json:"status,omitempty"`             // open|resolved|dismissed|superseded
	SourceType         string `json:"sourceType,omitempty"`         // review_comment|review_body|issue_comment
	SourceID           int64  `json:"sourceId,omitempty"`           // Numeric source comment/review ID
	SourceNodeID       string `json:"sourceNodeId,omitempty"`       // GitHub node ID for traceability
	SourceURL          string `json:"sourceUrl,omitempty"`          // GitHub HTML URL
	ReviewerLogin      string `json:"reviewerLogin,omitempty"`      // GitHub login of feedback author
	ReviewerType       string `json:"reviewerType,omitempty"`       // ai_bot|human
	Path               string `json:"path,omitempty"`               // File path for inline comments
	Line               int    `json:"line,omitempty"`               // File line for inline comments
	CommitSHA          string `json:"commitSha,omitempty"`          // Commit SHA associated with finding
	RawText            string `json:"rawText,omitempty"`            // Raw reviewer text (may be truncated)
	ActionableText     string `json:"actionableText,omitempty"`     // Extracted actionable directive
	FirstSeenAt        int64  `json:"firstSeenAt,omitempty"`        // Unix millis
	LastSeenAt         int64  `json:"lastSeenAt,omitempty"`         // Unix millis
	FirstSeenIteration int    `json:"firstSeenIteration,omitempty"` // Review-loop iteration first observed
	LastSeenIteration  int    `json:"lastSeenIteration,omitempty"`  // Review-loop iteration last observed
}

type ReviewLoop struct {
	ID            string `json:"id"`                   // UUID primary key
	AgentRecordID string `json:"agentRecordId"`        // Agent that created the PR
	WorkflowID    string `json:"workflowId,omitempty"` // HITL workflow ID, if applicable
	UserID        string `json:"userId"`
	ChannelID     string `json:"channelId"`
	RootPostID    string `json:"rootPostId"` // Mattermost thread root
	TriggerPostID string `json:"triggerPostId"`

	// PR info (populated from AgentRecord + parsed PR URL)
	PRURL      string `json:"prUrl"`
	PRNumber   int    `json:"prNumber"`
	Repository string `json:"repository"` // "owner/repo"
	Owner      string `json:"owner"`      // Parsed from PR URL
	Repo       string `json:"repo"`       // Parsed from PR URL

	// State machine
	Phase     string `json:"phase"`     // See ReviewPhase* constants
	Iteration int    `json:"iteration"` // Current fix-review iteration (starts at 1)

	// Tracking
	LastCommitSHA string `json:"lastCommitSha,omitempty"` // HEAD SHA we last saw

	// Feedback dispatch tracking (Phase 1 data plumbing only)
	LastFeedbackDispatchAt  int64           `json:"lastFeedbackDispatchAt,omitempty"`  // Unix millis
	LastFeedbackDispatchSHA string          `json:"lastFeedbackDispatchSha,omitempty"` // SHA used for last dispatched bundle
	LastFeedbackDigest      string          `json:"lastFeedbackDigest,omitempty"`      // Digest for idempotency checks
	FeedbackCursor          string          `json:"feedbackCursor,omitempty"`          // Reserved for paging/cursor strategies
	Findings                []ReviewFinding `json:"findings,omitempty"`                // Persisted bounded finding history

	// Timeline (append-only log of phase transitions for dashboard display)
	History []ReviewLoopEvent `json:"history,omitempty"`

	CreatedAt int64 `json:"createdAt"` // Unix millis
	UpdatedAt int64 `json:"updatedAt"` // Unix millis
}

// ReviewLoopEvent records a single phase transition for the dashboard timeline.
type ReviewLoopEvent struct {
	Phase     string `json:"phase"`
	Timestamp int64  `json:"timestamp"`        // Unix millis
	Detail    string `json:"detail,omitempty"` // e.g., "3 comments", "approved after 2 iterations"
}

// HITL workflow phase constants.
const (
	PhaseContextReview = "context_review" // Waiting for user to approve enriched context
	PhasePlanning      = "planning"       // Planner Cursor agent is running
	PhasePlanReview    = "plan_review"    // Waiting for user to approve plan
	PhaseImplementing  = "implementing"   // Implementation Cursor agent is running
	PhaseRejected      = "rejected"       // User rejected at any stage (terminal)
	PhaseComplete      = "complete"       // Implementation finished (terminal)
)

// ReviewLoop phase constants.
const (
	ReviewPhaseRequestingReview = "requesting_review" // Just requested AI reviewers
	ReviewPhaseAwaitingReview   = "awaiting_review"   // Waiting for CodeRabbit/Copilot
	ReviewPhaseCursorFixing     = "cursor_fixing"     // Feedback dispatched, waiting for Cursor fixes
	ReviewPhaseApproved         = "approved"          // CodeRabbit approved
	ReviewPhaseHumanReview      = "human_review"      // Human reviewers assigned
	ReviewPhaseComplete         = "complete"          // Human approved (terminal)
	ReviewPhaseMaxIterations    = "max_iterations"    // Safety limit hit (terminal)
	ReviewPhaseFailed           = "failed"            // Error during review loop (terminal)
)

// KVStore defines the storage interface for the plugin.
type KVStore interface {
	// Agent records
	GetAgent(cursorAgentID string) (*AgentRecord, error)
	SaveAgent(record *AgentRecord) error
	DeleteAgent(cursorAgentID string) error
	ListActiveAgents() ([]*AgentRecord, error)
	GetAgentsByUser(userID string) ([]*AgentRecord, error)

	// Agent lookup by PR URL or branch (Phase 6: GitHub webhook support)
	GetAgentByPRURL(prURL string) (*AgentRecord, error)
	GetAgentByBranch(branchName string) (*AgentRecord, error)

	// Thread-to-agent mapping
	GetAgentIDByThread(rootPostID string) (string, error)
	SetThreadAgent(rootPostID string, cursorAgentID string) error
	DeleteThreadAgent(rootPostID string) error

	// Channel settings
	GetChannelSettings(channelID string) (*ChannelSettings, error)
	SaveChannelSettings(channelID string, settings *ChannelSettings) error

	// User settings
	GetUserSettings(userID string) (*UserSettings, error)
	SaveUserSettings(userID string, settings *UserSettings) error

	// Idempotency (Phase 6: GitHub webhook dedup)
	HasDeliveryBeenProcessed(deliveryID string) (bool, error)
	MarkDeliveryProcessed(deliveryID string) error

	// HITL workflow records
	GetWorkflow(workflowID string) (*HITLWorkflow, error)
	SaveWorkflow(workflow *HITLWorkflow) error
	DeleteWorkflow(workflowID string) error

	// HITL workflow lookups
	GetWorkflowByThread(rootPostID string) (*HITLWorkflow, error)
	GetWorkflowByAgent(cursorAgentID string) (string, error)
	SetThreadWorkflow(rootPostID string, workflowID string) error
	SetAgentWorkflow(cursorAgentID string, workflowID string) error
	DeleteAgentWorkflow(cursorAgentID string) error

	// ReviewLoop records
	GetReviewLoop(reviewLoopID string) (*ReviewLoop, error)
	SaveReviewLoop(loop *ReviewLoop) error
	DeleteReviewLoop(reviewLoopID string) error

	// ReviewLoop lookups
	GetReviewLoopByPRURL(prURL string) (*ReviewLoop, error)
	GetReviewLoopByAgent(agentRecordID string) (*ReviewLoop, error)

	// Janitor indexes
	GetAllFinishedAgentsWithPR() ([]*AgentRecord, error)
}
