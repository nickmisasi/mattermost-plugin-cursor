package main

import (
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

// apiRequestCounts tracks request totals per endpoint.
var (
	apiRequestCountsMu sync.RWMutex
	apiRequestCounts   = map[string]int{}
)

func endpointKeyFromRequest(r *http.Request) string {
	endpoint := r.Method + " " + r.URL.Path
	if route := mux.CurrentRoute(r); route != nil {
		if pathTemplate, err := route.GetPathTemplate(); err == nil {
			endpoint = r.Method + " " + pathTemplate
		}
	}

	return endpoint
}

func incrementAPIRequestCount(endpoint string) {
	apiRequestCountsMu.Lock()
	defer apiRequestCountsMu.Unlock()

	apiRequestCounts[endpoint]++
}

func (p *Plugin) trackAPIRequestCounts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incrementAPIRequestCount(endpointKeyFromRequest(r))
		next.ServeHTTP(w, r)
	})
}

func (p *Plugin) trackNotFoundRequest(w http.ResponseWriter, r *http.Request) {
	incrementAPIRequestCount(endpointKeyFromRequest(r))
	http.NotFound(w, r)
}

func (p *Plugin) trackMethodNotAllowedRequest(w http.ResponseWriter, r *http.Request) {
	incrementAPIRequestCount(endpointKeyFromRequest(r))
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
