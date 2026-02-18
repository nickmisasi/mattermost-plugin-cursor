package main

import (
	"net/http"
	"sync"
	"time"
)

const (
	rateLimitMaxRequests = 100
	rateLimitWindow      = time.Minute
)

type rateLimitEntry struct {
	windowStart time.Time
	count       int
}

type inMemoryRateLimiter struct {
	mutex       sync.Mutex
	requests    map[string]rateLimitEntry
	maxRequests int
	window      time.Duration
	now         func() time.Time
}

func newInMemoryRateLimiter(maxRequests int, window time.Duration, now func() time.Time) *inMemoryRateLimiter {
	if now == nil {
		now = time.Now
	}

	return &inMemoryRateLimiter{
		requests:    make(map[string]rateLimitEntry),
		maxRequests: maxRequests,
		window:      window,
		now:         now,
	}
}

func (l *inMemoryRateLimiter) allow(userID string) bool {
	// Unauthenticated routes are protected by separate middleware and should
	// not be rate-limited by user ID.
	if userID == "" {
		return true
	}

	now := l.now()

	l.mutex.Lock()
	defer l.mutex.Unlock()

	entry, exists := l.requests[userID]
	if !exists || now.Sub(entry.windowStart) >= l.window {
		l.requests[userID] = rateLimitEntry{
			windowStart: now,
			count:       1,
		}
		return true
	}

	if entry.count >= l.maxRequests {
		return false
	}

	entry.count++
	l.requests[userID] = entry
	return true
}

func newRateLimitMiddleware(limiter *inMemoryRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.Header.Get("Mattermost-User-ID")
			if !limiter.allow(userID) {
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

var defaultRateLimitMiddleware = newRateLimitMiddleware(
	newInMemoryRateLimiter(rateLimitMaxRequests, rateLimitWindow, nil),
)

// RateLimitMiddleware enforces a per-user request limit of 100 requests/minute.
func RateLimitMiddleware(next http.Handler) http.Handler {
	return defaultRateLimitMiddleware(next)
}
