package main

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

var (
	// apiRequestCounts stores request totals by endpoint key.
	apiRequestCounts = map[string]int{}

	// apiRequestCountsLock protects apiRequestCounts across concurrent requests.
	apiRequestCountsLock sync.RWMutex
)

func recordAPIRequest(endpoint string) {
	apiRequestCountsLock.Lock()
	defer apiRequestCountsLock.Unlock()

	apiRequestCounts[endpoint]++
}

func endpointKey(r *http.Request) string {
	route := mux.CurrentRoute(r)
	if route != nil {
		if template, err := route.GetPathTemplate(); err == nil {
			return r.Method + " " + template
		}
	}

	path := r.URL.Path
	if path == "" {
		path = "/"
	}

	return r.Method + " " + path
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

func (p *Plugin) apiMetricsMiddleware(next http.Handler) http.Handler {
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		p.API.LogError("Failed to encode metrics response", "error", err.Error())
	}
}
