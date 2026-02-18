package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

var apiRequestCountsByEndpoint = map[string]int{}

func getAPIRequestCountsByEndpoint() map[string]int {
	return apiRequestCountsByEndpoint
}

func apiRequestEndpointKey(r *http.Request) string {
	path := r.URL.Path
	if route := mux.CurrentRoute(r); route != nil {
		if template, err := route.GetPathTemplate(); err == nil && template != "" {
			path = template
		}
	}

	return fmt.Sprintf("%s %s", r.Method, path)
}

func (p *Plugin) trackAPIRequestCounts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := apiRequestEndpointKey(r)
		apiRequestCountsByEndpoint[endpoint]++

		next.ServeHTTP(w, r)
	})
}
