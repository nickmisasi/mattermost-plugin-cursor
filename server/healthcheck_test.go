package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthzEndpoint_ReturnsStatusAndUptime(t *testing.T) {
	originalStart := healthcheckStartedAt
	healthcheckStartedAt = time.Now().Add(-5 * time.Second)
	t.Cleanup(func() {
		healthcheckStartedAt = originalStart
	})

	plugin := Plugin{}
	plugin.router = plugin.initRouter()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	plugin.ServeHTTP(nil, rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp HealthzResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)

	uptime, err := time.ParseDuration(resp.Uptime)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, uptime, 5*time.Second)
}

func TestHealthzEndpoint_MethodNotAllowed(t *testing.T) {
	plugin := Plugin{}
	plugin.router = plugin.initRouter()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	plugin.ServeHTTP(nil, rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}
