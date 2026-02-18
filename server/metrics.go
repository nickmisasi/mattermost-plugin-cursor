package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

// apiRequestCounts tracks request totals by "METHOD /path/template".
var apiRequestCounts = map[string]int{}

func apiRequestMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := apiRequestEndpointKey(r)
		apiRequestCounts[endpoint]++
		next.ServeHTTP(w, r)
	})
}

func apiRequestEndpointKey(r *http.Request) string {
	route := mux.CurrentRoute(r)
	if route != nil {
		if template, err := route.GetPathTemplate(); err == nil && template != "" {
			return r.Method + " " + template
		}
	}

	return r.Method + " " + r.URL.Path
}

func getAPIRequestCounts() map[string]int {
	return apiRequestCounts
}
