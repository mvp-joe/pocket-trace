# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

pocket-trace is a self-contained distributed tracing system for Go services: a zero-dependency Go library for instrumenting apps, a daemon that stores traces in SQLite, and a React UI for visualization. Spans flow from instrumented apps → slog (stdout) + HTTP POST → daemon (:7070) → SQLite → React UI.

## Build Commands

```bash
make build          # Production: builds React UI then Go binary with embedded assets
make dev            # Dev: Go binary only (empty UI embed, use Vite dev server)
make clean          # Remove build artifacts
cd ui && npm run dev   # Vite dev server for UI development
cd ui && npm run build # Build React UI separately
cd ui && npm run lint  # Lint React UI
```

## Testing

```bash
go test ./...                              # All Go tests
go test -v ./internal/store -run TestName  # Single test in a package
go test -v -run TestExporter               # Single test from root
```

Tests use table-driven patterns, `httptest.Server` for HTTP mocking, and context-based timeouts. No external test dependencies.

## Architecture

### Go Library (`trace.go`, `exporter.go`)
Zero external dependencies. `trace.Start(ctx, name, attrs...)` creates spans with context propagation. `HTTPExporter` batches spans in a buffered channel (4096) and flushes async to the daemon. Non-blocking; drops spans if buffer full.

### Daemon (`cmd/pocket-trace/`, `internal/`)
- **CLI** (`cmd/pocket-trace/`): Cobra commands — `run`, `install`, `uninstall`, `status`, `purge`
- **Server** (`internal/server/`): Fiber v3 HTTP server. `SpanBuffer` in `ingest.go` accumulates spans and flushes to SQLite periodically via background goroutine.
- **Store** (`internal/store/`): SQLite with WAL mode. Single `spans` table with indexes on trace_id, service_name+start_time, start_time. Uses CTEs for trace aggregation. Upserts via `INSERT OR REPLACE`.
- **Config** (`internal/config/`): YAML config merged with code defaults. Custom `UnmarshalYAML` for `time.Duration`. System path: `/etc/pocket-trace/`, user path: `~/.local/share/pocket-trace/`.
- **Daemon** (`internal/daemon/`): Systemd service management (install/uninstall unit files).

### UI (`ui/`)
React 19 + TypeScript + Vite 6 + TailwindCSS 4. Uses React Query for data fetching, React Router 7 for routing, shadcn components.

### UI Embedding
Two build tags: `embed.go` (production, embeds `ui/dist/`) and `embed_dev.go` (dev, empty FS). Default build embeds assets; `make dev` uses empty embed for live Vite development.

## API Endpoints (`internal/server/routes.go`)

- `POST /api/ingest` — accept spans from library exporter
- `GET /api/services` — list services with span counts
- `GET /api/traces` — search traces (filters: service, span_name, duration, time range)
- `GET /api/traces/:traceID` — full trace as span tree
- `GET /api/traces/:traceID/spans/:spanID` — single span
- `GET /api/dependencies` — service dependency graph
- `GET /api/status` — daemon health + DB stats
- `POST /api/purge?olderThan=24h` — delete old spans
- `* /*` — SPA fallback for React routing

## Key Patterns

- Version injected via ldflags: `-X main.version=$(VERSION)`
- Go module: `github.com/jward/pocket-trace` (Go 1.25)
- Spans always dual-write to slog and exporter (if configured)
- Parent-child span relationships tracked via parent_id for tree reconstruction
