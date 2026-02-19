package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

func setupDialogTestPlugin(t *testing.T) (*Plugin, *plugintest.API, *mockKVStore) {
	t.Helper()

	api := &plugintest.API{}
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

	store := &mockKVStore{}

	p := &Plugin{}
	p.SetAPI(api)
	p.client = pluginapi.NewClient(api, nil)
	p.kvstore = store
	p.botUserID = "bot-user-id"
	p.router = p.initRouter()

	return p, api, store
}

func TestSettingsDialog_ValidSubmission(t *testing.T) {
	p, api, store := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId: "user-1",
		State:  "ch-1|user-1",
		Submission: map[string]any{
			"channel_default_repo":   "org/repo",
			"channel_default_branch": "main",
			"user_default_repo":      "user/personal",
			"user_default_branch":    "develop",
			"user_default_model":     "claude-sonnet",
		},
	}

	store.On("SaveChannelSettings", "ch-1", &kvstore.ChannelSettings{
		DefaultRepository: "org/repo",
		DefaultBranch:     "main",
	}).Return(nil)
	store.On("SaveUserSettings", "user-1", &kvstore.UserSettings{
		DefaultRepository: "user/personal",
		DefaultBranch:     "develop",
		DefaultModel:      "claude-sonnet",
	}).Return(nil)
	api.On("SendEphemeralPost", "user-1", mock.MatchedBy(func(p *model.Post) bool {
		return p.ChannelId == "ch-1" && p.Message == ":white_check_mark: Cursor settings saved successfully."
	})).Return(&model.Post{})

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusOK, result.StatusCode)

	respBody, _ := io.ReadAll(result.Body)
	assert.Equal(t, "{}", string(respBody))

	store.AssertExpectations(t)
}

func TestSettingsDialog_InvalidRepo(t *testing.T) {
	p, _, _ := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId: "user-1",
		State:  "ch-1|user-1",
		Submission: map[string]any{
			"channel_default_repo":   "badformat",
			"channel_default_branch": "",
			"user_default_repo":      "",
			"user_default_branch":    "",
			"user_default_model":     "",
		},
	}

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusOK, result.StatusCode)

	var resp model.SubmitDialogResponse
	_ = json.NewDecoder(result.Body).Decode(&resp)
	assert.Contains(t, resp.Errors, "channel_default_repo")
	assert.Contains(t, resp.Errors["channel_default_repo"], "owner/repo format")
}

func TestSettingsDialog_EmptySubmission(t *testing.T) {
	p, api, store := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId: "user-1",
		State:  "ch-1|user-1",
		Submission: map[string]any{
			"channel_default_repo":   "",
			"channel_default_branch": "",
			"user_default_repo":      "",
			"user_default_branch":    "",
			"user_default_model":     "",
		},
	}

	store.On("SaveChannelSettings", "ch-1", &kvstore.ChannelSettings{}).Return(nil)
	store.On("SaveUserSettings", "user-1", &kvstore.UserSettings{}).Return(nil)
	api.On("SendEphemeralPost", "user-1", mock.Anything).Return(&model.Post{})

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusOK, result.StatusCode)
	store.AssertExpectations(t)
}

func TestSettingsDialog_InvalidState(t *testing.T) {
	p, _, _ := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId:     "user-1",
		State:      "invalid-state-no-pipe",
		Submission: map[string]any{},
	}

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, result.StatusCode)
}

func TestSettingsDialog_UserMismatch(t *testing.T) {
	p, _, _ := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId:     "attacker-user",
		State:      "ch-1|user-1",
		Submission: map[string]any{},
	}

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "attacker-user")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusForbidden, result.StatusCode)
}

func TestSettingsDialog_InvalidUserRepo(t *testing.T) {
	p, _, _ := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId: "user-1",
		State:  "ch-1|user-1",
		Submission: map[string]any{
			"channel_default_repo": "org/repo",
			"user_default_repo":    "not-a-valid-repo-format!!",
		},
	}

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()

	var resp model.SubmitDialogResponse
	_ = json.NewDecoder(result.Body).Decode(&resp)
	assert.Contains(t, resp.Errors, "user_default_repo")
}

func TestSettingsDialog_SavesHITLSettings(t *testing.T) {
	p, api, store := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId: "user-1",
		State:  "ch-1|user-1",
		Submission: map[string]any{
			"channel_default_repo":       "",
			"channel_default_branch":     "",
			"user_default_repo":          "org/repo",
			"user_default_branch":        "main",
			"user_default_model":         "auto",
			"user_enable_context_review": true,
			"user_enable_plan_loop":      false,
		},
	}

	store.On("SaveChannelSettings", "ch-1", mock.Anything).Return(nil)
	store.On("SaveUserSettings", "user-1", mock.MatchedBy(func(s *kvstore.UserSettings) bool {
		return s.DefaultRepository == "org/repo" &&
			s.EnableContextReview != nil && *s.EnableContextReview == true &&
			s.EnablePlanLoop != nil && *s.EnablePlanLoop == false
	})).Return(nil)
	api.On("SendEphemeralPost", "user-1", mock.Anything).Return(&model.Post{})

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusOK, result.StatusCode)

	store.AssertExpectations(t)
}

func TestSettingsDialog_SavesHITLSettings_StringCoercion(t *testing.T) {
	p, api, store := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId: "user-1",
		State:  "ch-1|user-1",
		Submission: map[string]any{
			"channel_default_repo":       "",
			"channel_default_branch":     "",
			"user_default_repo":          "org/repo",
			"user_default_branch":        "main",
			"user_default_model":         "auto",
			"user_enable_context_review": "true",
			"user_enable_plan_loop":      "false",
		},
	}

	store.On("SaveChannelSettings", "ch-1", mock.Anything).Return(nil)
	store.On("SaveUserSettings", "user-1", mock.MatchedBy(func(s *kvstore.UserSettings) bool {
		return s.DefaultRepository == "org/repo" &&
			s.EnableContextReview != nil && *s.EnableContextReview == true &&
			s.EnablePlanLoop != nil && *s.EnablePlanLoop == false
	})).Return(nil)
	api.On("SendEphemeralPost", "user-1", mock.Anything).Return(&model.Post{})

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusOK, result.StatusCode)

	store.AssertExpectations(t)
}

func TestSettingsDialog_NilHITLSettings_NoOverride(t *testing.T) {
	p, api, store := setupDialogTestPlugin(t)

	submission := model.SubmitDialogRequest{
		UserId: "user-1",
		State:  "ch-1|user-1",
		Submission: map[string]any{
			"channel_default_repo":   "",
			"channel_default_branch": "",
			"user_default_repo":      "",
			"user_default_branch":    "",
			"user_default_model":     "",
			// No HITL fields submitted -- should result in nil pointers.
		},
	}

	store.On("SaveChannelSettings", "ch-1", mock.Anything).Return(nil)
	store.On("SaveUserSettings", "user-1", mock.MatchedBy(func(s *kvstore.UserSettings) bool {
		return s.EnableContextReview == nil && s.EnablePlanLoop == nil
	})).Return(nil)
	api.On("SendEphemeralPost", "user-1", mock.Anything).Return(&model.Post{})

	body, _ := json.Marshal(submission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/dialog/settings", bytes.NewReader(body))
	r.Header.Set("Mattermost-User-ID", "user-1")

	p.ServeHTTP(nil, w, r)

	result := w.Result()
	defer func() { _ = result.Body.Close() }()
	assert.Equal(t, http.StatusOK, result.StatusCode)

	store.AssertExpectations(t)
}

func TestParseOptionalDialogBool(t *testing.T) {
	t.Run("bool true", func(t *testing.T) {
		value, ok := parseOptionalDialogBool(true)
		assert.True(t, ok)
		if assert.NotNil(t, value) {
			assert.True(t, *value)
		}
	})

	t.Run("bool false", func(t *testing.T) {
		value, ok := parseOptionalDialogBool(false)
		assert.True(t, ok)
		if assert.NotNil(t, value) {
			assert.False(t, *value)
		}
	})

	t.Run("string true", func(t *testing.T) {
		value, ok := parseOptionalDialogBool("true")
		assert.True(t, ok)
		if assert.NotNil(t, value) {
			assert.True(t, *value)
		}
	})

	t.Run("string false", func(t *testing.T) {
		value, ok := parseOptionalDialogBool("false")
		assert.True(t, ok)
		if assert.NotNil(t, value) {
			assert.False(t, *value)
		}
	})

	t.Run("blank string means nil override", func(t *testing.T) {
		value, ok := parseOptionalDialogBool("   ")
		assert.True(t, ok)
		assert.Nil(t, value)
	})

	t.Run("invalid type", func(t *testing.T) {
		value, ok := parseOptionalDialogBool(1)
		assert.False(t, ok)
		assert.Nil(t, value)
	})

	t.Run("invalid string", func(t *testing.T) {
		value, ok := parseOptionalDialogBool("maybe")
		assert.False(t, ok)
		assert.Nil(t, value)
	})
}
