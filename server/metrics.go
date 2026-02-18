package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

// apiRequestCounts stores request totals per API endpoint.
var apiRequestCounts = map[string]int{}

func endpointKeyForRequest(r *http.Request) string {
	endpoint := r.URL.Path

	if route := mux.CurrentRoute(r); route != nil {
		if pathTemplate, err := route.GetPathTemplate(); err == nil {
			endpoint = pathTemplate
		}
	}

	return r.Method + " " + endpoint
}

func (p *Plugin) trackAPIRequestCount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiRequestCounts[endpointKeyForRequest(r)]++
		next.ServeHTTP(w, r)
	})
}

func getAPIRequestCounts() map[string]int {
	counts := make(map[string]int, len(apiRequestCounts))
	for endpoint, count := range apiRequestCounts {
		counts[endpoint] = count
	}

	return counts
}
