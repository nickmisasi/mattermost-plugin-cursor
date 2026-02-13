# Modify the Agent Prompt Pipeline

This skill explains how the prompt pipeline works from user message to Cursor API call, and how to modify each stage.

## Overview: The Prompt Pipeline

When a user mentions `@cursor fix the login bug` in a thread, the prompt goes through this pipeline before reaching the Cursor API:

```
User Message
    |
    v
Parser (server/parser/parser.go)
    |-- Extracts: prompt, repo, branch, model, forceNew, autoPR
    |
    v
Thread Enrichment (server/handlers.go: enrichFromThread)
    |-- Gathers thread context (all messages, images)
    |-- Optionally enriches via bridge client LLM
    |-- Falls back to raw "--- Thread Context ---" wrapping
    |
    v
System Prompt Wrapping (server/handlers.go: wrapPromptWithSystemInstructions)
    |-- Wraps in <system-instructions>...</system-instructions>
    |-- Wraps task in <task>...</task>
    |
    v
Cursor API LaunchAgent Request
    |-- prompt.text = wrapped prompt
    |-- prompt.images = extracted thread images
```

## Stage 1: The Parser

**File:** `server/parser/parser.go`

The parser extracts structured options from the user's message and returns a `ParsedMention`:

```go
type ParsedMention struct {
    Prompt     string   // The cleaned prompt text
    Repository string   // "owner/repo"
    Branch     string   // Base branch name
    Model      string   // AI model name
    AutoPR     *bool    // nil = use default
    ForceNew   bool     // "@cursor agent ..." forces new agent
}
```

Supported syntax:
- `@cursor fix the bug` -- simple prompt
- `@cursor in org/repo, fix the bug` -- "in <repo>" natural language
- `@cursor with claude-sonnet, fix the bug` -- "with <model>" natural language
- `@cursor [repo=org/repo, branch=dev, model=opus] fix the bug` -- bracketed options
- `@cursor repo=org/repo branch=dev fix the bug` -- inline key=value options
- `@cursor agent fix the bug` -- force new agent in thread

### Modifying the Parser

To add a new option (e.g., `timeout=30`):

1. Add the field to `ParsedMention` in `server/parser/parser.go`
2. Add the case to `applyOption()`:
   ```go
   func applyOption(key, value string, result *ParsedMention) {
       switch key {
       // ... existing cases ...
       case "timeout":
           t, _ := strconv.Atoi(value)
           result.Timeout = t
       }
   }
   ```
3. Add the key to the `inlineOptRe` regex (it already matches `\b(repo|branch|model|autopr)=(\S+)`):
   ```go
   inlineOptRe = regexp.MustCompile(`(?i)\b(repo|branch|model|autopr|timeout)=(\S+)`)
   ```
4. Add tests in `server/parser/parser_test.go`

## Stage 2: Thread Context Enrichment

**File:** `server/handlers.go`, function `enrichFromThread`

When the user posts in a thread (has a `RootId`), the plugin gathers the entire thread as context:

### How It Works

1. **Fetch the thread** via `p.API.GetPostThread(post.RootId)`
2. **Format the thread** via `formatThread(postList)`:
   - Iterates posts in chronological order
   - Formats as `[DisplayName]: message text`
   - Extracts images from file attachments (up to 5 images, 10MB total)
   - Decodes image dimensions for the Cursor API
3. **Enrich via bridge client** via `enrichPromptViaBridge(threadText)`:
   - Uses the mattermost-plugin-ai bridge client for LLM-based enrichment
   - Sends the enrichment prompt + thread text to the LLM
   - Returns a refined, actionable task description
4. **Fallback** if bridge client is unavailable or fails:
   - Wraps raw thread text in `--- Thread Context ---` / `--- End Thread Context ---`

### Modifying the Enrichment LLM Prompt

The enrichment prompt is a constant in `server/handlers.go`:

```go
const enrichmentPrompt = `You are a context formatter. Given a Mattermost thread conversation, extract and clearly describe the task or issue being discussed...`
```

To modify it:
1. Edit the `enrichmentPrompt` constant directly
2. The prompt instructs the LLM to describe WHAT the issue is, NOT how to fix it
3. Keep the instruction to NOT prescribe technical solutions -- the Cursor agent has full codebase access

### Modifying Thread Formatting

The `formatThread` function formats threads as `[DisplayName]: message`. To change the format:

```go
func (p *Plugin) formatThread(postList *model.PostList) (string, []cursor.Image) {
    // ... existing sorting and iteration ...
    for _, postID := range order {
        threadPost := posts[postID]
        displayName := p.getDisplayName(threadPost.UserId)
        sb.WriteString(fmt.Sprintf("[%s]: %s\n", displayName, threadPost.Message))
        // ... image extraction ...
    }
    // ...
}
```

### Modifying Image Extraction

Image limits are constants:

```go
const (
    maxThreadImages    = 5                     // Max images per thread
    maxThreadImageSize = 10 * 1024 * 1024      // 10MB total
)
```

Images are converted to base64 with dimension metadata for the Cursor API:

```go
images = append(images, cursor.Image{
    Data: base64.StdEncoding.EncodeToString(fileData),
    Dimension: cursor.ImageDimension{
        Width:  imgConfig.Width,
        Height: imgConfig.Height,
    },
})
```

## Stage 3: System Prompt Wrapping

**File:** `server/handlers.go`, function `wrapPromptWithSystemInstructions`

The system prompt is prepended to the task prompt using XML tags:

```go
func (p *Plugin) wrapPromptWithSystemInstructions(taskPrompt string) string {
    systemPrompt := p.getSystemPrompt()
    return fmt.Sprintf("<system-instructions>\n%s\n</system-instructions>\n\n<task>\n%s\n</task>", systemPrompt, taskPrompt)
}
```

### The System Prompt Cascade

1. **Admin-configured:** If `CursorAgentSystemPrompt` is set in System Console, it is used
2. **Default:** Otherwise, the built-in `defaultSystemPrompt` constant is used

```go
func (p *Plugin) getSystemPrompt() string {
    config := p.getConfiguration()
    if config.CursorAgentSystemPrompt != "" {
        return config.CursorAgentSystemPrompt
    }
    return defaultSystemPrompt
}
```

### The Default System Prompt

```go
const defaultSystemPrompt = `## Development Guidelines

Before making any changes:
1. Run ` + "`./enable-claude-docs.sh`" + ` if it exists in the repository root
2. Read any CLAUDE.md files in the repository for project-specific instructions
3. Read webapp/STYLE_GUIDE.md (if present) for frontend coding standards
4. Investigate the issue thoroughly before proposing or implementing a fix

When working on the task:
- ONLY make changes that directly solve the task at hand
- Do NOT make changes to irrelevant code, even if you notice other issues
- Do NOT refactor, clean up, or "improve" code outside the scope of the task
- Follow existing code patterns and conventions in the repository
- If the task involves UI/frontend work, ensure changes are accessible and theme-compatible`
```

### Modifying the Default System Prompt

Edit the `defaultSystemPrompt` constant in `server/handlers.go`. Note the string concatenation for backticks -- Go raw string literals cannot contain backtick characters, so inline code formatting uses:

```go
"Run " + "`./some-command`" + " to do something"
```

### Modifying the XML Wrapping

The wrapping format is in `wrapPromptWithSystemInstructions`. The current format is:

```xml
<system-instructions>
{system prompt content}
</system-instructions>

<task>
{enriched task prompt OR raw prompt}
</task>
```

## Stage 4: The Final API Request

In `launchNewAgent` in `server/handlers.go`, the final request is assembled:

```go
launchReq := cursor.LaunchAgentRequest{
    Prompt: cursor.Prompt{Text: promptText, Images: promptImages},
    Source: cursor.Source{Repository: repoURL, Ref: branch},
    Target: &cursor.Target{
        BranchName:   sanitizeBranchName(parsed.Prompt),
        AutoCreatePr: autoCreatePR,
    },
    Model: modelName,
}
```

### Modifying What Goes to the API

- `promptText` is the fully-wrapped prompt (system instructions + enriched task)
- `promptImages` are base64-encoded images from the thread
- `repoURL` is resolved from parsed mention > channel settings > user settings > global config
- `branch` follows the same cascade
- `modelName` follows the same cascade

## Slash Command Prompt (Different Path)

When launched via `/cursor fix the bug`, the prompt takes a simpler path in `server/command/command.go`:

1. Parser extracts options from the command text
2. No thread enrichment (slash commands are not in threads)
3. No system prompt wrapping (the command handler does NOT wrap)
4. The raw `parsed.Prompt` is sent directly

To add system prompt wrapping to slash command launches, you would need to call `wrapPromptWithSystemInstructions` in `executeLaunch` in `server/command/command.go`. Currently, only the `MessageHasBeenPosted` handler wraps prompts.

## Testing Prompt Changes

1. Enable debug logging
2. Trigger an agent launch via `@cursor`
3. Check logs for `"Agent launch prompt prepared"` which shows the prompt length and preview
4. Check `"Cursor API request body"` for the exact JSON sent to the API

## Checklist for Prompt Modifications

- [ ] Identified which stage to modify (parser, enrichment, system prompt, wrapping)
- [ ] Made the change in the correct file
- [ ] If modifying the parser, updated the regex and added tests
- [ ] If modifying the enrichment prompt, kept the "describe WHAT, not HOW" philosophy
- [ ] If modifying the system prompt, used string concatenation for backticks
- [ ] Tested with debug logging enabled
- [ ] Verified the final prompt in the API request body log
- [ ] Run `go test ./server/...` to verify
