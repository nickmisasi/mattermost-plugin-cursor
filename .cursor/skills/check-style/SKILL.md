# Check Style

## Description

Run linting and style checks to ensure code quality standards are met. Use this skill to catch formatting issues, lint violations, and type errors before committing.

## When to Use

- Before committing any code changes
- When asked to lint, format, or check code style
- After making changes to Go or TypeScript files
- When CI checks fail on style issues

## Instructions

### Full Style Check

Run the complete style check suite:

```bash
cd /home/user/mattermost-plugin-cursor && make check-style
```

This runs both Go and webapp linting.

### Go Linting Only

If you only changed Go files:

```bash
cd /home/user/mattermost-plugin-cursor && make install-go-tools && golangci-lint run ./server/...
```

### Webapp Linting Only

If you only changed TypeScript/React files:

```bash
cd /home/user/mattermost-plugin-cursor/webapp && npm run lint && npm run check-types
```

### Common Style Issues and Fixes

**Go:**
- Unused variables/imports: remove them
- Missing error checks: handle all returned errors
- Formatting: run `gofmt -w <file>` to auto-fix

**TypeScript:**
- Type errors: ensure all types align with interfaces in `webapp/src/types.ts`
- Missing return types: add explicit return types to exported functions
- Unused imports: remove them

### Reporting

After running checks, summarize:
1. Whether all checks passed or failed
2. List of any violations with file paths and line numbers
3. Suggested fixes for each violation
