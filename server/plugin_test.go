package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// --- Auth middleware tests ---

func TestServeHTTPUnauthorized(t *testing.T) {
	plugin := Plugin{}
	plugin.router = plugin.initRouter()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/health", nil)
	// No Mattermost-User-ID header

	plugin.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, result.StatusCode)
}

func TestServeHTTPHealthRoute_NonAdmin(t *testing.T) {
	plugin := Plugin{}
	plugin.router = plugin.initRouter()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/health", nil)
	r.Header.Set("Mattermost-User-ID", "test-user-id")

	plugin.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()

	// Without p.client initialized, isSystemAdmin will fail and return Forbidden
	assert.Equal(t, http.StatusForbidden, result.StatusCode)
}

func TestServeHTTPNotFound(t *testing.T) {
	plugin := Plugin{}
	plugin.router = plugin.initRouter()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	r.Header.Set("Mattermost-User-ID", "test-user-id")

	plugin.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, result.StatusCode)
}

// --- Configuration validation tests ---

func TestConfigurationIsValid(t *testing.T) {
	tests := []struct {
		name    string
		cfg     configuration
		wantErr string
	}{
		{
			name:    "missing API key",
			cfg:     configuration{PollIntervalSeconds: 30},
			wantErr: "cursor API Key is required",
		},
		{
			name: "valid config",
			cfg: configuration{
				CursorAPIKey:        "cur_test123",
				PollIntervalSeconds: 30,
			},
		},
		{
			name: "valid config with all fields",
			cfg: configuration{
				CursorAPIKey:        "cur_abc123",
				DefaultRepository:   "mattermost/mattermost",
				DefaultBranch:       "main",
				DefaultModel:        "auto",
				PollIntervalSeconds: 30,
			},
		},
		{
			name: "poll interval too low",
			cfg: configuration{
				CursorAPIKey:        "cur_test123",
				PollIntervalSeconds: 5,
			},
			wantErr: "poll interval must be at least 10 seconds",
		},
		{
			name: "poll interval at minimum",
			cfg: configuration{
				CursorAPIKey:        "cur_test123",
				PollIntervalSeconds: 10,
			},
		},
		{
			name: "invalid repo format - no slash",
			cfg: configuration{
				CursorAPIKey:        "cur_abc123",
				DefaultRepository:   "justrepo",
				PollIntervalSeconds: 30,
			},
			wantErr: "must be in 'owner/repo' format",
		},
		{
			name: "invalid repo format - empty owner",
			cfg: configuration{
				CursorAPIKey:        "cur_abc123",
				DefaultRepository:   "/repo",
				PollIntervalSeconds: 30,
			},
			wantErr: "must be in 'owner/repo' format",
		},
		{
			name: "invalid repo format - empty repo",
			cfg: configuration{
				CursorAPIKey:        "cur_abc123",
				DefaultRepository:   "owner/",
				PollIntervalSeconds: 30,
			},
			wantErr: "must be in 'owner/repo' format",
		},
		{
			name: "valid config with repo",
			cfg: configuration{
				CursorAPIKey:        "cur_abc123",
				DefaultRepository:   "mattermost/mattermost",
				PollIntervalSeconds: 30,
			},
		},
		{
			name: "empty repo is allowed",
			cfg: configuration{
				CursorAPIKey:        "cur_abc123",
				PollIntervalSeconds: 30,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.IsValid()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfigurationGetPollInterval(t *testing.T) {
	tests := []struct {
		name     string
		cfg      configuration
		expected int
	}{
		{
			name:     "zero defaults to 30",
			cfg:      configuration{},
			expected: 30,
		},
		{
			name:     "valid value",
			cfg:      configuration{PollIntervalSeconds: 60},
			expected: 60,
		},
		{
			name:     "minimum value",
			cfg:      configuration{PollIntervalSeconds: 10},
			expected: 10,
		},
		{
			name:     "below minimum defaults to 30",
			cfg:      configuration{PollIntervalSeconds: 5},
			expected: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cfg.GetPollInterval())
		})
	}
}

func TestConfigurationClone(t *testing.T) {
	cfg := &configuration{
		CursorAPIKey:  "test-key",
		DefaultBranch: "main",
	}
	clone := cfg.Clone()
	require.NotSame(t, cfg, clone)
	assert.Equal(t, cfg.CursorAPIKey, clone.CursorAPIKey)
	assert.Equal(t, cfg.DefaultBranch, clone.DefaultBranch)
}

// --- Health endpoint tests ---

func TestHealthCheck_Unauthenticated(t *testing.T) {
	plugin := Plugin{}
	plugin.configuration = &configuration{CursorAPIKey: ""}
	plugin.router = plugin.initRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/health", nil)
	// No user header -> should get 401

	plugin.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	bodyBytes, _ := io.ReadAll(result.Body)
	assert.Equal(t, http.StatusUnauthorized, result.StatusCode)
	assert.Contains(t, string(bodyBytes), "Not authorized")
}

func TestHealthCheck_Healthy(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	// System admin check.
	api.On("GetUser", "admin-user").Return(&model.User{
		Id:    "admin-user",
		Roles: "system_admin system_user",
	}, nil).Maybe()

	cursorClient := &mockCursorClient{}
	store := &mockKVStore{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.cursorClient = cursorClient
	p.kvstore = store
	p.configuration = &configuration{
		CursorAPIKey:        "test-key",
		PollIntervalSeconds: 30,
	}
	p.router = p.initRouter()

	// Mock GetMe success.
	cursorClient.On("GetMe", mock.Anything).Return(&cursor.APIKeyInfo{
		UserEmail:  "test@example.com",
		APIKeyName: "test-key",
	}, nil)

	// Mock active agents.
	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{
		{CursorAgentID: "agent-1", Status: "RUNNING"},
		{CursorAgentID: "agent-2", Status: "CREATING"},
		{CursorAgentID: "agent-3", Status: "RUNNING"},
	}, nil)

	rr := doHealthRequest(p, "admin-user")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.True(t, resp.Healthy)
	assert.True(t, resp.CursorAPI.OK)
	assert.True(t, resp.Configuration.OK)
	assert.Equal(t, 3, resp.ActiveAgentCount)
	assert.Equal(t, "0.1.0", resp.PluginVersion)
}

func TestHealthCheck_NoAPIKey(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	api.On("GetUser", "admin-user").Return(&model.User{
		Id:    "admin-user",
		Roles: "system_admin system_user",
	}, nil).Maybe()

	store := &mockKVStore{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.kvstore = store
	p.configuration = &configuration{
		PollIntervalSeconds: 30,
	}
	p.router = p.initRouter()

	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{}, nil)

	rr := doHealthRequest(p, "admin-user")
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.False(t, resp.Healthy)
	assert.False(t, resp.CursorAPI.OK)
	assert.Contains(t, resp.CursorAPI.Message, "Cursor API key not configured")
	assert.False(t, resp.Configuration.OK)
	assert.Contains(t, resp.Configuration.Message, "cursor API Key is required")
}

func TestHealthCheck_BadAPIKey(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	api.On("GetUser", "admin-user").Return(&model.User{
		Id:    "admin-user",
		Roles: "system_admin system_user",
	}, nil).Maybe()

	cursorClient := &mockCursorClient{}
	store := &mockKVStore{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.cursorClient = cursorClient
	p.kvstore = store
	p.configuration = &configuration{
		CursorAPIKey:        "bad-key",
		PollIntervalSeconds: 30,
	}
	p.router = p.initRouter()

	cursorClient.On("GetMe", mock.Anything).Return(nil, fmt.Errorf("401 Unauthorized"))
	store.On("ListActiveAgents").Return([]*kvstore.AgentRecord{}, nil)

	rr := doHealthRequest(p, "admin-user")
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.False(t, resp.Healthy)
	assert.False(t, resp.CursorAPI.OK)
	assert.Contains(t, resp.CursorAPI.Message, "Cursor API unreachable")
	assert.True(t, resp.Configuration.OK)
}

func TestHealthCheck_StoreError(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	api.On("GetUser", "admin-user").Return(&model.User{
		Id:    "admin-user",
		Roles: "system_admin system_user",
	}, nil).Maybe()

	cursorClient := &mockCursorClient{}
	store := &mockKVStore{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.cursorClient = cursorClient
	p.kvstore = store
	p.configuration = &configuration{
		CursorAPIKey:        "test-key",
		PollIntervalSeconds: 30,
	}
	p.router = p.initRouter()

	cursorClient.On("GetMe", mock.Anything).Return(&cursor.APIKeyInfo{
		UserEmail: "test@example.com",
	}, nil)
	store.On("ListActiveAgents").Return(nil, fmt.Errorf("KV store error"))

	rr := doHealthRequest(p, "admin-user")
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.True(t, resp.Healthy) // Config OK and API OK
	assert.Equal(t, -1, resp.ActiveAgentCount)
}

func TestHealthCheck_NonAdmin(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	// Non-admin user.
	api.On("GetUser", "regular-user").Return(&model.User{
		Id:    "regular-user",
		Roles: "system_user",
	}, nil).Maybe()

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.configuration = &configuration{
		CursorAPIKey:        "test-key",
		PollIntervalSeconds: 30,
	}
	p.router = p.initRouter()

	rr := doHealthRequest(p, "regular-user")
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// doHealthRequest sends a GET request to the admin health endpoint.
func doHealthRequest(p *Plugin, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/health", nil)
	if userID != "" {
		req.Header.Set("Mattermost-User-ID", userID)
	}

	rr := httptest.NewRecorder()
	p.ServeHTTP(nil, rr, req)
	return rr
}
