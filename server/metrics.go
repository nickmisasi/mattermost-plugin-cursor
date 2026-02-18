package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

const unmatchedEndpoint = "UNMATCHED"

func apiRequestEndpointKey(r *http.Request) string {
	path := unmatchedEndpoint
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

		p.apiRequestCountsByEndpointMu.Lock()
		if p.apiRequestCountsByEndpoint == nil {
			p.apiRequestCountsByEndpoint = make(map[string]int)
		}
		p.apiRequestCountsByEndpoint[endpoint]++
		p.apiRequestCountsByEndpointMu.Unlock()

		next.ServeHTTP(w, r)
	})
}
