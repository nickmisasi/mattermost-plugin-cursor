# Message Parser

## Purpose

Parses `@cursor` mention messages into structured `ParsedMention` results. Extracts repository, branch, model, auto-PR flag, and the task prompt from natural language, inline options, and bracketed options.

## ParsedMention Fields

```go
type ParsedMention struct {
    Prompt     string  // The actual task instruction (everything left after options removed)
    Repository string  // GitHub repo in owner/repo format (or short name)
    Branch     string  // Base branch name
    Model      string  // AI model name
    AutoPR     *bool   // nil = use default, non-nil = explicit override
    ForceNew   bool    // true when "@cursor agent ..." prefix used
}
```

## Supported Input Formats

### Natural Language
```
@cursor in backend-api, fix the auth issue      -> Repository: "backend-api"
@cursor in org/repo, fix the auth issue          -> Repository: "org/repo"
@cursor with opus, fix the login bug             -> Model: "opus"
@cursor in org/repo, with opus, fix it           -> Repository + Model
```

### Inline Key=Value
```
@cursor branch=dev autopr=false Fix the bug      -> Branch: "dev", AutoPR: false
@cursor repo=org/repo model=o3 branch=dev Fix it -> All three
```

### Bracketed Options (highest priority)
```
@cursor [branch=dev, model=o3, repo=org/repo] Fix the bug
```

### Force New Agent
```
@cursor agent start a new agent for this         -> ForceNew: true
```

### Combined
```
@cursor [repo=org/repo] branch=dev with opus, fix it   -> All options set
```

## Precedence Rules

When the same option appears in multiple formats, the order of precedence is:
1. **Bracketed** `[key=val]` (highest)
2. **Inline** `key=val`
3. **Natural language** `in <repo>`, `with <model>` (lowest)

The natural language patterns are always stripped from the remainder regardless of whether they set a value, to keep the prompt clean.

## Parsing Steps

1. Find and strip the bot mention (case-insensitive)
2. Check for `agent ` prefix (sets `ForceNew`)
3. Extract bracketed options block `[...]` at the start
4. Extract inline `key=value` options (processed in reverse index order to preserve positions)
5. Extract natural language `in <repo>` pattern
6. Extract natural language `with <model>` pattern
7. Clean up: trim whitespace, collapse multiple spaces
8. Remaining text becomes `Prompt`

## Return Value

Returns `nil` when:
- The message does not contain the bot mention
- The message is empty after stripping the mention (user just typed `@cursor`)

## Testing

Table-driven tests in `parser_test.go` with ~20 test cases covering all formats and edge cases.

To add a new parsing format:
1. Add the regex pattern at file top
2. Add extraction logic in `Parse()` at the appropriate precedence step
3. Add test cases to the table in `TestParse`

## Common Pitfalls

- **Case-insensitive mention matching**: The parser does case-insensitive matching for the bot mention, but preserves the original case of the prompt text.
- **Comma after natural language**: Both `in repo,` and `in repo` work (trailing comma is part of the regex match).
- **Short repo names**: `in backend-api` captures single names (no `/`), not just `owner/repo` format. The caller resolves these against defaults.
- **Empty prompt**: If only options are provided (e.g., `@cursor repo=org/repo branch=main`), the `Prompt` field will be empty string, not nil. The ParsedMention itself is still returned.
