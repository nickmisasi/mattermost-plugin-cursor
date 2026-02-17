# Contributing Guide

Thanks for contributing to the Cursor Background Agents plugin for Mattermost.

This guide focuses on two things:
- setting up a local development environment
- running tests and checks before opening a PR

## Prerequisites

- Go `1.24.11` (matches `go.mod`)
- Node.js `20.11.x` (see `.nvmrc`)
- npm
- GNU Make
- Git

Optional (only needed for local plugin deployment/testing in Mattermost):
- A local Mattermost server (`>= 9.6.0`)

## 1) Clone and enter the repository

```bash
git clone https://github.com/mattermost/mattermost-plugin-cursor.git
cd mattermost-plugin-cursor
```

## 2) Set up language runtimes

### Go

Verify your Go version:

```bash
go version
```

Expected: Go `1.24.11` (or a compatible `1.24.x` release).

### Node.js

If you use `nvm`, run:

```bash
nvm install
nvm use
```

Then verify:

```bash
node -v
npm -v
```

## 3) Install project dependencies

Install webapp dependencies:

```bash
cd webapp
npm install
cd ..
```

You can also let Make handle dependency setup automatically when running build/test targets.

## 4) Run tests

### Server tests (Go)

```bash
go test ./server/...
```

### Webapp tests (Jest)

```bash
cd webapp
npm test -- --watchAll=false
cd ..
```

### Run everything via Make

```bash
make test
```

## 5) Run style/type checks

Before submitting changes, run:

```bash
make check-style
```

This runs:
- webapp linting
- TypeScript type checks
- Go vet + golangci-lint

## 6) Optional: build and deploy to a local Mattermost server

Build a distributable plugin bundle:

```bash
make dist
```

Deploy to your local Mattermost instance:

```bash
make deploy
```

For deploy to work, configure Mattermost pluginctl credentials (for example via environment variables such as `MM_SERVICESETTINGS_SITEURL`, `MM_ADMIN_USERNAME`, and `MM_ADMIN_PASSWORD`).

## Submitting changes

Before opening a PR:
- keep changes scoped to the issue/task
- run tests relevant to your changes
- run `make check-style`
- ensure CI is green
