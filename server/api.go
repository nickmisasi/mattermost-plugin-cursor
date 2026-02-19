package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/mattermost/mattermost-plugin-cursor/server/attachments"
	"github.com/mattermost/mattermost-plugin-cursor/server/cursor"
	"github.com/mattermost/mattermost-plugin-cursor/server/store/kvstore"
)

// initRouter initializes the HTTP router for the plugin.
func (p *Plugin) initRouter() *mux.Router {
	router := mux.NewRouter()
	router.Use(apiMetricsMiddleware)

	// GitHub webhook endpoint -- NO auth middleware (uses HMAC signature verification).
	router.HandleFunc("/api/v1/webhooks/github", p.handleGitHubWebhook).Methods(http.MethodPost)

	// All other API routes require a logged-in Mattermost user.
	authedRouter := router.PathPrefix("/api/v1").Subrouter()
	authedRouter.Use(p.MattermostAuthorizationRequired)

	// Dialog submission endpoint for /cursor settings.
	authedRouter.HandleFunc("/dialog/settings", p.handleSettingsDialogSubmission).Methods(http.MethodPost)

	// HITL action button handler (Phase 2).
	authedRouter.HandleFunc("/actions/hitl-response", p.handleHITLResponse).Methods(http.MethodPost)

	// Phase 4: REST endpoints for the webapp frontend.
	authedRouter.HandleFunc("/agents", p.handleGetAgents).Methods(http.MethodGet)
	authedRouter.HandleFunc("/agents/{id}", p.handleGetAgent).Methods(http.MethodGet)
	authedRouter.HandleFunc("/agents/{id}/followup", p.handleAddFollowup).Methods(http.MethodPost)
	authedRouter.HandleFunc("/agents/{id}", p.handleCancelAgent).Methods(http.MethodDelete)
	authedRouter.HandleFunc("/agents/{id}/archive", p.handleArchiveAgent).Methods(http.MethodPost)
	authedRouter.HandleFunc("/agents/{id}/unarchive", p.handleUnarchiveAgent).Methods(http.MethodPost)

	// Phase 5: Workflow detail endpoint for the webapp.
	authedRouter.HandleFunc("/workflows/{id}", p.handleGetWorkflow).Methods(http.MethodGet)

	// Admin-only routes.
	adminRouter := authedRouter.PathPrefix("/admin").Subrouter()
	adminRouter.Use(p.RequireSystemAdmin)
	adminRouter.HandleFunc("/health", p.handleHealthCheck).Methods(http.MethodGet)
	adminRouter.HandleFunc("/metrics", p.handleGetMetrics).Methods(http.MethodGet)

	return router
}

func (p *Plugin) MattermostAuthorizationRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireSystemAdmin is middleware that rejects requests from non-admin users.
func (p *Plugin) RequireSystemAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		if !p.isSystemAdmin(userID) {
			http.Error(w, "Forbidden: system admin required", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HealthResponse is the JSON response from the health check endpoint.
type HealthResponse struct {
	Healthy          bool         `json:"healthy"`
	CursorAPI        HealthStatus `json:"cursor_api"`
	ActiveAgentCount int          `json:"active_agent_count"`
	Configuration    HealthStatus `json:"configuration"`
	PluginVersion    string       `json:"plugin_version"`
}

// HealthStatus represents the health of a single subsystem.
type HealthStatus struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func (p *Plugin) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()

	response := HealthResponse{
		PluginVersion: "0.1.0",
	}

	// 1. Validate configuration.
	if err := config.IsValid(); err != nil {
		response.Configuration = HealthStatus{OK: false, Message: err.Error()}
	} else {
		response.Configuration = HealthStatus{OK: true}
	}

	// 2. Test Cursor API connectivity.
	if config.CursorAPIKey == "" {
		response.CursorAPI = HealthStatus{OK: false, Message: "Cursor API key not configured"}
	} else {
		cursorClient := p.getCursorClient()
		if cursorClient == nil {
			response.CursorAPI = HealthStatus{OK: false, Message: "Cursor client not initialized"}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_, err := cursorClient.GetMe(ctx)
			if err != nil {
				response.CursorAPI = HealthStatus{OK: false, Message: fmt.Sprintf("Cursor API unreachable: %s", err.Error())}
			} else {
				response.CursorAPI = HealthStatus{OK: true}
			}
		}
	}

	// 3. Count active agents.
	if p.kvstore != nil {
		activeAgents, err := p.kvstore.ListActiveAgents()
		if err != nil {
			p.API.LogError("Failed to list active agents for health check", "error", err.Error())
			response.ActiveAgentCount = -1 // Indicates error.
		} else {
			response.ActiveAgentCount = len(activeAgents)
		}
	}

	// 4. Overall health: healthy only if config is valid AND Cursor API is reachable.
	response.Healthy = response.Configuration.OK && response.CursorAPI.OK

	// 5. Write response.
	w.Header().Set("Content-Type", "application/json")
	if !response.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		p.API.LogError("Failed to encode health response", "error", err.Error())
	}
}

// isSystemAdmin checks if the user is a system admin.
func (p *Plugin) isSystemAdmin(userID string) bool {
	if p.client == nil {
		return false
	}
	user, err := p.client.User.Get(userID)
	if err != nil {
		return false
	}
	return user.IsSystemAdmin()
}

// --- Phase 4: REST API handlers for webapp frontend ---

// AgentResponse is the JSON representation of an agent for the webapp.
type AgentResponse struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	Repository         string `json:"repository"`
	Branch             string `json:"branch"`
	Prompt             string `json:"prompt"`
	Description        string `json:"description"`
	PrURL              string `json:"pr_url"`
	CursorURL          string `json:"cursor_url"`
	ChannelID          string `json:"channel_id"`
	PostID             string `json:"post_id"`
	RootPostID         string `json:"root_post_id"`
	Summary            string `json:"summary"`
	Model              string `json:"model"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
	Archived           bool   `json:"archived,omitempty"`
	WorkflowID         string `json:"workflow_id,omitempty"`
	WorkflowPhase      string `json:"workflow_phase,omitempty"`
	PlanIterationCount int    `json:"plan_iteration_count,omitempty"`
}

// AgentsListResponse is the response from GET /api/v1/agents.
type AgentsListResponse struct {
	Agents []AgentResponse `json:"agents"`
}

// FollowupRequestBody is the request body for POST /api/v1/agents/{id}/followup.
type FollowupRequestBody struct {
	Message string `json:"message"`
}

// StatusOKResponse is a generic OK response.
type StatusOKResponse struct {
	Status string `json:"status"`
}

func (p *Plugin) handleGetAgents(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	wantArchived := r.URL.Query().Get("archived") == "true"

	agents, err := p.kvstore.GetAgentsByUser(userID)
	if err != nil {
		p.API.LogError("Failed to get agents by user", "userID", userID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	resp := AgentsListResponse{
		Agents: make([]AgentResponse, 0, len(agents)),
	}
	for _, a := range agents {
		// Filter by archived status.
		if a.Archived != wantArchived {
			continue
		}

		agentResp := AgentResponse{
			ID:          a.CursorAgentID,
			Status:      a.Status,
			Repository:  a.Repository,
			Branch:      a.Branch,
			PrURL:       a.PrURL,
			CursorURL:   fmt.Sprintf("https://cursor.com/agents/%s", a.CursorAgentID),
			ChannelID:   a.ChannelID,
			PostID:      a.PostID,
			RootPostID:  a.PostID,
			Prompt:      a.Prompt,
			Description: a.Description,
			Model:       a.Model,
			Summary:     a.Summary,
			CreatedAt:   a.CreatedAt,
			UpdatedAt:   a.UpdatedAt,
			Archived:    a.Archived,
		}

		// Look up workflow association for HITL-aware agents.
		if wfID, wfErr := p.kvstore.GetWorkflowByAgent(a.CursorAgentID); wfErr == nil && wfID != "" {
			if wf, wfGetErr := p.kvstore.GetWorkflow(wfID); wfGetErr == nil && wf != nil {
				agentResp.WorkflowID = wf.ID
				agentResp.WorkflowPhase = wf.Phase
				agentResp.PlanIterationCount = wf.PlanIterationCount
			}
		}

		resp.Agents = append(resp.Agents, agentResp)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Plugin) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	// Optionally refresh status from Cursor API.
	cursorClient := p.getCursorClient()
	if cursorClient != nil && !cursor.AgentStatus(record.Status).IsTerminal() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if remoteAgent, apiErr := cursorClient.GetAgent(ctx, agentID); apiErr == nil {
			if string(remoteAgent.Status) != record.Status {
				record.Status = string(remoteAgent.Status)
				if remoteAgent.Target.PrURL != "" {
					record.PrURL = remoteAgent.Target.PrURL
				}
				if remoteAgent.Summary != "" {
					record.Summary = remoteAgent.Summary
				}
				record.UpdatedAt = time.Now().UnixMilli()
				_ = p.kvstore.SaveAgent(record)
			}
		}
	}

	resp := AgentResponse{
		ID:          record.CursorAgentID,
		Status:      record.Status,
		Repository:  record.Repository,
		Branch:      record.Branch,
		PrURL:       record.PrURL,
		CursorURL:   fmt.Sprintf("https://cursor.com/agents/%s", record.CursorAgentID),
		ChannelID:   record.ChannelID,
		PostID:      record.PostID,
		RootPostID:  record.PostID,
		Prompt:      record.Prompt,
		Description: record.Description,
		Model:       record.Model,
		Summary:     record.Summary,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
		Archived:    record.Archived,
	}

	// Look up workflow association.
	if wfID, wfErr := p.kvstore.GetWorkflowByAgent(record.CursorAgentID); wfErr == nil && wfID != "" {
		if wf, wfGetErr := p.kvstore.GetWorkflow(wfID); wfGetErr == nil && wf != nil {
			resp.WorkflowID = wf.ID
			resp.WorkflowPhase = wf.Phase
			resp.PlanIterationCount = wf.PlanIterationCount
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Plugin) handleAddFollowup(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	var reqBody FollowupRequestBody
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if reqBody.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	if record.Status != string(cursor.AgentStatusRunning) {
		http.Error(w, "Agent is not in RUNNING state", http.StatusBadRequest)
		return
	}

	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		http.Error(w, "Cursor client not configured", http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, apiErr := cursorClient.AddFollowup(ctx, agentID, cursor.FollowupRequest{
		Prompt: cursor.Prompt{Text: reqBody.Message},
	})
	if apiErr != nil {
		p.API.LogError("Failed to add followup", "agentID", agentID, "error", apiErr.Error())
		http.Error(w, "Failed to send follow-up to Cursor API", http.StatusBadGateway)
		return
	}

	// Post a thread reply via bot.
	if record.PostID != "" {
		_, _ = p.API.CreatePost(&model.Post{
			UserId:    p.getBotUserID(),
			ChannelId: record.ChannelID,
			RootId:    record.PostID,
			Message:   fmt.Sprintf(":speech_balloon: Follow-up sent: %s", reqBody.Message),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(StatusOKResponse{Status: "ok"})
}

func (p *Plugin) handleCancelAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	status := cursor.AgentStatus(record.Status)
	if status.IsTerminal() {
		http.Error(w, fmt.Sprintf("Agent is already in %s state", record.Status), http.StatusBadRequest)
		return
	}

	cursorClient := p.getCursorClient()
	if cursorClient == nil {
		http.Error(w, "Cursor client not configured", http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, apiErr := cursorClient.StopAgent(ctx, agentID); apiErr != nil {
		p.API.LogError("Failed to stop agent via Cursor API", "agentID", agentID, "error", apiErr.Error())
		http.Error(w, "Failed to stop agent via Cursor API", http.StatusBadGateway)
		return
	}

	// Update KV store.
	record.Status = string(cursor.AgentStatusStopped)
	if err := p.kvstore.SaveAgent(record); err != nil {
		p.API.LogError("Failed to update agent record", "agentID", agentID, "error", err.Error())
	}

	// Transition any associated HITL workflow to rejected.
	p.rejectWorkflowForAgent(agentID)

	// Update thread.
	if record.TriggerPostID != "" {
		p.removeReaction(record.TriggerPostID, "hourglass_flowing_sand")
		p.addReaction(record.TriggerPostID, "no_entry_sign")
	}
	if record.PostID != "" {
		cancelAttachment := attachments.BuildStoppedAttachment(
			agentID, record.Repository, record.Branch, record.Model,
		)
		cancelAttachment.Title = "Agent was cancelled via the dashboard."

		cancelPost := &model.Post{
			UserId:    p.getBotUserID(),
			ChannelId: record.ChannelID,
			RootId:    record.PostID,
		}
		model.ParseSlackAttachment(cancelPost, []*model.SlackAttachment{cancelAttachment})
		_, _ = p.API.CreatePost(cancelPost)

		// Also update the original bot reply post to reflect cancellation.
		p.updateBotReplyWithAttachment(record.BotReplyPostID, cancelAttachment)
	}

	// Publish WebSocket event.
	p.publishAgentStatusChange(record)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(StatusOKResponse{Status: "ok"})
}

func (p *Plugin) handleArchiveAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	// If agent is still active, stop it first.
	status := cursor.AgentStatus(record.Status)
	if !status.IsTerminal() {
		cursorClient := p.getCursorClient()
		if cursorClient == nil {
			p.API.LogError("Cannot stop agent: Cursor client not initialized", "agentID", agentID)
			http.Error(w, "Cursor client not configured", http.StatusInternalServerError)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, apiErr := cursorClient.StopAgent(ctx, agentID); apiErr != nil {
			p.API.LogError("Failed to stop agent before archiving", "agentID", agentID, "error", apiErr.Error())
			http.Error(w, "Failed to stop agent", http.StatusInternalServerError)
			return
		}
		record.Status = string(cursor.AgentStatusStopped)
	}

	record.Archived = true
	record.UpdatedAt = time.Now().UnixMilli()
	if err := p.kvstore.SaveAgent(record); err != nil {
		p.API.LogError("Failed to save archived agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Transition any associated HITL workflow to rejected.
	p.rejectWorkflowForAgent(agentID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(StatusOKResponse{Status: "ok"})
}

func (p *Plugin) handleUnarchiveAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	agentID := mux.Vars(r)["id"]

	record, err := p.kvstore.GetAgent(agentID)
	if err != nil {
		p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if record == nil || record.UserID != userID {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	record.Archived = false
	record.UpdatedAt = time.Now().UnixMilli()
	if err := p.kvstore.SaveAgent(record); err != nil {
		p.API.LogError("Failed to save unarchived agent", "agentID", agentID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(StatusOKResponse{Status: "ok"})
}

// WorkflowResponse is the JSON representation of a HITL workflow for the webapp.
type WorkflowResponse struct {
	ID                 string `json:"id"`
	UserID             string `json:"user_id"`
	ChannelID          string `json:"channel_id"`
	RootPostID         string `json:"root_post_id"`
	Phase              string `json:"phase"`
	Repository         string `json:"repository"`
	Branch             string `json:"branch"`
	Model              string `json:"model"`
	OriginalPrompt     string `json:"original_prompt"`
	EnrichedContext    string `json:"enriched_context"`
	ApprovedContext    string `json:"approved_context"`
	PlannerAgentID     string `json:"planner_agent_id"`
	RetrievedPlan      string `json:"retrieved_plan"`
	ApprovedPlan       string `json:"approved_plan"`
	PlanIterationCount int    `json:"plan_iteration_count"`
	ImplementerAgentID string `json:"implementer_agent_id"`
	SkipContextReview  bool   `json:"skip_context_review"`
	SkipPlanLoop       bool   `json:"skip_plan_loop"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

func (p *Plugin) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	workflowID := mux.Vars(r)["id"]

	workflow, err := p.kvstore.GetWorkflow(workflowID)
	if err != nil {
		p.API.LogError("Failed to get workflow", "workflowID", workflowID, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if workflow == nil || workflow.UserID != userID {
		http.Error(w, "Workflow not found", http.StatusNotFound)
		return
	}

	resp := WorkflowResponse{
		ID:                 workflow.ID,
		UserID:             workflow.UserID,
		ChannelID:          workflow.ChannelID,
		RootPostID:         workflow.RootPostID,
		Phase:              workflow.Phase,
		Repository:         workflow.Repository,
		Branch:             workflow.Branch,
		Model:              workflow.Model,
		OriginalPrompt:     workflow.OriginalPrompt,
		EnrichedContext:    workflow.EnrichedContext,
		ApprovedContext:    workflow.ApprovedContext,
		PlannerAgentID:     workflow.PlannerAgentID,
		RetrievedPlan:      workflow.RetrievedPlan,
		ApprovedPlan:       workflow.ApprovedPlan,
		PlanIterationCount: workflow.PlanIterationCount,
		ImplementerAgentID: workflow.ImplementerAgentID,
		SkipContextReview:  workflow.SkipContextReview,
		SkipPlanLoop:       workflow.SkipPlanLoop,
		CreatedAt:          workflow.CreatedAt,
		UpdatedAt:          workflow.UpdatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleHITLResponse processes PostAction button clicks for HITL Accept/Reject.
func (p *Plugin) handleHITLResponse(w http.ResponseWriter, r *http.Request) {
	// Step 1: Parse the PostActionIntegrationRequest.
	var request model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		p.API.LogError("Failed to decode HITL action request", "error", err.Error())
		p.writePostActionResponseAttachment(w, nil)
		return
	}

	// Step 2: Extract context values.
	workflowID, _ := request.Context["workflow_id"].(string)
	action, _ := request.Context["action"].(string)
	phase, _ := request.Context["phase"].(string)

	if workflowID == "" || action == "" || phase == "" {
		p.API.LogError("HITL action missing required context",
			"workflow_id", workflowID,
			"action", action,
			"phase", phase,
		)
		p.writePostActionResponseAttachment(w, nil)
		return
	}

	// Step 3: Load the workflow.
	workflow, err := p.kvstore.GetWorkflow(workflowID)
	if err != nil {
		p.API.LogError("Failed to get workflow for HITL action",
			"workflow_id", workflowID,
			"error", err.Error(),
		)
		p.writePostActionResponseAttachment(w, nil)
		return
	}
	if workflow == nil {
		p.sendEphemeralToActionUser(request, "This workflow no longer exists.")
		p.writePostActionResponseAttachment(w, nil)
		return
	}

	// Step 4: Validate caller is the workflow initiator.
	callerUserID := request.UserId
	if callerUserID != workflow.UserID {
		ownerUsername := p.getUsername(workflow.UserID)
		p.sendEphemeralToActionUser(request, fmt.Sprintf("Only @%s can approve or reject this workflow.", ownerUsername))
		p.writePostActionResponseAttachment(w, nil)
		return
	}

	// Step 5: Check for already-handled workflows (double-click prevention).
	username := p.getUsername(workflow.UserID)

	if phase == kvstore.PhaseContextReview && workflow.Phase != kvstore.PhaseContextReview {
		p.sendEphemeralToActionUser(request, "This context review has already been resolved.")
		var updatedAttachment *model.SlackAttachment
		if workflow.Phase == kvstore.PhaseRejected {
			updatedAttachment = attachments.BuildContextRejectedAttachment(username)
		} else {
			updatedAttachment = attachments.BuildContextAcceptedAttachment(
				workflow.Repository, workflow.Branch, workflow.Model, username,
			)
		}
		p.writePostActionResponseAttachment(w, updatedAttachment)
		return
	}

	if phase == kvstore.PhasePlanReview && workflow.Phase != kvstore.PhasePlanReview {
		p.sendEphemeralToActionUser(request, "This plan review has already been resolved.")
		var updatedAttachment *model.SlackAttachment
		if workflow.Phase == kvstore.PhaseRejected {
			updatedAttachment = attachments.BuildPlanRejectedAttachment(username)
		} else {
			updatedAttachment = attachments.BuildPlanAcceptedAttachment(username, workflow.PlanIterationCount)
		}
		p.writePostActionResponseAttachment(w, updatedAttachment)
		return
	}

	// Step 6: Handle the action.
	switch action {
	case "accept":
		switch phase {
		case kvstore.PhaseContextReview:
			acceptedAttachment := attachments.BuildContextAcceptedAttachment(
				workflow.Repository, workflow.Branch, workflow.Model, username,
			)
			p.writePostActionResponseAttachment(w, acceptedAttachment)
			go p.acceptContext(workflow)
		case kvstore.PhasePlanReview:
			acceptedAttachment := attachments.BuildPlanAcceptedAttachment(username, workflow.PlanIterationCount)
			p.writePostActionResponseAttachment(w, acceptedAttachment)
			go p.acceptPlan(workflow)
		default:
			p.API.LogError("Unknown HITL phase for accept", "phase", phase, "action", action)
			p.writePostActionResponseAttachment(w, nil)
		}

	case "reject":
		switch phase {
		case kvstore.PhaseContextReview:
			rejectedAttachment := attachments.BuildContextRejectedAttachment(username)
			p.writePostActionResponseAttachment(w, rejectedAttachment)
			go p.rejectWorkflow(workflow)
		case kvstore.PhasePlanReview:
			rejectedAttachment := attachments.BuildPlanRejectedAttachment(username)
			p.writePostActionResponseAttachment(w, rejectedAttachment)
			go p.rejectWorkflow(workflow)
		default:
			p.API.LogError("Unknown HITL phase for reject", "phase", phase, "action", action)
			p.writePostActionResponseAttachment(w, nil)
		}

	default:
		p.API.LogError("Unknown HITL action", "action", action)
		p.writePostActionResponseAttachment(w, nil)
	}
}

// writePostActionResponseAttachment writes a PostActionIntegrationResponse.
// If attachment is non-nil, the response uses Update to replace the post's attachment
// (this removes the action buttons). If nil, returns an empty response (no-op on the post).
func (p *Plugin) writePostActionResponseAttachment(w http.ResponseWriter, attachment *model.SlackAttachment) {
	w.Header().Set("Content-Type", "application/json")

	resp := &model.PostActionIntegrationResponse{}
	if attachment != nil {
		resp.Update = &model.Post{}
		model.ParseSlackAttachment(resp.Update, []*model.SlackAttachment{attachment})
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		p.API.LogError("Failed to encode PostActionIntegrationResponse", "error", err.Error())
	}
}

// sendEphemeralToActionUser sends an ephemeral post to the user who clicked a PostAction button.
func (p *Plugin) sendEphemeralToActionUser(request model.PostActionIntegrationRequest, message string) {
	// Determine the thread root: if the post is already in a thread, use its RootId.
	rootID := request.PostId
	if post, err := p.API.GetPost(request.PostId); err == nil && post != nil && post.RootId != "" {
		rootID = post.RootId
	}

	_ = p.API.SendEphemeralPost(request.UserId, &model.Post{
		UserId:    p.getBotUserID(),
		ChannelId: request.ChannelId,
		RootId:    rootID,
		Message:   message,
	})
}
