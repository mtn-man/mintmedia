# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
# Build
go build ./cmd/mintmedia/

# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/processor/

# Run a single test
go test ./internal/processor/ -run TestPlan_TableDriven

# Run tests with race detector
go test -race ./...

# Vet
go vet ./...
```

The compiled binary is `mintmedia`. Default config path is `~/.config/mintmedia/config.toml`.

## Architecture

### Execution Modes

`main.go` dispatches into one of two paths after config load and processor wiring:

- **One-shot**: `--plan`, `--apply`, `--process`, `--process-drop` — run, print results, exit.
- **Daemon**: `--daemon` — long-running watch/poll loop managed by `internal/daemon`.

### Wiring (main.go)

All dependency wiring happens in `main.go` before mode dispatch:
1. `config.Load()` → `(*config.Config, *config.Resolved, bool, error)` — the bool is `bootstrapped` (true when no config existed and a default was written). If `--config` is not passed, Load auto-generates `~/.config/mintmedia/config.toml` from the embedded `internal/config/defaults.toml` on first run.
2. `logging.New()` → `logging.Logger` (two sinks: console + JSONL history)
3. `transfer.NewRenameOrCopy()` → `transfer.Transferer` (fast rename, falls back to copy+atomic finalize)
4. `processor.New(cfg, xfer, logger)` → `processor.Processor`
5. In daemon mode, `daemon.New(...)` wires the above with `watch.Watcher`, `clipboard.Poller`, and optionally `transmission.Client`

### Plan/Apply Separation

The processor (`internal/processor`) splits work into two phases:
- `Plan()` — deterministic, no filesystem writes, inspects paths and returns `[]Plan`
- `Apply()` — executes the plans (file moves, trash), writes history log entries
- `Process()` — calls both with policy (silently ignores non-media/no-main-media cases)

This enables the `--plan` dry-run mode and also makes `Plan()` straightforwardly testable without mocks.

### Logging Boundary

`logging.Logger` has two distinct output channels:
- **Console** methods (`ConsoleInfo`, `ConsoleWarn`, `ConsoleError`) — operational output for the user, not persisted
- **History** methods (`HistoryInfo`, `HistoryWarn`, `HistoryError`) — persisted to JSONL, controlled by `history_level` and an info allowlist
- **Combined** methods (`Info`, `Warn`, `Error`) — write to both

The boundary is enforced: daemon/processor code must not cross it (e.g., no console output from deep in processor logic). This was an explicit recent refactor.

### Key Interfaces

```go
// internal/processor/types.go
type Processor interface {
    Plan(ctx context.Context, req Request) ([]Plan, error)
    Apply(ctx context.Context, plans []Plan) ([]Result, error)
    Process(ctx context.Context, req Request) ([]Result, error)
}

// internal/transfer/transfer.go
type Transferer interface {
    Move(ctx context.Context, src, dst string) error
}

// internal/logging/types.go
type Logger interface { ... }
```

### Error Handling Conventions

- Sentinel errors (e.g., `ErrNotMedia`, `ErrNoMainMediaFound`) are package-level vars for `errors.Is()` checks.
- Contextual wrapped types (e.g., `ParseShowError`, `PartialPlanError`, `CleanupError`) carry structured data for `errors.As()`.
- `CleanupError` specifically signals partial success — some files moved, cleanup step failed.

### macOS-Specific Code

- `internal/clipboard/pasteboard_darwin.go` — cgo-based pasteboard polling; `pasteboard_unsupported.go` stubs for Linux.
- `internal/notify/` — wraps `afplay` (sounds) and `caffeinate` (sleep prevention).
- Transmission automation uses `transmission-remote` CLI subprocess calls.

### Testing Patterns

- Tests within a package use the internal package name (e.g., `package processor`), giving access to unexported types.
- `processor` tests use `TestMain` to set up a temp `$HOME` with a `.Trash` directory — required for trash-based cleanup logic.
- Table-driven tests throughout; parallel tests are the norm (`t.Parallel()`).
- No database or network mocks needed — external calls (Transmission, `afplay`) are abstracted behind interfaces or thin wrappers tested separately.
