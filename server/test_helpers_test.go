package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"
)

const testUserID = "user-1"

type memoryKV struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newMemoryKV() *memoryKV {
	return &memoryKV{values: make(map[string][]byte)}
}

func (m *memoryKV) KVGet(key string) ([]byte, *model.AppError) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.values[key]...), nil
}

func (m *memoryKV) KVSet(key string, value []byte) *model.AppError {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = append([]byte(nil), value...)
	return nil
}

func (m *memoryKV) KVCompareAndSet(key string, oldValue, newValue []byte) (bool, *model.AppError) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, exists := m.values[key]
	if oldValue == nil && exists {
		return false, nil
	}
	if oldValue != nil && !bytes.Equal(current, oldValue) {
		return false, nil
	}
	m.values[key] = append([]byte(nil), newValue...)
	return true, nil
}

func (m *memoryKV) KVDelete(key string) *model.AppError {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}

func newTestPlugin(t *testing.T, upstream http.Handler) (*Plugin, *memoryKV) {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	kv := newMemoryKV()
	keyStore, err := NewKeyStore(kv)
	require.NoError(t, err)
	p := &Plugin{
		keyStore:   keyStore,
		httpClient: server.Client(),
	}
	p.setConfiguration(&configuration{CursorAPIBaseURL: server.URL})
	p.cache.initialize(5 * time.Minute)
	p.hydration.initialize(hydrationCacheCapacity)
	p.initRouter()
	p.getMCPUserID = func(_ context.Context) string { return testUserID }
	return p, kv
}

func authenticatedRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Mattermost-User-Id", testUserID)
	return request
}
