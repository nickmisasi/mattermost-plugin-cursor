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
	Model          string `json:"model"`
	Summary        string `json:"summary"`
	CreatedAt      int64  `json:"createdAt"` // Unix millis
	UpdatedAt      int64  `json:"updatedAt"` // Unix millis
}

// ChannelSettings stores per-channel defaults.
type ChannelSettings struct {
	DefaultRepository string `json:"defaultRepository"`
	DefaultBranch     string `json:"defaultBranch"`
}

// UserSettings stores per-user defaults.
type UserSettings struct {
	DefaultRepository string `json:"defaultRepository"`
	DefaultBranch     string `json:"defaultBranch"`
	DefaultModel      string `json:"defaultModel"`
}

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
}
