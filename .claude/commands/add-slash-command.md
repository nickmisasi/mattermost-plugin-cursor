---
description: "Walk through adding a new subcommand to the /cursor slash command system, including dispatch routing, handler implementation, autocomplete, help text, and tests."
---

# Add a New `/cursor` Subcommand

This skill walks you through adding a new subcommand to the `/cursor` slash command system.

## Overview

The command system lives in `server/command/command.go` and follows a dependency-injection pattern. The `Handler` struct receives all external dependencies via a `Dependencies` struct, and command dispatch happens in `Handle()` via a simple `switch` statement.

## Step 1: Define the Subcommand Constant

In `server/command/command.go`, add a new constant alongside the existing ones:

```go
const (
    // ... existing constants ...
    subcommandList     = "list"
    subcommandStatus   = "status"
    subcommandCancel   = "cancel"
    subcommandSettings = "settings"
    subcommandModels   = "models"
    subcommandHelp     = "help"
    subcommandYourNew  = "yournew"  // <-- ADD HERE
)
```

## Step 2: Add the Case to the Dispatch Switch

In the `Handle` method, add a new case to the switch statement:

```go
func (h *Handler) Handle(args *model.CommandArgs) (*model.CommandResponse, error) {
    fields := strings.Fields(args.Command)

    if len(fields) < 2 {
        return h.executeHelp(), nil
    }

    subcommand := strings.ToLower(fields[1])

    switch subcommand {
    // ... existing cases ...
    case subcommandYourNew:
        return h.executeYourNew(args, fields[2:])
    default:
        return h.executeLaunch(args)
    }
}
```

Note: The `default` case falls through to `executeLaunch` -- any unrecognized subcommand is treated as a prompt to launch a new agent. Your new case MUST come before `default`.

## Step 3: Implement the Handler Method

Follow the established handler function signature pattern. There are two patterns used:

**Pattern A: No extra args (like `list`, `models`, `settings`)**
```go
func (h *Handler) executeYourNew(args *model.CommandArgs) (*model.CommandResponse, error) {
```

**Pattern B: With extra args (like `status <id>`, `cancel <id>`)**
```go
func (h *Handler) executeYourNew(args *model.CommandArgs, params []string) (*model.CommandResponse, error) {
```

### Nil CursorClient Check

If your subcommand interacts with the Cursor API, add this check at the very top of your handler. This handles the case where the admin has not configured an API key:

```go
func (h *Handler) executeYourNew(args *model.CommandArgs, params []string) (*model.CommandResponse, error) {
    if h.deps.CursorClient == nil {
        return ephemeralResponse(errNoCursorClient), nil
    }
    // ... rest of handler ...
}
```

### Returning Ephemeral Responses

Always use the `ephemeralResponse()` helper for user-only messages:

```go
return ephemeralResponse("Your message here"), nil
```

For "success with no visible message" (like when opening a dialog), return an empty response:

```go
return &model.CommandResponse{}, nil
```

### Interacting with the Cursor Client

Use `h.deps.CursorClient` with a context timeout:

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

agent, err := h.deps.CursorClient.GetAgent(ctx, agentID)
if err != nil {
    return ephemeralResponse(formatAPIError("Failed to get agent", err)), nil
}
```

The `formatAPIError` helper formats `cursor.APIError` responses with pretty-printed JSON in code blocks.

### Interacting with the KV Store

Use `h.deps.Store` to read/write plugin state:

```go
record, err := h.deps.Store.GetAgent(agentID)
if err != nil || record == nil {
    return ephemeralResponse(fmt.Sprintf("Agent `%s` not found.", agentID)), nil
}
```

### Posting as the Bot

Use `h.deps.Client.Post.CreatePost` with `h.deps.BotUserID`:

```go
botPost := &model.Post{
    UserId:    h.deps.BotUserID,
    ChannelId: args.ChannelId,
    RootId:    someRootPostID,  // for thread replies
    Message:   "Bot message here",
}
if err := h.deps.Client.Post.CreatePost(botPost); err != nil {
    return ephemeralResponse("Failed to post message."), nil
}
```

## Step 4: Register Autocomplete Data

In the `getAutocompleteData()` function in `server/command/command.go`, add your subcommand:

```go
func getAutocompleteData() *model.AutocompleteData {
    ac := model.NewAutocompleteData(
        CommandTrigger,
        "[subcommand]",
        "Launch and manage Cursor Background Agents",
    )

    // ... existing subcommands ...

    yournew := model.NewAutocompleteData(subcommandYourNew, "<argHint>", "Description of your subcommand")
    yournew.AddTextArgument("Description of the argument", "[argHint]", "")  // only if it takes args
    ac.AddCommand(yournew)

    return ac
}
```

## Step 5: Update the Help Text

In `executeHelp()`, add your new command to the appropriate section:

```go
func (h *Handler) executeHelp() *model.CommandResponse {
    helpText := `#### Cursor Background Agents - Help
...
**Management:**
` + "- `/cursor yournew <arg>` - Description of your command" + `
...`
```

Note the backtick concatenation pattern -- this is because Go raw string literals cannot contain backticks, so inline code formatting uses string concatenation.

## Step 6: Add Tests

In `server/command/command_test.go`, add tests using the established `setupTest` pattern:

```go
func TestYourNew_Success(t *testing.T) {
    env := setupTest(t)

    // Set up mock expectations
    env.store.On("GetAgent", "abc123").Return(&kvstore.AgentRecord{
        CursorAgentID: "abc123",
        UserID:        "user-1",
        Status:        "RUNNING",
    }, nil)

    resp, err := env.handler.Handle(&model.CommandArgs{
        Command:   "/cursor yournew abc123",
        UserId:    "user-1",
        ChannelId: "ch-1",
    })

    require.NoError(t, err)
    assert.Contains(t, resp.Text, "expected text")
}
```

### Testing with Nil CursorClient

Add a nil-client test using `setupTestNilClient`:

```go
func TestNilCursorClient_YourNew(t *testing.T) {
    env := setupTestNilClient(t)

    resp, err := env.handler.Handle(&model.CommandArgs{
        Command: "/cursor yournew abc123",
    })

    require.NoError(t, err)
    assert.Contains(t, resp.Text, "Cursor API key is not configured")
}
```

### Test Environment

The `setupTest` function in `command_test.go` creates:
- `env.handler` -- the command handler to test
- `env.api` -- `*plugintest.API` mock for Mattermost API calls
- `env.cursorClient` -- `*mockCursorClient` for Cursor API calls
- `env.store` -- `*mockKVStore` for KV store operations

All log methods are pre-mocked with `.Maybe()`.

## Checklist

- [ ] Added subcommand constant in `server/command/command.go`
- [ ] Added case to the `switch` in `Handle()`
- [ ] Implemented handler method with correct signature
- [ ] Added nil CursorClient check (if it uses Cursor API)
- [ ] Registered autocomplete in `getAutocompleteData()`
- [ ] Updated help text in `executeHelp()`
- [ ] Added success test case
- [ ] Added error/edge-case test cases
- [ ] Added nil-client test case (if applicable)
- [ ] Run `go test ./server/command/...` to verify
