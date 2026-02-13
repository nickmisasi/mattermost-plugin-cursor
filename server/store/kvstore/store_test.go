package kvstore

import (
	"encoding/json"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupStore(t *testing.T) (*store, *plugintest.API) {
	t.Helper()
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	client := pluginapi.NewClient(api, nil)
	s := &store{client: client}
	return s, api
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// mockKVSet sets up the KVSetWithOptions mock for a Set call.
func mockKVSet(api *plugintest.API, key string, value []byte) {
	api.On("KVSetWithOptions", key, value, model.PluginKVSetOptions{}).Return(true, nil)
}

// mockKVDelete sets up the KVSetWithOptions mock for a Delete call (pluginapi.Delete calls Set(key, nil)).
func mockKVDelete(api *plugintest.API, key string) {
	api.On("KVSetWithOptions", key, []byte(nil), model.PluginKVSetOptions{}).Return(true, nil)
}

func TestSaveAndGetAgent(t *testing.T) {
	s, api := setupStore(t)

	record := &AgentRecord{
		CursorAgentID: "agent-123",
		PostID:        "post-1",
		ChannelID:     "ch-1",
		UserID:        "user-1",
		Status:        "RUNNING",
		Repository:    "org/repo",
	}

	mockKVSet(api, prefixAgent+"agent-123", mustJSON(t, record))
	mockKVSet(api, prefixAgentIdx+"agent-123", mustJSON(t, "agent-123"))
	mockKVSet(api, prefixUserAgentIdx+"user-1:agent-123", mustJSON(t, "agent-123"))

	err := s.SaveAgent(record)
	require.NoError(t, err)

	api.On("KVGet", prefixAgent+"agent-123").Return(mustJSON(t, record), nil)

	got, err := s.GetAgent("agent-123")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "agent-123", got.CursorAgentID)
	assert.Equal(t, "post-1", got.PostID)
	assert.Equal(t, "RUNNING", got.Status)
	assert.Equal(t, "org/repo", got.Repository)
	api.AssertExpectations(t)
}

func TestSaveAgentCreatingStatus(t *testing.T) {
	s, api := setupStore(t)

	record := &AgentRecord{
		CursorAgentID: "agent-new",
		Status:        "CREATING",
	}

	mockKVSet(api, prefixAgent+"agent-new", mustJSON(t, record))
	mockKVSet(api, prefixAgentIdx+"agent-new", mustJSON(t, "agent-new"))

	err := s.SaveAgent(record)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestSaveAgentTerminalStatusRemovesIndex(t *testing.T) {
	for _, status := range []string{"FINISHED", "FAILED", "STOPPED"} {
		t.Run(status, func(t *testing.T) {
			s, api := setupStore(t)

			record := &AgentRecord{
				CursorAgentID: "agent-terminal",
				Status:        status,
			}

			mockKVSet(api, prefixAgent+"agent-terminal", mustJSON(t, record))
			mockKVDelete(api, prefixAgentIdx+"agent-terminal")

			err := s.SaveAgent(record)
			require.NoError(t, err)
			api.AssertExpectations(t)
		})
	}
}

func TestDeleteAgent(t *testing.T) {
	s, api := setupStore(t)

	// GetAgent called first to find UserID for user index cleanup
	api.On("KVGet", prefixAgent+"agent-del").Return([]byte(nil), nil)
	mockKVDelete(api, prefixAgent+"agent-del")
	mockKVDelete(api, prefixAgentIdx+"agent-del")

	err := s.DeleteAgent("agent-del")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestDeleteAgentWithUserIndex(t *testing.T) {
	s, api := setupStore(t)

	record := &AgentRecord{
		CursorAgentID: "agent-del-user",
		UserID:        "user-1",
		Status:        "RUNNING",
	}

	api.On("KVGet", prefixAgent+"agent-del-user").Return(mustJSON(t, record), nil)
	mockKVDelete(api, prefixAgent+"agent-del-user")
	mockKVDelete(api, prefixAgentIdx+"agent-del-user")
	mockKVDelete(api, prefixUserAgentIdx+"user-1:agent-del-user")

	err := s.DeleteAgent("agent-del-user")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestGetNonExistentAgent(t *testing.T) {
	s, api := setupStore(t)

	api.On("KVGet", prefixAgent+"nonexistent").Return([]byte(nil), nil)

	got, err := s.GetAgent("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
	api.AssertExpectations(t)
}

func TestGetAgentError(t *testing.T) {
	s, api := setupStore(t)

	api.On("KVGet", prefixAgent+"err-agent").Return([]byte(nil), model.NewAppError("KVGet", "test", nil, "error", 500))

	got, err := s.GetAgent("err-agent")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "failed to get agent record")
	api.AssertExpectations(t)
}

func TestListActiveAgents(t *testing.T) {
	s, api := setupStore(t)

	agent1 := &AgentRecord{CursorAgentID: "a1", Status: "RUNNING"}
	agent2 := &AgentRecord{CursorAgentID: "a2", Status: "CREATING"}

	api.On("KVList", 0, 1000).Return([]string{
		prefixAgentIdx + "a1",
		prefixAgentIdx + "a2",
	}, nil)
	api.On("KVGet", prefixAgent+"a1").Return(mustJSON(t, agent1), nil)
	api.On("KVGet", prefixAgent+"a2").Return(mustJSON(t, agent2), nil)

	agents, err := s.ListActiveAgents()
	require.NoError(t, err)
	require.Len(t, agents, 2)
	assert.Equal(t, "a1", agents[0].CursorAgentID)
	assert.Equal(t, "a2", agents[1].CursorAgentID)
	api.AssertExpectations(t)
}

func TestListActiveAgentsSkipsErroredRecords(t *testing.T) {
	s, api := setupStore(t)

	agent2 := &AgentRecord{CursorAgentID: "a2", Status: "RUNNING"}

	api.On("KVList", 0, 1000).Return([]string{
		prefixAgentIdx + "a1",
		prefixAgentIdx + "a2",
	}, nil)
	api.On("KVGet", prefixAgent+"a1").Return([]byte(nil), model.NewAppError("KVGet", "test", nil, "error", 500))
	api.On("KVGet", prefixAgent+"a2").Return(mustJSON(t, agent2), nil)

	agents, err := s.ListActiveAgents()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "a2", agents[0].CursorAgentID)
	api.AssertExpectations(t)
}

func TestListActiveAgentsEmpty(t *testing.T) {
	s, api := setupStore(t)

	api.On("KVList", 0, 1000).Return([]string{}, nil)

	agents, err := s.ListActiveAgents()
	require.NoError(t, err)
	assert.Empty(t, agents)
	api.AssertExpectations(t)
}

func TestGetAgentIDByThread(t *testing.T) {
	s, api := setupStore(t)

	api.On("KVGet", prefixThread+"root-post-1").Return(mustJSON(t, "agent-123"), nil)

	agentID, err := s.GetAgentIDByThread("root-post-1")
	require.NoError(t, err)
	assert.Equal(t, "agent-123", agentID)
	api.AssertExpectations(t)
}

func TestGetAgentIDByThreadNotFound(t *testing.T) {
	s, api := setupStore(t)

	api.On("KVGet", prefixThread+"nonexistent").Return([]byte(nil), nil)

	agentID, err := s.GetAgentIDByThread("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, agentID)
	api.AssertExpectations(t)
}

func TestSetAndDeleteThreadAgent(t *testing.T) {
	s, api := setupStore(t)

	mockKVSet(api, prefixThread+"root-1", mustJSON(t, "agent-1"))

	err := s.SetThreadAgent("root-1", "agent-1")
	require.NoError(t, err)

	mockKVDelete(api, prefixThread+"root-1")

	err = s.DeleteThreadAgent("root-1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestChannelSettingsCRUD(t *testing.T) {
	s, api := setupStore(t)

	settings := &ChannelSettings{
		DefaultRepository: "org/repo",
		DefaultBranch:     "main",
	}

	mockKVSet(api, prefixChannel+"ch-1", mustJSON(t, settings))

	err := s.SaveChannelSettings("ch-1", settings)
	require.NoError(t, err)

	api.On("KVGet", prefixChannel+"ch-1").Return(mustJSON(t, settings), nil)

	got, err := s.GetChannelSettings("ch-1")
	require.NoError(t, err)
	assert.Equal(t, "org/repo", got.DefaultRepository)
	assert.Equal(t, "main", got.DefaultBranch)
	api.AssertExpectations(t)
}

func TestUserSettingsCRUD(t *testing.T) {
	s, api := setupStore(t)

	settings := &UserSettings{
		DefaultRepository: "user/repo",
		DefaultBranch:     "develop",
		DefaultModel:      "claude-sonnet",
	}

	mockKVSet(api, prefixUser+"user-1", mustJSON(t, settings))

	err := s.SaveUserSettings("user-1", settings)
	require.NoError(t, err)

	api.On("KVGet", prefixUser+"user-1").Return(mustJSON(t, settings), nil)

	got, err := s.GetUserSettings("user-1")
	require.NoError(t, err)
	assert.Equal(t, "user/repo", got.DefaultRepository)
	assert.Equal(t, "develop", got.DefaultBranch)
	assert.Equal(t, "claude-sonnet", got.DefaultModel)
	api.AssertExpectations(t)
}

func TestIsActiveStatus(t *testing.T) {
	assert.True(t, isActiveStatus("CREATING"))
	assert.True(t, isActiveStatus("RUNNING"))
	assert.False(t, isActiveStatus("FINISHED"))
	assert.False(t, isActiveStatus("FAILED"))
	assert.False(t, isActiveStatus("STOPPED"))
	assert.False(t, isActiveStatus(""))
	assert.False(t, isActiveStatus("UNKNOWN"))
}
