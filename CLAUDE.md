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

- **One-shot**: `--plan [path]`, `--process <path>`, `--process-drop` — run, print results, exit.
- **Daemon**: `--daemon` — long-running watch/poll loop managed by `internal/daemon`.

### Config: Two-Struct Design

`config.Load()` returns both `*config.Config` (raw decoded TOML) and `*config.Resolved` (normalized, validated, absolute paths). All downstream packages should use `Resolved`; `Config` is only needed to check feature flags or pass to sub-systems that do their own normalization. The `bootstrapped` bool is true only when no config existed and defaults were written on first run.

### Wiring (main.go)

All dependency wiring happens in `main.go` before mode dispatch:
1. `config.Load()` → `(*config.Config, *config.Resolved, bool, error)`
2. `logging.New()` → `logging.Logger` (two sinks: console + JSONL history)
3. `transfer.NewRenameOrCopy()` → `transfer.Transferer` (fast rename, falls back to copy+atomic finalize)
4. `processor.New(cfg, xfer, logger)` → `processor.Processor`
5. In daemon mode, `daemon_mode.go` constructs `&daemon.Daemon{...}` directly (struct literal, no constructor) and calls `d.Run(runCtx)`, wiring in `watch.Watcher`, `clipboard.Poller`, and optionally `transmission.Client`

### Plan/Apply Separation

The processor (`internal/processor`) splits work into two phases:
- `Plan()` — deterministic, no filesystem writes, inspects paths and returns `[]Plan`
- `Apply()` — executes the plans (file moves, trash), writes history log entries
- `Process()` — calls both with policy (silently ignores non-media/no-main-media cases); streams results via `req.OnResult`; returns `error` only
- `ProcessEach()` — convenience wrapper that wires `onResult` into `req.OnResult` and calls `Process()`. Daemon and process-drop use this.

This enables the `--plan` dry-run mode and also makes `Plan()` straightforwardly testable without mocks.

### Media-Aware Ordering

`processor.SortCandidates()` sorts a batch of paths before processing: movies first (alphabetical by title), then shows (by name → season → episode), then unparseable fallbacks. The daemon wires this into the watcher's settle batch via `Watcher.SetSortFunc`; process-drop calls it directly. Sorting uses `parseSortKey()` — filename-only parsing with no `Plan()` calls and no filesystem I/O.

### Logging Boundary

`logging.Logger` has two distinct output channels:
- **Console** methods (`ConsoleInfo`, `ConsoleWarn`, `ConsoleError`) — operational output for the user, not persisted
- **History** methods (`HistoryInfo`, `HistoryWarn`, `HistoryError`) — persisted to JSONL, controlled by `history_level` and an info allowlist
- **Combined** methods (`Info`, `Warn`, `Error`) — write to both

The boundary is enforced: daemon/processor code must not cross it (e.g., no console output from deep in processor logic). This was an explicit recent refactor.

**History allowlist**: Even when `logging.history_level = WARN`, specific Info events (startup, shutdown, moves applied, etc.) are still persisted — see `logging.DefaultHistoryInfoAllowlist()`. This means `history_level` is not a pure level gate; it's combined with an explicit per-event allowlist for Info-level events that must always be recorded.

### Key Interfaces

```go
// internal/processor/types.go
type Processor interface {
    Plan(ctx context.Context, req Request) ([]Plan, error)
    Apply(ctx context.Context, plans []Plan) ([]Result, error)
    Process(ctx context.Context, req Request) error
    SortCandidates(ctx context.Context, paths []string) ([]string, []SortError, error)
}

// internal/processor/types.go
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

### Daemon Shutdown Model

`Daemon.Run()`'s event loop stops accepting new work as soon as its caller-supplied `ctx` (`runCtx`) is cancelled (SIGINT/SIGTERM), then waits for `runWorker` to fully stop before returning. `runWorker` dequeues one `workItem` at a time and runs it via `internal/jobrunner.Run`, which owns the actual graceful-then-forced drain: it lets the in-flight item keep running on its own detached, per-item context for up to `policy.Grace`, then cancels that item's context and waits up to `policy.Force` before giving up. `runWorker` checks `runCtx.Done()` with priority before dequeuing another item, so this grace/force window is bounded to the single item that was in flight when shutdown began, not repeated per queued item. `internal/shutdown` contains the generic drain/policy primitives (`Policy`, `Drain`, `Hooks`) that `internal/jobrunner.Run` builds on; both daemon and process-drop modes call `jobrunner.Run` directly rather than each hand-rolling their own drain loop.

### Platform-Specific Code

- `internal/clipboard/pasteboard_darwin.go` — cgo-based pasteboard polling; `pasteboard_linux.go` — Wayland-based polling via `wl-paste`; `pasteboard_unsupported.go` stubs for all other platforms (build tag: `(!darwin || !cgo) && !linux`).
- `internal/notify/` — wraps `afplay` (sounds) and `caffeinate` (sleep prevention).
- Transmission automation uses a native HTTP JSON-RPC client (`internal/transmission/client.go`); no subprocess calls.

### Testing Patterns

- Tests within a package use the internal package name (e.g., `package processor`), giving access to unexported types.
- `processor` tests use `TestMain` to set up a temp `$HOME` with a `.Trash` directory — required for trash-based cleanup logic.
- Table-driven tests throughout; parallel tests are the norm (`t.Parallel()`).
- No database or network mocks needed — external calls (Transmission, `afplay`) are abstracted behind interfaces or thin wrappers tested separately.
