---
name: go-testing
description: Go test conventions — testify assert/require with t.Run subtests, one top-level Test function per concern, fakes over mocks, httptest for outside services, and build-tagged integration tests. Use when writing, editing or reviewing Go tests (*_test.go).
---

# Go Testing Conventions

## Framework

- **testify** (`assert` + `require`) with **`t.Run` subtests** by default.
- A module that keeps its dependencies minimal — an operator CLI, for instance (`go-cli`
  skill) — uses the standard `testing` package only. The project's instructions say which
  module does which.

## Rules

1. **One top-level `Test<Thing>` function** per type or concern in the file
2. **`t.Run` for each case** — the name reads like a sentence
3. **`require`** for setup steps (stops the test on failure)
4. **`assert`** for assertions (reports all failures)
5. **Fakes over mocks**: a port gets a small hand-written fake in the test file; an outside HTTP
   service gets an `httptest.Server`.
6. **A test never calls a real outside service** — not a third-party API, not a geocoder, not a
   cloud provider.

## Test File Naming

Each implementation file gets its own test file: `client.go` → `client_test.go`.

## Tagged Trees

Tests that need a local service (a database, a queue) carry `//go:build integration` and run
through the project's integration-test task. `go test ./...` skips them, so vet them explicitly:
`go vet -tags integration ./...`.
