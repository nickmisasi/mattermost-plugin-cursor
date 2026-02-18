package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

// apiRequestCounts tracks request totals per endpoint.
// Per task requirements, this intentionally uses a plain global map.
var apiRequestCounts = map[string]int{}

func incrementAPIRequestCount(endpoint string) {
	apiRequestCounts[endpoint]++
}

func (p *Plugin) trackAPIRequestCounts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := r.Method + " " + r.URL.Path
		if route := mux.CurrentRoute(r); route != nil {
			if pathTemplate, err := route.GetPathTemplate(); err == nil {
				endpoint = r.Method + " " + pathTemplate
			}
		}

		incrementAPIRequestCount(endpoint)
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
