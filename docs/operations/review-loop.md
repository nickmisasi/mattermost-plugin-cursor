# Review Loop Operations (Direct Dispatch Only)

This runbook covers day-2 operations for the AI review loop after the direct-follow-up cutover.

## Architecture Snapshot

- Review-loop fix iterations dispatch feedback to Cursor using `AddFollowup` only.
- The legacy `@cursor` PR-comment relay path is not part of runtime behavior.
- Dispatch failures are fail-fast and visible:
  - history detail on the `ReviewLoop`
  - structured plugin logs with dispatch SHA, digest, and counts

## Quick Health Checks

1. Confirm review loop feature is enabled:
   - `EnableAIReviewLoop=true`
2. Confirm credentials are configured:
   - `CursorAPIKey` (Cursor follow-up dispatch)
   - `GitHubPAT` (reviewer assignment + feedback collection from GitHub)
3. Confirm plugin health endpoint:
   - `GET /plugins/com.mattermost.plugin-cursor/api/v1/admin/health`
4. Inspect review-loop timeline in API/webapp:
   - check `history[].detail` for direct dispatch outcomes or failure details

## Expected Dispatch Outcomes

- `direct_success`: follow-up dispatched, loop advances to `cursor_fixing`
- `idempotent_same_sha_digest`: duplicate bundle skipped without re-dispatch
- direct dispatch failure (`cursor_client_nil`, `add_followup_error`, or `direct_failed`):
  - loop does not advance
  - iteration does not increment
  - history records manual intervention requirement

## Failure Triage

When dispatch fails:

1. Inspect plugin logs for `Review feedback dispatch decision`
   - fields: `dispatch_mode`, `decision_reason`, `dispatch_sha`, `dispatch_digest`
   - counts: `new_count`, `repeated_count`, `dismissed_count`, `dispatchable_count`
   - error: `error_primary`
2. Validate Cursor path:
   - Cursor API key validity
   - Cursor API availability
   - target agent state allows follow-up
3. Validate network/connectivity between Mattermost plugin host and Cursor API.
4. Validate GitHub data collection only if feedback extraction also looks wrong.

## Manual Recovery

- Restore direct dispatch health first (credentials/connectivity/agent state).
- Re-trigger review-loop progression by submitting new review feedback (AI or human).
- If needed, use a manual follow-up action on the affected agent/thread once path health is restored.

## Incident Guardrail

- Do not route incidents through legacy PR-comment relay.
- For sustained incidents with high operational risk, temporarily disable `EnableAIReviewLoop`.
- Re-enable once direct dispatch is healthy.
