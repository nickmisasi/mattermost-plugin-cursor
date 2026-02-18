package main

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

const unmatchedEndpoint = "UNMATCHED"

var (
	apiRequestCountsByEndpoint   = map[string]int{}
	apiRequestCountsByEndpointMu sync.Mutex
)

func getAPIRequestCountsByEndpoint() map[string]int {
	apiRequestCountsByEndpointMu.Lock()
	defer apiRequestCountsByEndpointMu.Unlock()

	snapshot := make(map[string]int, len(apiRequestCountsByEndpoint))
	for key, count := range apiRequestCountsByEndpoint {
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

		apiRequestCountsByEndpointMu.Lock()
		apiRequestCountsByEndpoint[endpoint]++
		apiRequestCountsByEndpointMu.Unlock()

		next.ServeHTTP(w, r)
	})
}
