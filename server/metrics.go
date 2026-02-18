package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

const unmatchedEndpoint = "UNMATCHED"

func (p *Plugin) getAPIRequestCountsByEndpoint() map[string]int {
	p.apiRequestCountsByEndpointMu.Lock()
	defer p.apiRequestCountsByEndpointMu.Unlock()

	snapshot := make(map[string]int, len(p.apiRequestCountsByEndpoint))
	for key, count := range p.apiRequestCountsByEndpoint {
		snapshot[key] = count
	}

	return snapshot
}

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
		p.apiRequestCountsByEndpoint[endpoint]++
		p.apiRequestCountsByEndpointMu.Unlock()

		next.ServeHTTP(w, r)
	})
}
