package main

import (
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

var (
	apiRequestMetricsMutex sync.RWMutex
	apiRequestMetrics      = map[string]uint64{}
)

func endpointKeyFromRequest(r *http.Request) (string, bool) {
	route := mux.CurrentRoute(r)
	if route == nil {
		return "", false
	}

	pathTemplate, err := route.GetPathTemplate()
	if err != nil || pathTemplate == "" {
		return "", false
	}

	return r.Method + " " + pathTemplate, true
}

func incrementAPIRequestCounter(endpointKey string) {
	apiRequestMetricsMutex.Lock()
	defer apiRequestMetricsMutex.Unlock()

	apiRequestMetrics[endpointKey]++
}

func getAPIRequestCountersSnapshot() map[string]uint64 {
	apiRequestMetricsMutex.RLock()
	defer apiRequestMetricsMutex.RUnlock()

	snapshot := make(map[string]uint64, len(apiRequestMetrics))
	for key, count := range apiRequestMetrics {
		snapshot[key] = count
	}

	return snapshot
}

func resetAPIRequestCounters() {
	apiRequestMetricsMutex.Lock()
	defer apiRequestMetricsMutex.Unlock()

	clear(apiRequestMetrics)
}

func apiRequestMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if endpointKey, ok := endpointKeyFromRequest(r); ok {
			incrementAPIRequestCounter(endpointKey)
		}

		next.ServeHTTP(w, r)
	})
}
