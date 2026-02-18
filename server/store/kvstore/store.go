package kvstore

import (
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

// Key prefixes for the KV store.
const (
	prefixAgent        = "agent:"
	prefixThread       = "thread:"
	prefixChannel      = "channel:"
	prefixUser         = "user:"
	prefixAgentIdx     = "agentidx:"     // Index for listing active agents
	prefixUserAgentIdx = "useragentidx:" // Index for listing agents by user
	prefixPRURLIdx     = "prurlidx:"     // Index for PR URL -> agent ID lookup
	prefixBranchIdx    = "branchidx:"    // Index for branch name -> agent ID lookup
	prefixDelivery     = "ghdelivery:"   // Idempotency key for GitHub webhook deliveries
	prefixHITL         = "hitl:"         // HITL workflow records
	prefixHITLAgent    = "hitlagent:"    // Reverse index: Cursor agent ID -> workflow ID
	prefixReviewLoop   = "reviewloop:"   // ReviewLoop records
	prefixRLByPR       = "rlbypr:"       // PR URL -> ReviewLoop ID index
	prefixRLByAgent      = "rlbyagent:"    // Agent record ID -> ReviewLoop ID index
	prefixFinishedWithPR = "finishedpr:"   // Index for FINISHED agents with PrURL (janitor)
)

// hitlThreadPrefix is prepended to workflow IDs when stored in thread mappings
// to distinguish them from bare agent IDs.
const hitlThreadPrefix = "hitl:"

type store struct {
	client *pluginapi.Client
}

// isActiveStatus returns true if the agent status represents a non-terminal state.
func isActiveStatus(status string) bool {
	return status == "CREATING" || status == "RUNNING"
}

// normalizeURL strips trailing slashes for consistent index lookup.
func normalizeURL(u string) string {
	return strings.TrimRight(u, "/")
}

func NewKVStore(client *pluginapi.Client) KVStore {
	return &store{client: client}
}

func (s *store) GetAgent(cursorAgentID string) (*AgentRecord, error) {
	var record AgentRecord
	err := s.client.KV.Get(prefixAgent+cursorAgentID, &record)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get agent record")
	}
	if record.CursorAgentID == "" {
		return nil, nil // Not found
	}
	return &record, nil
}

func (s *store) SaveAgent(record *AgentRecord) error {
	_, err := s.client.KV.Set(prefixAgent+record.CursorAgentID, record)
	if err != nil {
		return errors.Wrap(err, "failed to save agent record")
	}

	// Maintain an index of active (non-terminal) agent IDs for ListActiveAgents.
	if isActiveStatus(record.Status) {
		_, err = s.client.KV.Set(prefixAgentIdx+record.CursorAgentID, record.CursorAgentID)
		if err != nil {
			return errors.Wrap(err, "failed to save agent index")
		}
	} else {
		// Terminal state: remove from active index
		_ = s.client.KV.Delete(prefixAgentIdx + record.CursorAgentID)
	}

	// Maintain a per-user agent index for GetAgentsByUser.
	if record.UserID != "" {
		key := prefixUserAgentIdx + record.UserID + ":" + record.CursorAgentID
		_, _ = s.client.KV.Set(key, record.CursorAgentID)
	}

	// Maintain PR URL index for GitHub webhook lookup.
	if record.PrURL != "" {
		_, _ = s.client.KV.Set(prefixPRURLIdx+normalizeURL(record.PrURL), record.CursorAgentID)
	}

	// Maintain branch index for GitHub webhook lookup.
	if record.TargetBranch != "" {
		_, _ = s.client.KV.Set(prefixBranchIdx+record.TargetBranch, record.CursorAgentID)
	}

	// Maintain finished-with-PR index for janitor sweep.
	if record.PrURL != "" && !isActiveStatus(record.Status) {
		_, _ = s.client.KV.Set(prefixFinishedWithPR+record.CursorAgentID, record.CursorAgentID)
	} else {
		_ = s.client.KV.Delete(prefixFinishedWithPR + record.CursorAgentID)
	}

	return nil
}

func (s *store) DeleteAgent(cursorAgentID string) error {
	// Get record first to clean up user index.
	record, _ := s.GetAgent(cursorAgentID)

	err := s.client.KV.Delete(prefixAgent + cursorAgentID)
	if err != nil {
		return errors.Wrap(err, "failed to delete agent record")
	}
	_ = s.client.KV.Delete(prefixAgentIdx + cursorAgentID)
	_ = s.client.KV.Delete(prefixFinishedWithPR + cursorAgentID)

	if record != nil && record.UserID != "" {
		_ = s.client.KV.Delete(prefixUserAgentIdx + record.UserID + ":" + cursorAgentID)
	}

	return nil
}

func (s *store) GetAgentsByUser(userID string) ([]*AgentRecord, error) {
	prefix := prefixUserAgentIdx + userID + ":"
	keys, err := s.client.KV.ListKeys(0, 1000, pluginapi.WithPrefix(prefix))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user agent keys")
	}

	var agents []*AgentRecord
	for _, key := range keys {
		agentID := strings.TrimPrefix(key, prefix)
		record, err := s.GetAgent(agentID)
		if err != nil {
			continue
		}
		if record != nil {
			agents = append(agents, record)
		}
	}
	return agents, nil
}

func (s *store) ListActiveAgents() ([]*AgentRecord, error) {
	keys, err := s.client.KV.ListKeys(0, 1000, pluginapi.WithPrefix(prefixAgentIdx))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list active agent keys")
	}

	var agents []*AgentRecord
	for _, key := range keys {
		agentID := strings.TrimPrefix(key, prefixAgentIdx)
		record, err := s.GetAgent(agentID)
		if err != nil {
			continue // Skip errored records
		}
		if record != nil {
			agents = append(agents, record)
		}
	}
	return agents, nil
}

func (s *store) GetAgentByPRURL(prURL string) (*AgentRecord, error) {
	var agentID string
	err := s.client.KV.Get(prefixPRURLIdx+normalizeURL(prURL), &agentID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get PR URL index")
	}
	if agentID == "" {
		return nil, nil
	}
	return s.GetAgent(agentID)
}

func (s *store) GetAgentByBranch(branchName string) (*AgentRecord, error) {
	var agentID string
	err := s.client.KV.Get(prefixBranchIdx+branchName, &agentID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get branch index")
	}
	if agentID == "" {
		return nil, nil
	}
	return s.GetAgent(agentID)
}

func (s *store) GetAgentIDByThread(rootPostID string) (string, error) {
	var agentID string
	err := s.client.KV.Get(prefixThread+rootPostID, &agentID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get thread mapping")
	}
	return agentID, nil
}

func (s *store) SetThreadAgent(rootPostID string, cursorAgentID string) error {
	_, err := s.client.KV.Set(prefixThread+rootPostID, cursorAgentID)
	if err != nil {
		return errors.Wrap(err, "failed to set thread mapping")
	}
	return nil
}

func (s *store) DeleteThreadAgent(rootPostID string) error {
	err := s.client.KV.Delete(prefixThread + rootPostID)
	if err != nil {
		return errors.Wrap(err, "failed to delete thread mapping")
	}
	return nil
}

func (s *store) GetChannelSettings(channelID string) (*ChannelSettings, error) {
	var settings ChannelSettings
	err := s.client.KV.Get(prefixChannel+channelID, &settings)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get channel settings")
	}
	return &settings, nil
}

func (s *store) SaveChannelSettings(channelID string, settings *ChannelSettings) error {
	_, err := s.client.KV.Set(prefixChannel+channelID, settings)
	if err != nil {
		return errors.Wrap(err, "failed to save channel settings")
	}
	return nil
}

func (s *store) GetUserSettings(userID string) (*UserSettings, error) {
	var settings UserSettings
	err := s.client.KV.Get(prefixUser+userID, &settings)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user settings")
	}
	return &settings, nil
}

func (s *store) SaveUserSettings(userID string, settings *UserSettings) error {
	_, err := s.client.KV.Set(prefixUser+userID, settings)
	if err != nil {
		return errors.Wrap(err, "failed to save user settings")
	}
	return nil
}

func (s *store) HasDeliveryBeenProcessed(deliveryID string) (bool, error) {
	var seen bool
	err := s.client.KV.Get(prefixDelivery+deliveryID, &seen)
	if err != nil {
		return false, errors.Wrap(err, "failed to check delivery")
	}
	return seen, nil
}

func (s *store) MarkDeliveryProcessed(deliveryID string) error {
	_, err := s.client.KV.Set(prefixDelivery+deliveryID, true, pluginapi.SetExpiry(24*time.Hour))
	if err != nil {
		return errors.Wrap(err, "failed to mark delivery processed")
	}
	return nil
}

func (s *store) GetWorkflow(workflowID string) (*HITLWorkflow, error) {
	var workflow HITLWorkflow
	err := s.client.KV.Get(prefixHITL+workflowID, &workflow)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get workflow")
	}
	if workflow.ID == "" {
		return nil, nil // Not found
	}
	return &workflow, nil
}

func (s *store) SaveWorkflow(workflow *HITLWorkflow) error {
	_, err := s.client.KV.Set(prefixHITL+workflow.ID, workflow)
	if err != nil {
		return errors.Wrap(err, "failed to save workflow")
	}
	return nil
}

func (s *store) DeleteWorkflow(workflowID string) error {
	err := s.client.KV.Delete(prefixHITL + workflowID)
	if err != nil {
		return errors.Wrap(err, "failed to delete workflow")
	}
	return nil
}

func (s *store) GetWorkflowByThread(rootPostID string) (*HITLWorkflow, error) {
	var value string
	err := s.client.KV.Get(prefixThread+rootPostID, &value)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get thread mapping")
	}
	if value == "" {
		return nil, nil
	}
	if !strings.HasPrefix(value, hitlThreadPrefix) {
		return nil, nil // This is a bare agent ID, not a workflow reference
	}
	workflowID := strings.TrimPrefix(value, hitlThreadPrefix)
	return s.GetWorkflow(workflowID)
}

func (s *store) GetWorkflowByAgent(cursorAgentID string) (string, error) {
	var workflowID string
	err := s.client.KV.Get(prefixHITLAgent+cursorAgentID, &workflowID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get agent-to-workflow mapping")
	}
	return workflowID, nil
}

func (s *store) SetThreadWorkflow(rootPostID string, workflowID string) error {
	_, err := s.client.KV.Set(prefixThread+rootPostID, hitlThreadPrefix+workflowID)
	if err != nil {
		return errors.Wrap(err, "failed to set thread workflow mapping")
	}
	return nil
}

func (s *store) SetAgentWorkflow(cursorAgentID string, workflowID string) error {
	_, err := s.client.KV.Set(prefixHITLAgent+cursorAgentID, workflowID)
	if err != nil {
		return errors.Wrap(err, "failed to set agent-to-workflow mapping")
	}
	return nil
}

func (s *store) DeleteAgentWorkflow(cursorAgentID string) error {
	err := s.client.KV.Delete(prefixHITLAgent + cursorAgentID)
	if err != nil {
		return errors.Wrap(err, "failed to delete agent-to-workflow mapping")
	}
	return nil
}

func (s *store) GetReviewLoop(reviewLoopID string) (*ReviewLoop, error) {
	var loop ReviewLoop
	err := s.client.KV.Get(prefixReviewLoop+reviewLoopID, &loop)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get review loop")
	}
	if loop.ID == "" {
		return nil, nil // Not found
	}
	return &loop, nil
}

func (s *store) SaveReviewLoop(loop *ReviewLoop) error {
	_, err := s.client.KV.Set(prefixReviewLoop+loop.ID, loop)
	if err != nil {
		return errors.Wrap(err, "failed to save review loop")
	}

	// Maintain PR URL -> ReviewLoop ID index.
	if loop.PRURL != "" {
		_, err = s.client.KV.Set(prefixRLByPR+normalizeURL(loop.PRURL), loop.ID)
		if err != nil {
			return errors.Wrap(err, "failed to save review loop PR URL index")
		}
	}

	// Maintain Agent Record ID -> ReviewLoop ID index.
	if loop.AgentRecordID != "" {
		_, err = s.client.KV.Set(prefixRLByAgent+loop.AgentRecordID, loop.ID)
		if err != nil {
			return errors.Wrap(err, "failed to save review loop agent index")
		}
	}

	// Remove from janitor index since a loop now exists for this agent.
	if loop.AgentRecordID != "" {
		_ = s.client.KV.Delete(prefixFinishedWithPR + loop.AgentRecordID)
	}

	return nil
}

func (s *store) DeleteReviewLoop(reviewLoopID string) error {
	// Get record first to clean up indexes.
	loop, _ := s.GetReviewLoop(reviewLoopID)

	err := s.client.KV.Delete(prefixReviewLoop + reviewLoopID)
	if err != nil {
		return errors.Wrap(err, "failed to delete review loop")
	}

	if loop != nil {
		if loop.PRURL != "" {
			_ = s.client.KV.Delete(prefixRLByPR + normalizeURL(loop.PRURL))
		}
		if loop.AgentRecordID != "" {
			_ = s.client.KV.Delete(prefixRLByAgent + loop.AgentRecordID)
		}
	}

	return nil
}

func (s *store) GetReviewLoopByPRURL(prURL string) (*ReviewLoop, error) {
	var reviewLoopID string
	err := s.client.KV.Get(prefixRLByPR+normalizeURL(prURL), &reviewLoopID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get review loop PR URL index")
	}
	if reviewLoopID == "" {
		return nil, nil
	}
	return s.GetReviewLoop(reviewLoopID)
}

func (s *store) GetReviewLoopByAgent(agentRecordID string) (*ReviewLoop, error) {
	var reviewLoopID string
	err := s.client.KV.Get(prefixRLByAgent+agentRecordID, &reviewLoopID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get review loop agent index")
	}
	if reviewLoopID == "" {
		return nil, nil
	}
	return s.GetReviewLoop(reviewLoopID)
}

func (s *store) GetAllFinishedAgentsWithPR() ([]*AgentRecord, error) {
	keys, err := s.client.KV.ListKeys(0, 1000, pluginapi.WithPrefix(prefixFinishedWithPR))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list finished-with-PR keys")
	}
	var agents []*AgentRecord
	for _, key := range keys {
		agentID := strings.TrimPrefix(key, prefixFinishedWithPR)
		record, err := s.GetAgent(agentID)
		if err != nil || record == nil {
			_ = s.client.KV.Delete(key) // Clean up orphaned index entry.
			continue
		}
		agents = append(agents, record)
	}
	return agents, nil
}
