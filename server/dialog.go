package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

var repoFormatRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`)

func (p *Plugin) handleSettingsDialogSubmission(w http.ResponseWriter, r *http.Request) {
	var request model.SubmitDialogRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	parts := strings.SplitN(request.State, "|", 2)
	if len(parts) != 2 {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	channelID := parts[0]
	userID := parts[1]

	if request.UserId != userID {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	// Validate repo format if non-empty.
	dialogErrors := make(map[string]string)

	channelRepo, _ := request.Submission["channel_default_repo"].(string)
	channelBranch, _ := request.Submission["channel_default_branch"].(string)
	userRepo, _ := request.Submission["user_default_repo"].(string)
	userBranch, _ := request.Submission["user_default_branch"].(string)
	userModel, _ := request.Submission["user_default_model"].(string)

	if channelRepo != "" && !repoFormatRe.MatchString(channelRepo) {
		dialogErrors["channel_default_repo"] = "Must be in owner/repo format (e.g., mattermost/mattermost)"
	}
	if userRepo != "" && !repoFormatRe.MatchString(userRepo) {
		dialogErrors["user_default_repo"] = "Must be in owner/repo format (e.g., mattermost/mattermost)"
	}

	if len(dialogErrors) > 0 {
		w.Header().Set("Content-Type", "application/json")
		resp := model.SubmitDialogResponse{Errors: dialogErrors}
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Save channel settings.
	err := p.kvstore.SaveChannelSettings(channelID, &kvstore.ChannelSettings{
		DefaultRepository: channelRepo,
		DefaultBranch:     channelBranch,
	})
	if err != nil {
		p.API.LogError("Failed to save channel settings", "error", err.Error())
	}

	// Save user settings.
	err = p.kvstore.SaveUserSettings(userID, &kvstore.UserSettings{
		DefaultRepository: userRepo,
		DefaultBranch:     userBranch,
		DefaultModel:      userModel,
	})
	if err != nil {
		p.API.LogError("Failed to save user settings", "error", err.Error())
	}

	// Send confirmation ephemeral post.
	_ = p.API.SendEphemeralPost(userID, &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: channelID,
		Message:   ":white_check_mark: Cursor settings saved successfully.",
	})

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}
