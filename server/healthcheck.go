package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// healthcheckStartedAt tracks process start time for /healthz uptime reporting.
var healthcheckStartedAt = time.Now()

// HealthzResponse is the JSON payload for the lightweight /healthz endpoint.
type HealthzResponse struct {
	Status string `json:"status"`
	Uptime string `json:"uptime"`
}

func (p *Plugin) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	response := HealthzResponse{
		Status: "ok",
		Uptime: time.Since(healthcheckStartedAt).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		p.API.LogError("Failed to encode /healthz response", "error", err.Error())
	}
}
