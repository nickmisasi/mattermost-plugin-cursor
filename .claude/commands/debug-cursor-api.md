---
description: "Investigate and resolve Cursor API issues: enable debug logging, interpret log output, diagnose common HTTP errors, test with curl, and check plugin health."
---

# Debug Cursor API Issues

This skill covers how to investigate and resolve issues with the Cursor Background Agents API.

## Step 1: Enable Debug Logging

In the Mattermost System Console:

1. Go to **Plugins > Cursor Background Agents**
2. Set **Enable Debug Logging** to `true`
3. Click **Save**

This enables the `logDebug` conditional logger defined in `server/plugin.go`:

```go
func (p *Plugin) logDebug(msg string, keyValuePairs ...interface{}) {
    if p.getConfiguration().EnableDebugLogging {
        p.API.LogDebug(msg, keyValuePairs...)
    }
}
```

The Cursor API client also receives this logger via `cursor.WithLogger(&pluginLogger{plugin: p})`, which logs detailed request/response information.

## Step 2: View Logs

Use the Makefile target to tail logs in real time:

```bash
make logs-watch
```

This tails the Mattermost server log with grep for the plugin ID.

Alternatively, check the Mattermost log file directly (typically at `logs/mattermost.log` in your Mattermost installation directory).

## Step 3: Understand the Debug Log Points

When debug logging is enabled, the Cursor API client (`server/cursor/client.go`) logs at these points in the `doRequest` method:

### Request Logging

```
"Cursor API request"     method=POST  url=https://api.cursor.com/v0/agents  has_body=true
"Cursor API request body" method=POST  url=https://api.cursor.com/v0/agents  body={...json...}
```

### Response Logging (Success)

```
"Cursor API response"      method=POST  url=https://api.cursor.com/v0/agents  status=200  body_length=234
"Cursor API response body"  method=POST  url=https://api.cursor.com/v0/agents  body={...json...}
```

### Response Logging (Error)

```
"Cursor API error response"  method=POST  url=https://api.cursor.com/v0/agents  status=401  message=Invalid API key  raw_body={...}
```

### Retry Logging

```
"Cursor API retry"  attempt=1  max_retries=3  delay=1s  method=POST  url=https://api.cursor.com/v0/agents
```

### Transport Error Logging

```
"Cursor API transport error"  method=POST  url=https://api.cursor.com/v0/agents  attempt=0  error=dial tcp: lookup api.cursor.com: no such host
```

### Handler-Level Logging

The `handlers.go` file also logs at key points:

```
"Bot mention detected"          post_id=xxx  channel_id=xxx  user_id=xxx
"Parsed mention"                prompt_length=42  repository=org/repo  branch=main
"Resolved defaults"             repo=org/repo  branch=main  model=auto
"Agent launch prompt prepared"  prompt_length=500  image_count=0
"LaunchAgent request"           source_repository=https://github.com/org/repo  target_branch=cursor/fix-bug
```

The poller (`server/poller.go`) also logs:

```
"Polling agent status"  agent_id=abc123  current_status=RUNNING
"Polled agent status"   agent_id=abc123  stored_status=RUNNING  api_status=FINISHED  pr_url=https://...
```

## Step 4: Common Errors and What They Mean

### HTTP 400 -- Bad Request

**Symptom:** `:x: **Failed to launch agent**` with a JSON error body.

**Causes:**
- Invalid repository URL format
- Missing required fields in the launch request
- Invalid model name

**Debug:** Check the `"Cursor API request body"` log to see exactly what was sent. Compare with the [Cursor API docs](https://api.cursor.com).

### HTTP 401 -- Unauthorized

**Symptom:** `cursor API error (HTTP 401): Invalid API key`

**Causes:**
- API key is incorrect or expired
- API key was copy-pasted with leading/trailing whitespace

**Fix:**
1. Go to [cursor.com/dashboard](https://cursor.com/dashboard) -> Integrations
2. Generate a new API key
3. Update it in System Console > Plugins > Cursor Background Agents > Cursor API Key

**Verification:** The plugin validates the key on configuration change by calling `GET /v0/me`. Check logs for:
```
"Cursor API key validation failed"  error=...  hint=Check that your API key is correct
```
or
```
"Cursor API key validated successfully"
```

### HTTP 403 -- Forbidden

**Symptom:** `cursor API error (HTTP 403): Forbidden`

**Causes:**
- API key does not have permission for the requested operation
- Repository access not granted to the Cursor account

**Debug:** Check if the API key user has access to the repository in question.

### HTTP 429 -- Rate Limited

**Symptom:** Requests fail after retries.

The client automatically retries 429 responses with exponential backoff (1s, 2s, 4s). After 3 retries, it gives up with:
```
"request failed after 3 retries"
```

**Fix:** Increase the poll interval in System Console to reduce API calls. The minimum is 10 seconds; the default is 30.

### HTTP 500/502/503 -- Server Error

The client retries these automatically, same as 429. If the Cursor API is down, all operations will fail until it recovers.

## Step 5: The APIError.RawBody Fallback

When the Cursor API returns a non-standard error response, the client captures the raw response body in `APIError.RawBody`. The `formatAPIError` function in both `server/handlers.go` and `server/command/command.go` uses this to display the raw error:

```go
func formatAPIError(action string, err error) string {
    var apiErr *cursor.APIError
    if errors.As(err, &apiErr) && apiErr.RawBody != "" && strings.HasPrefix(strings.TrimSpace(apiErr.RawBody), "{") {
        // Pretty-print JSON in a code block
        var prettyJSON bytes.Buffer
        if jsonErr := json.Indent(&prettyJSON, []byte(apiErr.RawBody), "", "  "); jsonErr == nil {
            return fmt.Sprintf(":x: **%s**\n\nError details:\n```json\n%s\n```", action, prettyJSON.String())
        }
        return fmt.Sprintf(":x: **%s**\n\nError details:\n```\n%s\n```", action, apiErr.RawBody)
    }
    return fmt.Sprintf(":x: **%s**\n\n%s", action, err.Error())
}
```

This means even when the API returns unexpected HTML or plain text, the user sees something useful.

## Step 6: Test API Calls Manually with curl

The Cursor API uses Basic Auth with the API key as the username and empty password.

### Test API Key

```bash
export CURSOR_API_KEY="your_key_here"
curl -u "$CURSOR_API_KEY:" https://api.cursor.com/v0/me
```

### List Agents

```bash
curl -u "$CURSOR_API_KEY:" "https://api.cursor.com/v0/agents?limit=10"
```

### Get Agent Status

```bash
curl -u "$CURSOR_API_KEY:" https://api.cursor.com/v0/agents/AGENT_ID
```

### Launch Agent

```bash
curl -u "$CURSOR_API_KEY:" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": {"text": "Fix the login bug"},
    "source": {"repository": "https://github.com/owner/repo", "ref": "main"},
    "target": {"branchName": "cursor/fix-login", "autoCreatePr": true},
    "model": "auto"
  }' \
  https://api.cursor.com/v0/agents
```

### List Models

```bash
curl -u "$CURSOR_API_KEY:" https://api.cursor.com/v0/models
```

### Stop Agent

```bash
curl -u "$CURSOR_API_KEY:" -X POST https://api.cursor.com/v0/agents/AGENT_ID/stop
```

## Step 7: Check Plugin Health

The plugin exposes a health endpoint (admin-only):

```bash
curl -H "Mattermost-User-ID: YOUR_USER_ID" \
  http://localhost:8065/plugins/com.mattermost.plugin-cursor/api/v1/admin/health
```

This returns:
```json
{
  "healthy": true,
  "cursor_api": {"ok": true},
  "active_agent_count": 2,
  "configuration": {"ok": true},
  "plugin_version": "0.1.0"
}
```

If `cursor_api.ok` is false, the message will explain why (e.g., "Cursor API unreachable: ...").

## Quick Troubleshooting Flowchart

1. **Plugin not responding at all?**
   - Check System Console > Plugins -- is it enabled?
   - Check `make logs-watch` for activation errors

2. **"Cursor API key is not configured"?**
   - API key is empty in System Console
   - Set it and save

3. **Agent launch fails?**
   - Enable debug logging
   - Check the request body in logs
   - Try the same request with curl
   - Check repository URL format (must be `https://github.com/owner/repo`)

4. **Status polling not working?**
   - Check `PollIntervalSeconds` (minimum 10)
   - Look for `"Polling agent statuses"` in logs
   - Verify active agents exist with `/cursor list`

5. **Webhooks not working?**
   - Check `GitHubWebhookSecret` is set and matches GitHub config
   - Look for `"GitHub webhook signature verification failed"` in logs
   - Test with ping event from GitHub webhook settings
