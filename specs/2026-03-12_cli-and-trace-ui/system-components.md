# System Components

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    pocket-trace daemon                          │
│                                                                 │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│  │  Fiber HTTP   │    │  SpanBuffer  │    │   SQLite Store   │  │
│  │  Server       │───>│  (RAM queue) │───>│                  │  │
│  │              │    │  batch write  │    │  spans table     │  │
│  │  /api/ingest │    └──────────────┘    │                  │  │
│  │  /api/query  │<───────────────────────│  query methods   │  │
│  │  /* (UI)     │                        │                  │  │
│  └──────────────┘                        └──────────────────┘  │
│                                                                 │
│  ┌──────────────┐    ┌──────────────────┐                      │
│  │  Config      │    │  Retention       │                      │
│  │  Loader      │    │  Purger (ticker) │                      │
│  └──────────────┘    └──────────────────┘                      │
└─────────────────────────────────────────────────────────────────┘

        ▲  JSON HTTP POST
        │
┌───────┴──────────┐
│  App with         │
│  HTTPExporter     │
│  (library side)   │
└──────────────────┘
```

## Server

`internal/server/server.go`

**Responsibility:** Initializes the Fiber app, registers middleware and routes, manages server lifecycle.

- Creates Fiber app with JSON error handling and recovery middleware
- Registers API route group (`/api`) and static file serving (`/*`)
- Receives an `fs.FS` for the embedded UI assets, plus `retention` and `purgeInterval` durations via its constructor (passed from `cmd/pocket-trace/`)
- Owns the `SpanBuffer` and `Store` references, passes them to handlers
- Starts the retention purger goroutine if both retention and purgeInterval are non-zero
- `Start(listenAddr string) error` -- starts listening (blocking)
- `Shutdown(ctx context.Context) error` -- graceful shutdown (stops purger, flushes buffer, closes store)

## Route Registration

`internal/server/routes.go`

**Responsibility:** Maps endpoints to handler functions. Keeps route definitions separate from handler logic.

- `RegisterRoutes(app *fiber.App, h *Handlers)` -- wires all routes
- API group: `/api/ingest`, `/api/services`, `/api/traces`, `/api/traces/:traceID`, `/api/traces/:traceID/spans/:spanID`, `/api/dependencies`, `/api/status`, `/api/purge`
- Static: `/*` serves embedded UI assets with SPA fallback

## Handlers

`internal/server/handlers.go`

**Responsibility:** Request parsing, calling store methods, formatting responses.

```go
type Handlers struct {
    Store     *store.Store
    Buffer    *SpanBuffer
    StartTime time.Time  // for uptime calculation
    Version   string
}
```

Each handler is a method on `Handlers` with signature `func(fiber.Ctx) error`. Handlers parse query params / path params, call the appropriate store method, and return `APIResponse` JSON.

## Ingest Handler and SpanBuffer

`internal/server/ingest.go`

**Responsibility:** Receives span data via POST, converts to store format, buffers in RAM, batch-writes to SQLite.

**Ingest flow:**
1. Handler parses `IngestRequest` JSON body
2. Converts `IngestSpan` slices to `store.Span` slices (serializes attributes/events maps to `json.RawMessage`)
3. Pushes spans into `SpanBuffer.Add()`
4. Returns 202 immediately

**SpanBuffer:**
- Runs a background goroutine (same pattern as the existing OTLP exporter)
- Collects spans from a buffered channel
- Flushes to `Store.InsertSpans()` when batch size is reached or flush interval elapses
- On shutdown: drains the channel, flushes remaining spans

## Store

`internal/store/store.go`

**Responsibility:** SQLite database access -- schema creation, span insertion, querying, purging, stats.

- Opens database with `modernc.org/sqlite` driver via `database/sql`
- Applies PRAGMAs (WAL mode, synchronous=NORMAL, busy_timeout=5000)
- Runs idempotent schema creation (`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`)
- `InsertSpans` uses a transaction with a prepared `INSERT OR REPLACE INTO spans` statement
- Query methods build SQL with optional WHERE clauses based on query parameters
- `GetTrace` queries flat spans by trace_id, then builds `SpanNode` trees in-memory (index by spanID, walk to build children, roots = `parent_span_id IS NULL` + orphans whose parent is missing, children and roots sorted by startTime). Returns `TraceDetail` with `Roots []SpanNode` and aggregate stats.
- `PurgeOlderThan` deletes spans where `start_time < threshold`
- `Stats` queries span count, trace count (COUNT DISTINCT), DB file size (via `PRAGMA page_count * page_size`), min/max start_time

## Config

`internal/config/config.go`

**Responsibility:** Loads YAML configuration from file, applies defaults, validates.

- Searches for config file in order: explicit `--config` flag, `/etc/pocket-trace/config.yaml` (CWD-relative search removed — unreliable for daemons)
- Merges file values over defaults
- Validates: listen address format, positive buffer size, positive flush interval, valid log level

## Daemon Manager

`internal/daemon/daemon.go` + `internal/daemon/systemd.go`

**Responsibility:** Platform-specific service installation and management.

**Install workflow (systemd):**
1. Resolve absolute path to the pocket-trace binary
2. Write systemd unit file to `/etc/systemd/system/pocket-trace.service`
3. Run `systemctl daemon-reload`
4. Run `systemctl enable pocket-trace`
5. Run `systemctl start pocket-trace`

**Uninstall workflow (systemd):**
1. Run `systemctl stop pocket-trace`
2. Run `systemctl disable pocket-trace`
3. Remove unit file
4. Run `systemctl daemon-reload`

**Status:** Parses `systemctl show pocket-trace` output for ActiveState, SubState, MainPID, etc.

## Retention Purger

Runs inside the daemon's main loop (not a separate component file -- embedded in `server.go`).

- Starts a ticker based on config (e.g., runs every hour)
- Calls `Store.PurgeOlderThan(now - retention)`
- Logs the number of deleted spans

## Embedded UI

`cmd/pocket-trace/embed.go`

**Responsibility:** Embeds the built React app via `//go:embed`. Located in `cmd/pocket-trace/` (not the root package) to avoid import cycles — `internal/server/` cannot import the root `trace` package without pulling in the entire library.

```go
//go:embed all:ui/dist
var uiFS embed.FS
```

The embedded `fs.FS` is passed to `server.New()` as a parameter. The server uses `io/fs.Sub` to strip the `ui/dist` prefix and serves through Fiber's static middleware. A build tag (`embed_dev.go`) allows development without the embedded assets by providing an empty FS.

## HTTP Exporter (Library Side)

`exporter.go` (package root, alongside `trace.go`)

**Responsibility:** Batches finished spans and POSTs them as JSON to the daemon.

- Same architecture as the existing OTLP exporter: buffered channel, background goroutine, ticker-based flushing
- Converts `FinishedSpan` to `IngestSpan`, groups by service name into `IngestRequest`
- POSTs `application/json` to `{endpoint}/api/ingest`
- Non-blocking: drops spans if buffer is full (logs warning)
- On shutdown: drains channel, flushes remaining spans

## CLI Commands

`cmd/pocket-trace/main.go` and sibling files.

- **Root command (no subcommand):** Loads config, opens store, creates server, starts listening. Handles SIGINT/SIGTERM for graceful shutdown.
- **install:** Calls `DaemonManager.Install()`. Requires root/sudo.
- **uninstall:** Calls `DaemonManager.Uninstall()`. Requires root/sudo.
- **status:** Calls `GET /api/status` on the running daemon (HTTP request to localhost), also calls `DaemonManager.Status()` for service info. Prints formatted output.
- **purge:** Accepts `--older-than` flag (duration string). Sends `POST /api/purge` to the running daemon (same pattern as `status`). This avoids dual-writer SQLite issues. Requires the daemon to be running.
