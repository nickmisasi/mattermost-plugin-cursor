package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
)

var (
	// apiRequestCounts stores request totals by endpoint key.
	apiRequestCounts = map[string]int{}

	// apiRequestCountsLock protects apiRequestCounts across concurrent requests.
	apiRequestCountsLock sync.RWMutex

	apiPathNormalizers = []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{pattern: regexp.MustCompile(`^/api/v1/agents/[^/]+/followup$`), replacement: "/api/v1/agents/{id}/followup"},
		{pattern: regexp.MustCompile(`^/api/v1/agents/[^/]+/archive$`), replacement: "/api/v1/agents/{id}/archive"},
		{pattern: regexp.MustCompile(`^/api/v1/agents/[^/]+/unarchive$`), replacement: "/api/v1/agents/{id}/unarchive"},
		{pattern: regexp.MustCompile(`^/api/v1/agents/[^/]+$`), replacement: "/api/v1/agents/{id}"},
		{pattern: regexp.MustCompile(`^/api/v1/workflows/[^/]+$`), replacement: "/api/v1/workflows/{id}"},
	}
)

func recordAPIRequest(endpoint string) {
	apiRequestCountsLock.Lock()
	defer apiRequestCountsLock.Unlock()

	apiRequestCounts[endpoint]++
}

func endpointKey(r *http.Request) string {
	path := r.URL.Path
	if path == "" {
		path = "/"
	}
	path = normalizeAPIPath(path)

	return r.Method + " " + path
}

func normalizeAPIPath(path string) string {
	for _, normalizer := range apiPathNormalizers {
		if normalizer.pattern.MatchString(path) {
			// Keep replacements literal; never interpret $n as backreferences.
			return normalizer.pattern.ReplaceAllLiteralString(path, normalizer.replacement)
		}
	}

	return path
}

func getAPIRequestCountsSnapshot() map[string]int {
	apiRequestCountsLock.RLock()
	defer apiRequestCountsLock.RUnlock()

	snapshot := make(map[string]int, len(apiRequestCounts))
	for endpoint, count := range apiRequestCounts {
		snapshot[endpoint] = count
	}

	return snapshot
}

// apiMetricsMiddleware records every request that reaches the plugin router.
// Counts include requests later rejected by auth middleware (401/403) and
// unmatched paths that end as 404 responses.
func apiMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordAPIRequest(endpointKey(r))
		next.ServeHTTP(w, r)
	})
}

type MetricsResponse struct {
	APIRequestCounts map[string]int `json:"api_request_counts"`
}

func (p *Plugin) handleGetMetrics(w http.ResponseWriter, _ *http.Request) {
	response := MetricsResponse{
		APIRequestCounts: getAPIRequestCountsSnapshot(),
	}

	var payload bytes.Buffer
	if err := json.NewEncoder(&payload).Encode(response); err != nil {
		p.API.LogError("Failed to encode metrics response", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(payload.Bytes()); err != nil {
		p.API.LogError("Failed to write metrics response", "error", err.Error())
	}
}
