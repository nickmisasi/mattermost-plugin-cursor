# KV Store

## Design

Interface-based design in `kvstore/kvstore.go`. Implementation in `kvstore/store.go`. Uses `pluginapi.Client` KV methods (wraps the raw `p.API.KVSet`/`KVGet`).

## Interface: `KVStore`

```go
type KVStore interface {
    // Agent CRUD
    GetAgent(cursorAgentID string) (*AgentRecord, error)
    SaveAgent(record *AgentRecord) error
    DeleteAgent(cursorAgentID string) error
    ListActiveAgents() ([]*AgentRecord, error)
    GetAgentsByUser(userID string) ([]*AgentRecord, error)

    // Agent lookup by PR URL or branch (GitHub webhook support)
    GetAgentByPRURL(prURL string) (*AgentRecord, error)
    GetAgentByBranch(branchName string) (*AgentRecord, error)

    // Thread-to-agent mapping
    GetAgentIDByThread(rootPostID string) (string, error)
    SetThreadAgent(rootPostID string, cursorAgentID string) error
    DeleteThreadAgent(rootPostID string) error

    // Channel/user settings
    GetChannelSettings(channelID string) (*ChannelSettings, error)
    SaveChannelSettings(channelID string, settings *ChannelSettings) error
    GetUserSettings(userID string) (*UserSettings, error)
    SaveUserSettings(userID string, settings *UserSettings) error

    // Idempotency for GitHub webhooks
    HasDeliveryBeenProcessed(deliveryID string) (bool, error)
    MarkDeliveryProcessed(deliveryID string) error
}
```

## Key Prefix Conventions

All KV keys use a prefix to namespace data:

| Prefix | Format | Purpose |
|--------|--------|---------|
| `agent:` | `agent:{cursorAgentID}` | Primary agent record storage |
| `thread:` | `thread:{rootPostID}` | Maps thread root post -> cursor agent ID |
| `channel:` | `channel:{channelID}` | Per-channel default settings |
| `user:` | `user:{userID}` | Per-user default settings |
| `agentidx:` | `agentidx:{cursorAgentID}` | Index of active (non-terminal) agents |
| `useragentidx:` | `useragentidx:{userID}:{cursorAgentID}` | Per-user agent index |
| `prurlidx:` | `prurlidx:{normalizedPRURL}` | PR URL -> agent ID lookup |
| `branchidx:` | `branchidx:{branchName}` | Branch name -> agent ID lookup |
| `ghdelivery:` | `ghdelivery:{deliveryID}` | GitHub webhook deduplication (24h TTL) |

## AgentRecord Fields

```go
type AgentRecord struct {
    CursorAgentID string  // Cursor API agent ID (primary key)
    PostID        string  // Bot's reply post / thread root
    TriggerPostID string  // User's original @mention post (for reactions)
    ChannelID     string  // Mattermost channel
    UserID        string  // Mattermost user who launched the agent
    Status        string  // CREATING, RUNNING, FINISHED, FAILED, STOPPED, MERGED, PR_CLOSED
    Repository    string  // GitHub repo (owner/repo format)
    Branch        string  // Base branch
    TargetBranch  string  // Cursor-created branch (e.g., "cursor/fix-login")
    PrURL         string  // Pull request URL (set when agent finishes)
    Prompt        string  // Original user prompt
    Model         string  // AI model used
    Summary       string  // Agent completion summary
    CreatedAt     int64   // Unix milliseconds
    UpdatedAt     int64   // Unix milliseconds
}
```

## Active Agent Indexing

The poller needs to efficiently find agents to check. The store maintains an `agentidx:` index:
- On `SaveAgent()` with CREATING or RUNNING status: writes `agentidx:{id}` key
- On `SaveAgent()` with terminal status (FINISHED, FAILED, STOPPED): deletes `agentidx:{id}` key
- `ListActiveAgents()` lists all keys with `agentidx:` prefix, then fetches full records

Similarly, `useragentidx:` enables `GetAgentsByUser()` for the `/cursor list` command and the webapp REST API.

## PR URL and Branch Indexes

For GitHub webhook support, the store maintains two additional indexes:
- `prurlidx:` maps a normalized PR URL to an agent ID (URLs are normalized by stripping trailing slashes)
- `branchidx:` maps a target branch name to an agent ID

These are updated automatically in `SaveAgent()` when `PrURL` or `TargetBranch` fields are set.

## Testing

Tests in `kvstore/store_test.go` use `plugintest.API` mocks with `pluginapi.NewClient`.

Key helpers:
- `setupStore(t)` -- creates mock API + store instance
- `mustJSON(t, v)` -- marshals to JSON bytes for mock expectations
- `mockKVSet(api, key, value)` -- sets up `KVSetWithOptions` mock
- `mockKVDelete(api, key)` -- sets up delete mock (Set with nil value)

Note: `pluginapi` uses `KVSetWithOptions` internally, so mocks must match that signature, NOT `KVSet`.

## Updating the Interface

When adding a new method to `KVStore`:

1. Add the method to the interface in `kvstore/kvstore.go`
2. Implement it in `kvstore/store.go`
3. Add tests in `kvstore/store_test.go`
4. **Update mock implementations in ALL test files**:
   - `server/command/command_test.go` (`mockKVStore` struct)
   - Any new test files that mock `KVStore`

## Common Pitfalls

- **pluginapi.Client, not raw API**: The store uses `s.client.KV.Get()` / `s.client.KV.Set()`, not `s.API.KVGet()` / `s.API.KVSet()` directly.
- **Nil vs empty**: `GetAgent()` returns `(nil, nil)` when the key does not exist (empty struct has empty `CursorAgentID`).
- **Index cleanup**: `DeleteAgent()` cleans up `agentidx:` and `useragentidx:` entries. If you add new indexes, add cleanup logic here too.
- **TTL on deliveries**: `MarkDeliveryProcessed` sets a 24-hour TTL via `pluginapi.SetExpiry`. This is the only key with a TTL.
- **Mock signatures**: The `KVList` mock expects `(page int, count int)` arguments. The `KVSetWithOptions` mock expects `(key, value, options)`.
