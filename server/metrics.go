package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

// apiRequestCounts stores request totals by endpoint key.
//
// This map is intentionally unsynchronized per task requirements.
var apiRequestCounts = map[string]int{}

func recordAPIRequest(endpoint string) {
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

func (p *Plugin) apiMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordAPIRequest(endpointKey(r))
		next.ServeHTTP(w, r)
	})
}
