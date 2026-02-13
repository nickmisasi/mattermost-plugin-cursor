# Add a New Entity Type to the KV Store

This skill walks you through adding a new entity type to the plugin's KV store layer.

## Overview

The KV store abstraction lives in two files:
- `server/store/kvstore/kvstore.go` -- Interface definition and struct types
- `server/store/kvstore/store.go` -- Implementation using `pluginapi.Client`

The pattern is: define an interface in `kvstore.go`, implement it in `store.go`, and mock it in every test file that uses the store.

## Step 1: Define the Struct

In `server/store/kvstore/kvstore.go`, add your entity struct:

```go
// YourEntity stores information about...
type YourEntity struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    UserID    string `json:"userId"`
    CreatedAt int64  `json:"createdAt"`
}
```

Use `json` tags that match the codebase convention (camelCase).

## Step 2: Add Interface Methods

In `server/store/kvstore/kvstore.go`, add methods to the `KVStore` interface:

```go
type KVStore interface {
    // ... existing methods ...

    // YourEntity
    GetYourEntity(id string) (*YourEntity, error)
    SaveYourEntity(entity *YourEntity) error
    DeleteYourEntity(id string) error
    ListYourEntitiesByUser(userID string) ([]*YourEntity, error)
}
```

## Step 3: Add Key Prefix Constants

In `server/store/kvstore/store.go`, add key prefix constants:

```go
const (
    // ... existing prefixes ...
    prefixAgent        = "agent:"
    prefixThread       = "thread:"
    prefixChannel      = "channel:"
    prefixUser         = "user:"
    prefixAgentIdx     = "agentidx:"
    prefixUserAgentIdx = "useragentidx:"
    prefixPRURLIdx     = "prurlidx:"
    prefixBranchIdx    = "branchidx:"
    prefixDelivery     = "ghdelivery:"
    prefixYourEntity     = "yourentity:"     // <-- ADD HERE
    prefixYourEntityIdx  = "yourentityidx:"  // <-- ADD INDEX PREFIX if you need listing
)
```

### Key Prefix Conventions

- Primary record: `prefixYourEntity + id` -- stores the full struct
- Index for listing: `prefixYourEntityIdx + lookupKey + ":" + id` -- stores just the ID string
- Keep prefixes short but descriptive

## Step 4: Implement the Methods

In `server/store/kvstore/store.go`:

### Get

```go
func (s *store) GetYourEntity(id string) (*YourEntity, error) {
    var entity YourEntity
    err := s.client.KV.Get(prefixYourEntity+id, &entity)
    if err != nil {
        return nil, errors.Wrap(err, "failed to get your entity")
    }
    if entity.ID == "" {
        return nil, nil // Not found
    }
    return &entity, nil
}
```

The "not found" pattern: if the zero value of your primary key field is returned, the record does not exist. Return `(nil, nil)`.

### Save

```go
func (s *store) SaveYourEntity(entity *YourEntity) error {
    _, err := s.client.KV.Set(prefixYourEntity+entity.ID, entity)
    if err != nil {
        return errors.Wrap(err, "failed to save your entity")
    }

    // Maintain index for user-based listing (if needed)
    if entity.UserID != "" {
        key := prefixYourEntityIdx + entity.UserID + ":" + entity.ID
        _, _ = s.client.KV.Set(key, entity.ID)
    }

    return nil
}
```

### Delete

```go
func (s *store) DeleteYourEntity(id string) error {
    // Get record first to clean up indexes
    entity, _ := s.GetYourEntity(id)

    err := s.client.KV.Delete(prefixYourEntity + id)
    if err != nil {
        return errors.Wrap(err, "failed to delete your entity")
    }

    // Clean up index
    if entity != nil && entity.UserID != "" {
        _ = s.client.KV.Delete(prefixYourEntityIdx + entity.UserID + ":" + id)
    }

    return nil
}
```

### List by Index

```go
func (s *store) ListYourEntitiesByUser(userID string) ([]*YourEntity, error) {
    prefix := prefixYourEntityIdx + userID + ":"
    keys, err := s.client.KV.ListKeys(0, 1000, pluginapi.WithPrefix(prefix))
    if err != nil {
        return nil, errors.Wrap(err, "failed to list your entity keys")
    }

    var entities []*YourEntity
    for _, key := range keys {
        entityID := strings.TrimPrefix(key, prefix)
        entity, err := s.GetYourEntity(entityID)
        if err != nil {
            continue
        }
        if entity != nil {
            entities = append(entities, entity)
        }
    }
    return entities, nil
}
```

### TTL / Expiry

For ephemeral data (like delivery idempotency keys), use `pluginapi.SetExpiry`:

```go
_, err := s.client.KV.Set(prefixYourEntity+id, value, pluginapi.SetExpiry(24*time.Hour))
```

## Step 5: Add KV Store Tests

In `server/store/kvstore/store_test.go`, add tests:

```go
func TestYourEntityCRUD(t *testing.T) {
    s, api := setupStore(t)

    entity := &YourEntity{
        ID:     "entity-1",
        Name:   "Test Entity",
        UserID: "user-1",
    }

    // Test Save
    mockKVSet(api, prefixYourEntity+"entity-1", mustJSON(t, entity))
    mockKVSet(api, prefixYourEntityIdx+"user-1:entity-1", mustJSON(t, "entity-1"))

    err := s.SaveYourEntity(entity)
    require.NoError(t, err)

    // Test Get
    api.On("KVGet", prefixYourEntity+"entity-1").Return(mustJSON(t, entity), nil)

    got, err := s.GetYourEntity("entity-1")
    require.NoError(t, err)
    require.NotNil(t, got)
    assert.Equal(t, "entity-1", got.ID)
    assert.Equal(t, "Test Entity", got.Name)
    api.AssertExpectations(t)
}

func TestGetYourEntityNotFound(t *testing.T) {
    s, api := setupStore(t)

    api.On("KVGet", prefixYourEntity+"nonexistent").Return([]byte(nil), nil)

    got, err := s.GetYourEntity("nonexistent")
    require.NoError(t, err)
    assert.Nil(t, got)
}
```

### Test Helpers

- `setupStore(t)` -- Creates a `*store` with a mocked `plugintest.API`
- `mustJSON(t, v)` -- JSON-marshals a value (fails the test on error)
- `mockKVSet(api, key, value)` -- Mocks `KVSetWithOptions` for a Set call
- `mockKVDelete(api, key)` -- Mocks `KVSetWithOptions` for a Delete call (sets nil bytes)

## Step 6: Update Mock Implementations

This is the most labor-intensive step. Every test file that uses the `mockKVStore` struct must be updated.

### Files with mockKVStore to update:

1. **`server/command/command_test.go`** -- The mock in the command package
2. **`server/handlers_test.go`** -- The mock in the main server package
3. **`server/api_test.go`** -- Uses the mock from `handlers_test.go` (same package)

In each file, add the new methods to the `mockKVStore` struct:

```go
func (m *mockKVStore) GetYourEntity(id string) (*kvstore.YourEntity, error) {
    args := m.Called(id)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*kvstore.YourEntity), args.Error(1)
}

func (m *mockKVStore) SaveYourEntity(entity *kvstore.YourEntity) error {
    return m.Called(entity).Error(0)
}

func (m *mockKVStore) DeleteYourEntity(id string) error {
    return m.Called(id).Error(0)
}

func (m *mockKVStore) ListYourEntitiesByUser(userID string) ([]*kvstore.YourEntity, error) {
    args := m.Called(userID)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).([]*kvstore.YourEntity), args.Error(1)
}
```

### Mock Method Pattern

Follow this exact pattern for pointer-return methods:

```go
func (m *mockKVStore) GetThing(id string) (*kvstore.Thing, error) {
    args := m.Called(id)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*kvstore.Thing), args.Error(1)
}
```

The `nil` check on `args.Get(0)` prevents a nil pointer dereference when the mock returns `nil`.

For slice-return methods:

```go
func (m *mockKVStore) ListThings() ([]*kvstore.Thing, error) {
    args := m.Called()
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).([]*kvstore.Thing), args.Error(1)
}
```

For simple-return methods:

```go
func (m *mockKVStore) SaveThing(thing *kvstore.Thing) error {
    return m.Called(thing).Error(0)
}
```

## Checklist

- [ ] Defined entity struct in `server/store/kvstore/kvstore.go`
- [ ] Added interface methods to `KVStore` interface
- [ ] Added key prefix constants in `server/store/kvstore/store.go`
- [ ] Implemented Get, Save, Delete methods
- [ ] Implemented List method (if needed) with index prefix pattern
- [ ] Added store tests in `server/store/kvstore/store_test.go`
- [ ] Updated `mockKVStore` in `server/command/command_test.go`
- [ ] Updated `mockKVStore` in `server/handlers_test.go`
- [ ] Run `go test ./server/...` to verify all tests pass
