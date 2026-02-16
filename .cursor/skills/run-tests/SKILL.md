# Run Tests

## Description

Run the project's test suite to validate changes. Use this skill after making code changes to ensure nothing is broken.

## When to Use

- After modifying Go server code in `server/`
- After modifying TypeScript webapp code in `webapp/src/`
- When asked to verify, validate, or test changes
- Before finalizing a pull request or committing

## Instructions

### Go Server Tests

Run the Go test suite with race detection:

```bash
cd /home/user/mattermost-plugin-cursor && go test -race -count=1 ./server/...
```

If a specific test fails, re-run it in verbose mode to get detailed output:

```bash
cd /home/user/mattermost-plugin-cursor && go test -v -race -run <TestName> ./server/...
```

### Webapp Tests

Run the TypeScript/React tests:

```bash
cd /home/user/mattermost-plugin-cursor/webapp && npm test -- --watchAll=false
```

### Interpreting Results

- **Go tests**: Look for `PASS` or `FAIL` in the output. A line like `ok github.com/mattermost/mattermost-plugin-cursor/server` means that package passed.
- **Webapp tests**: Look for `Tests: X passed` in the Jest output summary.
- If tests fail, read the failure message carefully. Common issues:
  - Mock interface mismatches: update mock implementations to match interface changes
  - Missing nil checks on `CursorClient`
  - Thread-first messaging: ensure all bot posts use `RootId`

### Reporting

After running tests, summarize:
1. Total tests run and pass/fail counts
2. Any failing test names and a brief description of the failure
3. Suggested fixes for failures if obvious
