package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

// apiRequestCounts tracks request totals keyed by "METHOD /path/template".
var apiRequestCounts = map[string]int{}

func incrementAPIRequestCount(endpoint string) {
	apiRequestCounts[endpoint]++
}

func apiRequestMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := r.URL.Path
		if route := mux.CurrentRoute(r); route != nil {
			if pathTemplate, err := route.GetPathTemplate(); err == nil {
				endpoint = pathTemplate
			}
		}

		incrementAPIRequestCount(r.Method + " " + endpoint)
		next.ServeHTTP(w, r)
	})
}
