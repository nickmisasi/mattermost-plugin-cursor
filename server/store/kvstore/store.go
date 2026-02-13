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
)

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
