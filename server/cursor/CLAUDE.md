# Cursor API Client

## Design

Interface-based design for testability. The `Client` interface is defined in `client.go` and the concrete implementation is `clientImpl`.

```go
type Client interface {
    LaunchAgent(ctx, req) (*Agent, error)     // POST /v0/agents
    GetAgent(ctx, id) (*Agent, error)         // GET /v0/agents/{id}
    ListAgents(ctx, limit, cursor) (*ListAgentsResponse, error)  // GET /v0/agents
    AddFollowup(ctx, id, req) (*FollowupResponse, error)        // POST /v0/agents/{id}/followup
    GetConversation(ctx, id) (*Conversation, error)              // GET /v0/agents/{id}/conversation
    StopAgent(ctx, id) (*StopResponse, error)  // POST /v0/agents/{id}/stop
    DeleteAgent(ctx, id) (*DeleteResponse, error) // DELETE /v0/agents/{id}
    ListModels(ctx) (*ListModelsResponse, error)  // GET /v0/models
    GetMe(ctx) (*APIKeyInfo, error)            // GET /v0/me (health check)
}
```

## Authentication

Uses **HTTP Basic Auth** with the API key as the username and empty password:
```go
req.SetBasicAuth(c.apiKey, "")
```

**Not** Bearer token auth despite what some docs suggest. This is confirmed in tests.

## Base URL

- Default: `https://api.cursor.com`
- All paths are under `/v0/` (e.g., `/v0/agents`, `/v0/models`, `/v0/me`)
- Customizable via `NewClientWithBaseURL()` for testing

## Retry Logic

Automatic retries with exponential backoff:
- **Retries on**: 429 (rate limit), 5xx (server errors), transport errors
- **No retry on**: 4xx errors except 429 (client errors are final)
- **Max retries**: 3 (total 4 attempts)
- **Backoff**: 1s, 2s, 4s (exponential)
- **Context-aware**: Respects context cancellation between retries

## Error Handling

`APIError` struct with three fields:
- `StatusCode`: HTTP status code
- `Message`: Parsed from JSON response `{"message": "..."}`
- `RawBody`: Full response body string, useful when JSON parsing fails

Error fallback chain: parse JSON for `message` field -> if empty, use `RawBody` -> format as `"cursor API error (HTTP %d): %s"`.

## Logger

Optional debug logger via functional option:
```go
client := cursor.NewClient(apiKey, cursor.WithLogger(myLogger))
```

The `Logger` interface has a single method: `LogDebug(msg string, keyValuePairs ...interface{})`. When set, the client logs request URLs, response status codes, request/response bodies, and retry attempts.

## Agent Status Lifecycle

```
CREATING -> RUNNING -> FINISHED | FAILED | STOPPED
```

`AgentStatus.IsTerminal()` returns true for FINISHED, FAILED, STOPPED.

## Types (`types.go`)

Key types:
- `LaunchAgentRequest`: prompt, source (repo + ref), target (branch + auto-PR), model, webhook
- `Agent`: response object with ID, status, source, target (includes PrURL), summary
- `Prompt`: text + optional images (base64 data + dimensions)
- `Image`: base64 data string + width/height dimensions

## Testing Pattern

Tests use `httptest.NewServer` with custom handlers to mock the Cursor API:

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    assert.Equal(t, http.MethodPost, r.Method)
    assert.Equal(t, "/v0/agents", r.URL.Path)
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(expectedResponse)
}))
defer server.Close()

client := NewClientWithBaseURL("test-api-key", server.URL)
```

Tests cover: all 9 API methods, error handling (401, 403, 404), retry on 429/500, retry exhaustion, context cancellation, Basic Auth header format, non-JSON error responses.

## Common Pitfalls

- **Basic Auth, not Bearer**: The client uses `SetBasicAuth(apiKey, "")`, not an `Authorization: Bearer` header.
- **Context required**: All methods require a `context.Context`. Always use `context.WithTimeout` to avoid hanging requests.
- **RawBody for debugging**: When an API error message is unhelpful, check `apiErr.RawBody` for the full response.
- **ListAgents pagination**: Uses cursor-based pagination (`cursor` parameter), not page numbers.
