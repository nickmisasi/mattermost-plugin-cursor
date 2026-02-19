package main

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetAPIMetricsForTest(t *testing.T) {
	t.Helper()

	resetAPIRequestCounters()
	t.Cleanup(resetAPIRequestCounters)
}

func TestAPIRequestMetrics_AggregatesDynamicRouteIDs(t *testing.T) {
	resetAPIMetricsForTest(t)
	p, _, _, _ := setupAPITestPlugin(t)

	rr1 := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "")
	rr2 := doRequest(p, http.MethodGet, "/api/v1/agents/agent-2", nil, "")
	rr3 := doRequest(p, http.MethodGet, "/api/v1/agents/agent-3", nil, "")

	assert.Equal(t, http.StatusUnauthorized, rr1.Code)
	assert.Equal(t, http.StatusUnauthorized, rr2.Code)
	assert.Equal(t, http.StatusUnauthorized, rr3.Code)

	counters := getAPIRequestCountersSnapshot()
	assert.Equal(t, uint64(3), counters["GET /api/v1/agents/{id}"])
	assert.Len(t, counters, 1)
}

func TestAPIRequestMetrics_SplitsByMethod(t *testing.T) {
	resetAPIMetricsForTest(t)
	p, _, _, _ := setupAPITestPlugin(t)

	rrGet := doRequest(p, http.MethodGet, "/api/v1/agents/agent-1", nil, "")
	rrDelete1 := doRequest(p, http.MethodDelete, "/api/v1/agents/agent-1", nil, "")
	rrDelete2 := doRequest(p, http.MethodDelete, "/api/v1/agents/agent-2", nil, "")

	assert.Equal(t, http.StatusUnauthorized, rrGet.Code)
	assert.Equal(t, http.StatusUnauthorized, rrDelete1.Code)
	assert.Equal(t, http.StatusUnauthorized, rrDelete2.Code)

	counters := getAPIRequestCountersSnapshot()
	assert.Equal(t, uint64(1), counters["GET /api/v1/agents/{id}"])
	assert.Equal(t, uint64(2), counters["DELETE /api/v1/agents/{id}"])
	assert.Len(t, counters, 2)
}

func TestAPIRequestMetrics_DoesNotCountUnmatchedRoutes(t *testing.T) {
	resetAPIMetricsForTest(t)
	p, _, _, _ := setupAPITestPlugin(t)

	rr := doRequest(p, http.MethodGet, "/this/path/does/not/exist", nil, "")
	assert.Equal(t, http.StatusNotFound, rr.Code)

	counters := getAPIRequestCountersSnapshot()
	assert.Empty(t, counters)
}

func TestAPIRequestMetrics_ConcurrentRequests(t *testing.T) {
	resetAPIMetricsForTest(t)
	p, _, _, _ := setupAPITestPlugin(t)

	const requestCount = 200

	var wg sync.WaitGroup
	errors := make(chan error, requestCount)

	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			path := fmt.Sprintf("/api/v1/agents/agent-%d", i)
			rr := doRequest(p, http.MethodGet, path, nil, "")
			if rr.Code != http.StatusUnauthorized {
				errors <- fmt.Errorf("unexpected status %d for %s", rr.Code, path)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		require.NoError(t, err)
	}

	counters := getAPIRequestCountersSnapshot()
	assert.Equal(t, uint64(requestCount), counters["GET /api/v1/agents/{id}"])
	assert.Len(t, counters, 1)
}
