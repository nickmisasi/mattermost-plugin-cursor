# Handle a New GitHub Webhook Event Type

This skill walks you through adding support for a new GitHub webhook event (e.g., `check_run`, `issue_comment`, `push`).

## Overview

GitHub webhook handling lives in `server/webhook.go`. The flow is:

1. `handleGitHubWebhook` reads the body, verifies the HMAC-SHA256 signature, checks for duplicate deliveries, and routes by event type.
2. Each event type has its own handler function (e.g., `handlePullRequestEvent`, `handlePullRequestReviewEvent`).
3. Agent lookup uses `findAgentForPR()` which tries PR URL first, then branch name.
4. Notifications are posted to the agent's Mattermost thread via `postThreadNotification()`.

HMAC verification is already handled in the main `handleGitHubWebhook` function -- you do NOT need to verify signatures in your event handler.

## Step 1: Define the Event Type Constant

In `server/webhook.go`, add the event type string constant:

```go
const (
    // ... existing constants ...
    eventPullRequest       = "pull_request"
    eventPullRequestReview = "pull_request_review"
    eventPing              = "ping"
    eventCheckRun          = "check_run"  // <-- ADD HERE
)
```

The constant value must match exactly what GitHub sends in the `X-GitHub-Event` header.

## Step 2: Define the Event Payload Struct

Add a struct matching the GitHub webhook payload structure. Only include fields you actually need:

```go
// CheckRunEvent is the GitHub webhook payload for check_run events.
type CheckRunEvent struct {
    Action   string `json:"action"`
    CheckRun struct {
        Name       string `json:"name"`
        Status     string `json:"status"`
        Conclusion string `json:"conclusion"`
        HTMLURL    string `json:"html_url"`
    } `json:"check_run"`
    Repository ghRepository `json:"repository"`
    Sender     ghSender     `json:"sender"`
}
```

Reuse existing shared types where possible:
- `ghPullRequest` -- PR fields (number, html_url, title, state, merged, head.ref, base.ref, user.login)
- `ghReview` -- Review fields (state, body, html_url, user.login)
- `ghRepository` -- Repo fields (full_name, html_url)
- `ghSender` -- Sender fields (login)

## Step 3: Add the Case to the Event Router

In `handleGitHubWebhook`, add your event type to the switch statement:

```go
switch eventType {
case eventPing:
    p.handlePingEvent(w, body)
case eventPullRequest:
    p.handlePullRequestEvent(w, body)
case eventPullRequestReview:
    p.handlePullRequestReviewEvent(w, body)
case eventCheckRun:
    p.handleCheckRunEvent(w, body)  // <-- ADD HERE
default:
    p.API.LogDebug("Ignoring unhandled GitHub event type", "event", eventType)
    w.WriteHeader(http.StatusOK)
}
```

## Step 4: Implement the Event Handler

Follow the established pattern:

```go
func (p *Plugin) handleCheckRunEvent(w http.ResponseWriter, body []byte) {
    // 1. Unmarshal the payload
    var event CheckRunEvent
    if err := json.Unmarshal(body, &event); err != nil {
        p.API.LogWarn("Failed to parse check_run event", "error", err.Error())
        http.Error(w, "invalid payload", http.StatusBadRequest)
        return
    }

    // 2. Filter by action (only handle relevant actions)
    if event.Action != "completed" {
        w.WriteHeader(http.StatusOK)
        return
    }

    // 3. Look up the associated agent
    // If the event involves a PR, use findAgentForPR():
    agent := p.findAgentForPR(event.PullRequest)
    if agent == nil {
        p.API.LogDebug("No agent found for check run", "repo", event.Repository.FullName)
        w.WriteHeader(http.StatusOK)
        return
    }

    // 4. Build the notification message
    var message string
    if event.CheckRun.Conclusion == "failure" {
        message = fmt.Sprintf(
            ":x: **CI check `%s` failed** on [%s](%s)",
            event.CheckRun.Name,
            event.Repository.FullName,
            event.CheckRun.HTMLURL,
        )
    } else {
        message = fmt.Sprintf(
            ":white_check_mark: **CI check `%s` passed**",
            event.CheckRun.Name,
        )
    }

    // 5. Post to the agent's thread
    p.postThreadNotification(agent, message)

    // 6. Optionally update agent record
    // agent.Status = "SOME_STATUS"
    // _ = p.kvstore.SaveAgent(agent)

    w.WriteHeader(http.StatusOK)
}
```

### Agent Lookup Patterns

The `findAgentForPR` function tries two strategies in order:

1. **By PR URL** -- `p.kvstore.GetAgentByPRURL(pr.HTMLURL)` -- exact match on the PR's HTML URL
2. **By branch name** -- `p.kvstore.GetAgentByBranch(pr.Head.Ref)` -- matches the head branch

If your event does not have a PR (e.g., a push event), you may need to look up by branch directly:

```go
agent, err := p.kvstore.GetAgentByBranch(branchName)
if err != nil || agent == nil {
    w.WriteHeader(http.StatusOK)
    return
}
```

### Thread Notifications

Use the `postThreadNotification` helper to post a message in the agent's Mattermost thread:

```go
p.postThreadNotification(agent, message)
```

This posts as the bot user in the agent's thread (using `agent.PostID` as the root).

### Reaction Swaps

Use `swapReaction` to change the emoji on the trigger post:

```go
p.swapReaction(agent.TriggerPostID, "white_check_mark", "rocket")
```

## Step 5: Add Tests

In `server/webhook_test.go`, add tests following the established patterns.

### Test Setup

```go
func TestWebhook_CheckRunCompleted(t *testing.T) {
    p, store := setupWebhookTestPlugin(t)
    api := p.API.(*mockPluginAPI)
```

The `setupWebhookTestPlugin` function creates a plugin with `GitHubWebhookSecret` set to `testWebhookSecret`.

### Creating Signed Test Payloads

Always sign your test payload using the `signPayload` helper:

```go
event := CheckRunEvent{
    Action: "completed",
    // ... fill fields ...
}
body, _ := json.Marshal(event)
sig := signPayload(testWebhookSecret, body)
```

### Making Webhook Requests

Use `makeWebhookRequest` to create a properly-formed request:

```go
req := makeWebhookRequest(t, "check_run", "delivery-cr-1", body, sig)
rr := httptest.NewRecorder()

p.handleGitHubWebhook(rr, req)

assert.Equal(t, http.StatusOK, rr.Code)
```

### Mocking Delivery Idempotency

Every test must mock the delivery idempotency check:

```go
store.On("HasDeliveryBeenProcessed", "delivery-cr-1").Return(false, nil)
store.On("MarkDeliveryProcessed", "delivery-cr-1").Return(nil)
```

### Standard Test Cases

1. **Happy path** -- event processed, notification posted
2. **Ignored action** -- event with an action you do not handle (e.g., "created")
3. **No agent found** -- no matching agent in KV store
4. **Duplicate delivery** -- delivery ID already processed

Example ignored-action test:

```go
func TestWebhook_CheckRun_CreatedIgnored(t *testing.T) {
    p, store := setupWebhookTestPlugin(t)

    event := CheckRunEvent{Action: "created"}
    body, _ := json.Marshal(event)
    sig := signPayload(testWebhookSecret, body)

    store.On("HasDeliveryBeenProcessed", "delivery-cr-created").Return(false, nil)
    store.On("MarkDeliveryProcessed", "delivery-cr-created").Return(nil)

    req := makeWebhookRequest(t, "check_run", "delivery-cr-created", body, sig)
    rr := httptest.NewRecorder()
    p.handleGitHubWebhook(rr, req)

    assert.Equal(t, http.StatusOK, rr.Code)
    store.AssertNotCalled(t, "GetAgentByPRURL")
}
```

## Checklist

- [ ] Added event type constant in `server/webhook.go`
- [ ] Defined the event payload struct (reuse shared types)
- [ ] Added case to the event router switch in `handleGitHubWebhook`
- [ ] Implemented the event handler following the parse-filter-lookup-notify pattern
- [ ] Added happy-path test with signed payload
- [ ] Added ignored-action test
- [ ] Added no-agent-found test
- [ ] Added duplicate-delivery test
- [ ] Run `go test ./server/...` to verify
