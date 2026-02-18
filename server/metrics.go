package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

// apiRequestCounts stores request totals keyed by method and endpoint.
var apiRequestCounts = map[string]int{}

// apiMetricsMiddleware tracks request counts per endpoint.
func apiMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := r.URL.Path
		if route := mux.CurrentRoute(r); route != nil {
			if template, err := route.GetPathTemplate(); err == nil {
				endpoint = template
			}
		}

		key := r.Method + " " + endpoint
		apiRequestCounts[key]++

		next.ServeHTTP(w, r)
	})
}
