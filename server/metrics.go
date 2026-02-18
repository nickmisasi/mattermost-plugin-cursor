package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// apiRequestCounts tracks request volume per API endpoint.
var apiRequestCounts = map[string]int{}

func (p *Plugin) trackAPIRequestCounts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := r.URL.Path
		if route := mux.CurrentRoute(r); route != nil {
			if pathTemplate, err := route.GetPathTemplate(); err == nil {
				endpoint = pathTemplate
			}
		}

		key := fmt.Sprintf("%s %s", r.Method, endpoint)
		apiRequestCounts[key]++

		next.ServeHTTP(w, r)
	})
}
