package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func doRateLimitRequest(handler http.Handler, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	if userID != "" {
		req.Header.Set("Mattermost-User-ID", userID)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestRateLimitMiddleware_AllowsUpToLimitThenBlocks(t *testing.T) {
	limiter := newInMemoryRateLimiter(rateLimitMaxRequests, rateLimitWindow, nil)
	handler := newRateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	userID := "test-user-limit"
	for i := 0; i < rateLimitMaxRequests; i++ {
		rr := doRateLimitRequest(handler, userID)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	rr := doRateLimitRequest(handler, userID)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
}

func TestRateLimitMiddleware_IsPerUser(t *testing.T) {
	limiter := newInMemoryRateLimiter(1, rateLimitWindow, nil)
	handler := newRateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	firstUser := "test-user-a"
	secondUser := "test-user-b"

	rr := doRateLimitRequest(handler, firstUser)
	assert.Equal(t, http.StatusOK, rr.Code)

	rr = doRateLimitRequest(handler, firstUser)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	rr = doRateLimitRequest(handler, secondUser)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRateLimitMiddleware_ResetsAfterWindow(t *testing.T) {
	currentTime := time.Unix(0, 0)
	limiter := newInMemoryRateLimiter(2, time.Minute, func() time.Time {
		return currentTime
	})
	handler := newRateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	userID := "test-user-reset"
	assert.Equal(t, http.StatusOK, doRateLimitRequest(handler, userID).Code)
	assert.Equal(t, http.StatusOK, doRateLimitRequest(handler, userID).Code)
	assert.Equal(t, http.StatusTooManyRequests, doRateLimitRequest(handler, userID).Code)

	currentTime = currentTime.Add(time.Minute)
	assert.Equal(t, http.StatusOK, doRateLimitRequest(handler, userID).Code)
}
