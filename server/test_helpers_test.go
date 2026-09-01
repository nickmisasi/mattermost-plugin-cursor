package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	testServiceAccountAPIKey = "service-account-key"
	testMCPUserID            = "test-user"
)

func newTestPlugin(t *testing.T, upstream http.Handler) *Plugin {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	p := &Plugin{
		httpClient: server.Client(),
		lookupUser: func(userID string) (*model.User, error) {
			return &model.User{Id: userID, Roles: model.SystemUserRoleId}, nil
		},
		getMCPUserID: func(context.Context) string {
			return testMCPUserID
		},
	}
	p.setConfiguration(&configuration{
		CursorAPIBaseURL:     server.URL,
		ServiceAccountAPIKey: testServiceAccountAPIKey,
	})
	p.hydration.initialize(hydrationCacheCapacity)
	return p
}
