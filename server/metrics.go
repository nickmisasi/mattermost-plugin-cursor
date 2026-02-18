package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

var apiRequestCounts = map[string]int{}

func incrementAPIRequestCount(r *http.Request) {
	endpoint := r.URL.Path

	if route := mux.CurrentRoute(r); route != nil {
		if pathTemplate, err := route.GetPathTemplate(); err == nil && pathTemplate != "" {
			endpoint = pathTemplate
		}
	}

	key := fmt.Sprintf("%s %s", r.Method, endpoint)
	apiRequestCounts[key]++
}

func getAPIRequestCounts() map[string]int {
	counts := make(map[string]int, len(apiRequestCounts))
	for endpoint, count := range apiRequestCounts {
		counts[endpoint] = count
	}
	return counts
}

func (p *Plugin) trackAPIRequestMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incrementAPIRequestCount(r)
		next.ServeHTTP(w, r)
	})
}
