---
description: "Launch a read-only planner Cursor Cloud Agent, retrieve the plan from the conversation API, and understand the HITL plan loop patterns. Includes spike-validated API behavior, prompt templates, and gotchas."
---

# Launch a Planner Agent (HITL Plan Loop)

This skill covers how to launch a read-only Cursor Cloud Agent for planning, retrieve its plan output, and iterate. Based on a live spike test against `mattermost/mattermost` (Feb 2026).

## Overview

The planner agent analyzes a codebase and outputs an implementation plan WITHOUT modifying code. This is the core of the HITL Plan Loop (Milestone 3, Phase 3).

```
Approved context
    |
    v
Launch planner agent (read-only, autoCreatePr=false, autoBranch=false)
    |
    v
Poll until FINISHED
    |
    v
GET /agents/{id}/conversation → extract plan from LAST assistant_message
    |
    v
Post plan for user review (Accept / Reject / Iterate)
    |
    v (if iterate)
Create NEW planner agent with context + prev plan + feedback
```

## API Behavior (Spike-Validated)

### Agent Creation

```go
launchReq := cursor.LaunchAgentRequest{
    Prompt: cursor.Prompt{Text: plannerPrompt},
    Source: cursor.Source{Repository: repoURL, Ref: branch},
    Target: &cursor.Target{
        AutoCreatePr: false,
        AutoBranch:   false,  // CRITICAL — see gotcha below
    },
    Model: modelName,
}
```

**GOTCHA: `autoBranch` defaults to `true`**. If you omit it or set it via `omitempty`, the API will auto-generate a branch name like `cursor/image-compact-mode-alignment-d54e` on every agent creation. This creates orphan branches on every planning iteration. The `Target.AutoBranch` field in `server/cursor/types.go` uses `json:"autoBranch"` (no omitempty) so it serializes `false` correctly.

### Conversation Response Format

`GET /v0/agents/{id}/conversation` returns:

```json
{
  "id": "agent-uuid",
  "messages": [
    {"id": "msg-1", "type": "user_message", "text": "The original prompt..."},
    {"id": "msg-2", "type": "assistant_message", "text": "I'll start by reading CLAUDE.md..."},
    {"id": "msg-3", "type": "assistant_message", "text": "I'm now tracing the rendering path..."},
    {"id": "msg-4", "type": "assistant_message", "text": "I've narrowed the issue to..."},
    {"id": "msg-5", "type": "assistant_message", "text": "#### Summary\nRoot cause is in the..."}
  ]
}
```

**Key pattern**: The conversation typically has:
- 1 `user_message` (the prompt)
- N-1 `assistant_message` entries that are progress updates ("I'm now investigating...", "I found the relevant files...")
- 1 final `assistant_message` containing the structured plan

**Always extract the LAST `assistant_message`**, not the first.

### Plan Extraction Helper

```go
func extractPlanFromConversation(conv *cursor.Conversation) string {
    // Iterate in reverse to find the last assistant message
    for i := len(conv.Messages) - 1; i >= 0; i-- {
        if conv.Messages[i].Type == "assistant_message" {
            return conv.Messages[i].Text
        }
    }
    return ""
}
```

### Intermediate Progress Messages

The earlier `assistant_message` entries show the agent's investigation process. These can optionally be posted as thread status updates during the planning phase:

```go
func extractProgressMessages(conv *cursor.Conversation) []string {
    var progress []string
    for _, msg := range conv.Messages {
        if msg.Type == "assistant_message" {
            progress = append(progress, msg.Text)
        }
    }
    // All but last are progress; last is the plan
    if len(progress) > 1 {
        return progress[:len(progress)-1]
    }
    return nil
}
```

## The Planner System Prompt

Strong prompting is essential. The agent MUST be told explicitly and repeatedly not to modify code. The spike validated that this approach works — the agent analyzed files, traced code paths, and checked git history without making any changes.

```go
const defaultPlannerPrompt = `## CRITICAL INSTRUCTIONS — READ-ONLY PLANNING MODE

YOU ARE IN PLANNING MODE. YOU MUST NOT MODIFY ANY CODE. YOU MUST NOT CREATE, EDIT, OR DELETE ANY FILES. YOU MUST NOT CREATE BRANCHES OR PULL REQUESTS. YOUR ONLY JOB IS TO ANALYZE THE CODEBASE AND OUTPUT A PLAN.

IF YOU MODIFY ANY FILE, YOU HAVE FAILED YOUR TASK.

You MUST:
1. Run ` + "`./enable-claude-docs.sh`" + ` if it exists in the repository root
2. Read any CLAUDE.md files in the repository for project-specific instructions
3. Thoroughly investigate the codebase areas relevant to the task
4. Identify all files that would need to change
5. Describe the specific changes needed in each file
6. Consider edge cases, tests that need updating, and potential regressions
7. Output a clear, structured implementation plan

Format your plan as:
### Summary
[1-2 sentence overview of the approach]

### Files to Change
For each file:
- **` + "`path/to/file`" + `**: [What changes and why]

### Implementation Steps
[Numbered steps in dependency order]

### Testing Strategy
[What tests to add/modify]

### Risks & Considerations
[Edge cases, potential regressions, things to watch for]

REMEMBER: DO NOT MODIFY ANY FILES. ONLY ANALYZE AND PLAN.`
```

This prompt is overridable via `PlannerSystemPrompt` in System Console config.

## Plan Iteration (Creating New Agents)

Follow-ups only work on RUNNING agents. Since planner agents FINISH after outputting the plan, iteration requires a NEW agent:

```go
func (p *Plugin) iteratePlan(workflow *kvstore.HITLWorkflow, userFeedback string) {
    // Build accumulated prompt: context + previous plan + feedback
    iterationPrompt := fmt.Sprintf(`%s

<previous-plan>
%s
</previous-plan>

<user-feedback>
The user reviewed the plan above and requests these changes:
%s
</user-feedback>

Please revise the plan based on this feedback. Output a new complete plan.
REMEMBER: DO NOT MODIFY ANY FILES. ONLY ANALYZE AND PLAN.`,
        workflow.ApprovedContext,
        workflow.RetrievedPlan,
        userFeedback,
    )

    workflow.PlanIterationCount++
    // Launch new planner agent with iterationPrompt
    p.launchPlannerAgent(workflow)
}
```

## Spike Test Results (Reference)

Tested against `mattermost/mattermost` (master) for MM-66620:

| Metric | Value |
|---|---|
| Agent runtime | ~11 minutes |
| Conversation messages | 9 assistant + 1 user |
| Plan text size | ~3.5KB |
| Code modified | None (strong prompting worked) |
| Branch created | Yes — `autoBranch` defaults to `true`. Fixed by setting `false`. |
| Plan quality | High — identified correct component, specific files, root cause, implementation steps |
| API response format | Clean JSON, no artifacts, plan in last `assistant_message` |

## Poller Integration

The poller (`server/poller.go`) must be workflow-aware. When a planner agent reaches terminal status:

```go
// In the poller's terminal status handler:
workflowID, _ := p.kvstore.GetWorkflowByAgent(record.CursorAgentID)
if workflowID != "" {
    workflow, _ := p.kvstore.GetWorkflow(workflowID)
    if workflow != nil && workflow.Phase == kvstore.PhasePlanning {
        p.handlePlannerFinished(workflow, remoteAgent)
        return // Don't run normal terminal handling (no PR links, etc.)
    }
}
// ... existing terminal handling for implementation agents
```

## Checklist for Planner Changes

- [ ] `Target.AutoCreatePr` is `false`
- [ ] `Target.AutoBranch` is `false` (not omitted — must serialize as `false`)
- [ ] System prompt includes strong "DO NOT MODIFY" language
- [ ] Plan extracted from LAST `assistant_message` (not first)
- [ ] New planner agent created per iteration (not follow-up)
- [ ] `hitlagent:` reverse index updated when planner agent changes
- [ ] Previous planner agent cleaned up (stopped/deleted) on iteration
- [ ] Poller routes planner terminal status to workflow handler, not normal handler
- [ ] Plan text truncated if >4000 chars for Mattermost post display
