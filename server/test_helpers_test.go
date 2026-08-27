package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testServiceAccountAPIKey = "service-account-key"
)

func newTestPlugin(t *testing.T, upstream http.Handler) *Plugin {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	p := &Plugin{
		httpClient: server.Client(),
	}
	p.setConfiguration(&configuration{
		CursorAPIBaseURL:     server.URL,
		ServiceAccountAPIKey: testServiceAccountAPIKey,
	})
	p.hydration.initialize(hydrationCacheCapacity)
	return p
}
