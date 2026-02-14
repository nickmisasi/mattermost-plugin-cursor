---
description: "Walk through adding a new HTTP endpoint to the plugin's REST API, including route registration, handler implementation, webapp client method, and tests."
---

# Add a New REST API Endpoint

This skill walks you through adding a new HTTP endpoint to the plugin's REST API.

## Overview

The HTTP router is set up in `server/api.go` using gorilla/mux. There are three subrouters:

1. **Root router** -- No auth. Used for the GitHub webhook endpoint.
2. **`authedRouter`** -- Requires a logged-in Mattermost user (via `Mattermost-User-ID` header). Used for all frontend-facing endpoints.
3. **`adminRouter`** -- Requires both a logged-in user AND system admin privileges. Nested under `/api/v1/admin/`.

## Step 1: Add the Route

In `server/api.go`, add your route to the appropriate subrouter in `initRouter()`:

```go
func (p *Plugin) initRouter() *mux.Router {
    router := mux.NewRouter()

    // GitHub webhook -- NO auth
    router.HandleFunc("/api/v1/webhooks/github", p.handleGitHubWebhook).Methods(http.MethodPost)

    // Authenticated routes
    authedRouter := router.PathPrefix("/api/v1").Subrouter()
    authedRouter.Use(p.MattermostAuthorizationRequired)

    // ... existing routes ...
    authedRouter.HandleFunc("/your-endpoint", p.handleYourEndpoint).Methods(http.MethodGet)  // <-- ADD HERE

    // Admin-only routes
    adminRouter := authedRouter.PathPrefix("/admin").Subrouter()
    adminRouter.Use(p.RequireSystemAdmin)
    adminRouter.HandleFunc("/your-admin-endpoint", p.handleYourAdminEndpoint).Methods(http.MethodGet)  // <-- OR HERE

    return router
}
```

### URL Path Conventions

- All API routes are under `/api/v1/`
- Resource-based: `/api/v1/agents`, `/api/v1/agents/{id}`
- Admin routes: `/api/v1/admin/health`
- Dialog callbacks: `/api/v1/dialog/settings`
- Webhooks: `/api/v1/webhooks/github`

### URL Parameters

Use `mux.Vars(r)` to extract path parameters:

```go
agentID := mux.Vars(r)["id"]
```

## Step 2: Implement the Handler

Follow the established handler pattern in `server/api.go`:

```go
func (p *Plugin) handleYourEndpoint(w http.ResponseWriter, r *http.Request) {
    // 1. Extract the user ID (guaranteed non-empty by auth middleware)
    userID := r.Header.Get("Mattermost-User-ID")

    // 2. Extract path/query parameters
    agentID := mux.Vars(r)["id"]  // for path params
    limit := r.URL.Query().Get("limit")  // for query params

    // 3. Parse request body (for POST/PUT)
    var reqBody YourRequestBody
    if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    // 4. Validate input
    if reqBody.Message == "" {
        http.Error(w, "Message is required", http.StatusBadRequest)
        return
    }

    // 5. Call KV store / Cursor client
    record, err := p.kvstore.GetAgent(agentID)
    if err != nil {
        p.API.LogError("Failed to get agent", "agentID", agentID, "error", err.Error())
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    // 6. Authorization: verify the user owns the resource
    if record == nil || record.UserID != userID {
        http.Error(w, "Agent not found", http.StatusNotFound)
        return
    }

    // 7. Call Cursor API if needed
    cursorClient := p.getCursorClient()
    if cursorClient == nil {
        http.Error(w, "Cursor client not configured", http.StatusBadGateway)
        return
    }

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    result, apiErr := cursorClient.SomeMethod(ctx, agentID)
    if apiErr != nil {
        p.API.LogError("Cursor API call failed", "error", apiErr.Error())
        http.Error(w, "Cursor API error", http.StatusBadGateway)
        return
    }

    // 8. Return JSON response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(YourResponse{Field: result.Field})
}
```

### Response Type Structs

Define response types in `server/api.go` alongside the existing ones:

```go
type YourResponse struct {
    Field string `json:"field"`
}
```

### Posting WebSocket Events

If your endpoint changes state that the webapp needs to know about, publish a WebSocket event:

```go
p.publishAgentStatusChange(record)
```

Or create a custom event:

```go
p.API.PublishWebSocketEvent(
    "your_event_name",
    map[string]interface{}{
        "key": "value",
    },
    &model.WebsocketBroadcast{UserId: record.UserID},
)
```

## Step 3: Add the Webapp Client Method

In `webapp/src/client.ts`, add a corresponding method:

```typescript
class ClientClass {
    // ... existing methods ...

    yourMethod = async (agentId: string): Promise<YourResponse> => {
        const url = `${pluginApiBase}/your-endpoint/${encodeURIComponent(agentId)}`;
        const response = await fetch(url, Client4.getOptions({
            method: 'GET',
        }));
        if (!response.ok) {
            throw new Error(`GET /your-endpoint/${agentId} failed: ${response.status}`);
        }
        return response.json();
    };
}
```

Key pattern: Always use `Client4.getOptions({...})` to get the correct auth headers. Always use `encodeURIComponent()` for dynamic path segments.

### For POST/PUT/DELETE with a Body

```typescript
yourPostMethod = async (agentId: string, data: YourRequest): Promise<StatusResponse> => {
    const url = `${pluginApiBase}/agents/${encodeURIComponent(agentId)}/your-action`;
    const response = await fetch(url, Client4.getOptions({
        method: 'POST',
        body: JSON.stringify(data),
    }));
    if (!response.ok) {
        throw new Error(`POST failed: ${response.status}`);
    }
    return response.json();
};
```

### Add Types

If your endpoint has new request/response shapes, add them to `webapp/src/types.ts`:

```typescript
export interface YourResponse {
    field: string;
}
```

## Step 4: Add Tests

In `server/api_test.go`, add tests using the established `setupAPITestPlugin` pattern:

```go
func TestYourEndpoint_Success(t *testing.T) {
    p, api, cursorClient, store := setupAPITestPlugin(t)

    // Set up mocks
    store.On("GetAgent", "agent-1").Return(&kvstore.AgentRecord{
        CursorAgentID: "agent-1",
        UserID:        "user-1",
        Status:        "RUNNING",
    }, nil)

    // Make the request
    rr := doRequest(p, http.MethodGet, "/api/v1/your-endpoint/agent-1", nil, "user-1")

    // Assert
    assert.Equal(t, http.StatusOK, rr.Code)
    var resp YourResponse
    require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
    assert.Equal(t, "expected", resp.Field)
}
```

### The `doRequest` Helper

```go
func doRequest(p *Plugin, method, path string, body interface{}, userID string) *httptest.ResponseRecorder
```

- `body` can be any JSON-serializable struct or `nil`
- `userID` is set as the `Mattermost-User-ID` header; pass `""` to test unauthenticated access

### Standard Test Cases to Cover

```go
func TestYourEndpoint_Unauthorized(t *testing.T) {
    p, _, _, _ := setupAPITestPlugin(t)
    rr := doRequest(p, http.MethodGet, "/api/v1/your-endpoint", nil, "")
    assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestYourEndpoint_NotFound(t *testing.T) {
    // ...
}

func TestYourEndpoint_WrongUser(t *testing.T) {
    // ...
}

func TestYourEndpoint_StoreError(t *testing.T) {
    // ...
}
```

## Checklist

- [ ] Added route in `initRouter()` in `server/api.go` to the correct subrouter
- [ ] Implemented handler with user extraction, input validation, auth, and JSON response
- [ ] Defined any new request/response structs
- [ ] Added the webapp client method in `webapp/src/client.ts`
- [ ] Added any new types in `webapp/src/types.ts`
- [ ] Added success test case in `server/api_test.go`
- [ ] Added unauthorized, not-found, wrong-user, and error test cases
- [ ] If the endpoint publishes WebSocket events, update `webapp/src/websocket.ts` and `webapp/src/actions.ts`
- [ ] Run `go test ./server/...` to verify
