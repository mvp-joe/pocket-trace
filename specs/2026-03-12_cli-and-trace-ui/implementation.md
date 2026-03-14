# Implementation Plan

## Phase 1: Project Scaffolding

Set up directory structure, Cobra CLI skeleton, Vite/React project, and dependency management.

- [x] Create directory structure: `cmd/pocket-trace/`, `internal/daemon/`, `internal/server/`, `internal/store/`, `internal/config/`, `ui/`
- [x] Update `go.mod`: remove OTLP/protobuf/x-net deps, add cobra, fiber/v3, modernc.org/sqlite, gopkg.in/yaml.v3
- [x] Create `cmd/pocket-trace/main.go` with Cobra root command (stub: prints "daemon starting")
- [x] Create `cmd/pocket-trace/install.go` with stub install command
- [x] Create `cmd/pocket-trace/uninstall.go` with stub uninstall command
- [x] Create `cmd/pocket-trace/status.go` with stub status command
- [x] Create `cmd/pocket-trace/purge.go` with stub purge command (with `--older-than` flag)
- [x] Initialize Vite + React + TypeScript project in `ui/` (`npm create vite@latest . -- --template react-ts`)
- [x] Install UI deps: react-router-dom, @tanstack/react-query, tailwindcss v4, lucide-react, date-fns
- [x] Set up shadcn/ui in the Vite project
- [x] Configure Tailwind CSS v4
- [x] Verify `go build ./cmd/pocket-trace` compiles
- [x] Verify `cd ui && npm run build` produces `ui/dist/`
- [x] Update `.gitignore` with `ui/dist/` and `ui/node_modules/`
- [x] Delete `otlp/` directory and `scripts/` directory (if they exist)

## Phase 2: New JSON Exporter

Replace the OTLP exporter with a JSON HTTP POST exporter. Same batching pattern, different wire format.

- [x] Create `exporter.go` at package root with `HTTPExporter` struct
- [x] Implement `NewHTTPExporter(endpoint string, opts ...ExporterOption) *HTTPExporter`
- [x] Implement `ExportSpan(ctx, *FinishedSpan)` -- non-blocking push to buffered channel
- [x] Implement `Shutdown(ctx) error` -- drain channel, flush remaining, close
- [x] Implement background `run()` goroutine with batch size and ticker-based flushing
- [x] Implement `flush()` -- convert `[]*FinishedSpan` to `IngestRequest` JSON, POST to `{endpoint}/api/ingest`. Convert `SpanStatus` int to string ("UNSET"/"OK"/"ERROR"). Hardcode `SpanKind: 1` (internal) since `FinishedSpan` has no kind field. Convert zero `ParentID` to empty string (omitted via omitempty).
- [x] Implement `ExporterOption` functions: `WithBatchSize`, `WithFlushInterval`
- [x] Write unit tests: queuing, batching, flush on timer, flush on shutdown, drop on full buffer, error handling
- [x] Update `examples/main.go` to use `trace.NewHTTPExporter("http://localhost:7070")` instead of `otlp.NewExporter`

## Phase 3: Daemon Core

Build the Fiber server, SQLite store, ingest endpoint with RAM buffer, and config loading.

### Config
- [x] Create `internal/config/config.go` with `Config` struct and default values
- [x] Implement `Load(path string) (*Config, error)` -- read YAML, merge with defaults
- [x] Implement `Default() *Config`
- [x] Write config unit tests

### Store
- [x] Create `internal/store/store.go` with `Store` struct and domain types (`Span`, `ServiceSummary`, `TraceSummary`, `Dependency`, `DBStats`, `TraceQuery`)
- [x] Implement `New(dbPath string) (*Store, error)` -- open DB, set PRAGMAs, create schema
- [x] Implement `Close() error`
- [x] Implement `InsertSpans(ctx, []Span) error` -- transaction with prepared INSERT OR REPLACE
- [x] Write store insert tests (single, batch, idempotent upsert)

### SpanBuffer
- [x] Create `internal/server/ingest.go` with `SpanBuffer` struct, `IngestRequest`/`IngestSpan`/`IngestEvent` types
- [x] Implement `NewSpanBuffer(store, batchSize, flushInterval) *SpanBuffer`
- [x] Implement `Add([]store.Span)` -- push to buffered channel
- [x] Implement background flush goroutine (same pattern as exporter)
- [x] Implement `Shutdown()` -- drain and flush
- [x] Write buffer unit tests

### Server
- [x] Create `internal/server/server.go` with `Server` struct
- [x] Initialize Fiber app with recovery middleware and JSON error handler
- [x] Create `internal/server/routes.go` with `RegisterRoutes`
- [x] Create `internal/server/handlers.go` with `Handlers` struct
- [x] Implement `POST /api/ingest` handler -- parse JSON, convert to store.Span (convert empty ParentSpanID to SQL NULL for root spans), push to buffer, return 202
- [x] Wire root command to load config, open store, create server, start listening
- [x] Add SIGINT/SIGTERM handling for graceful shutdown (flush buffer, close store)
- [x] Write ingest integration test (POST spans, verify in DB)

## Phase 4: Query API

Implement all query endpoints and the store methods that back them.

- [x] Implement `Store.ListServices(ctx) ([]ServiceSummary, error)`
- [x] Implement `GET /api/services` handler
- [x] Write services query tests
- [x] Implement `Store.SearchTraces(ctx, TraceQuery) ([]TraceSummary, error)` -- dynamic WHERE clause building
- [x] Implement `GET /api/traces` handler with query param parsing
- [x] Write search traces tests (all filter combinations, limit, ordering)
- [x] Implement `Store.GetTrace(ctx, traceID) (*TraceDetail, error)` -- query flat spans, build tree in-memory (index by spanID, walk to build children, roots = `parent_span_id IS NULL` + orphans whose parent is missing, sort children and roots by startTime), compute aggregate stats from all spans
- [x] Implement `GET /api/traces/:traceID` handler -- returns `TraceDetail` with `Roots []SpanNode`
- [x] Write get trace tests (verify tree structure, children ordering, root detection, orphan promotion, aggregate stats)
- [x] Implement `Store.GetSpan(ctx, traceID, spanID) (*Span, error)`
- [x] Implement `GET /api/traces/:traceID/spans/:spanID` handler
- [x] Write get span tests
- [x] Implement `Store.GetDependencies(ctx, since time.Time) ([]Dependency, error)` -- self-join on spans by trace_id where service_name differs
- [x] Implement `GET /api/dependencies` handler with lookback parsing
- [x] Write dependency tests
- [x] Implement `Store.Stats(ctx) (*DBStats, error)`
- [x] Implement `GET /api/status` handler
- [x] Write status tests
- [x] Implement `Store.PurgeOlderThan(ctx, before time.Time) (int64, error)`
- [x] Implement `POST /api/purge` handler with `olderThan` query param parsing
- [x] Write purge tests
- [x] Add retention purger ticker to server startup (uses `Config.PurgeInterval`, calls PurgeOlderThan)

## Phase 5: Daemon Management

Implement DaemonManager interface, systemd implementation, and CLI commands.

- [x] Create `internal/daemon/daemon.go` with `DaemonManager` interface, `ServiceStatus` struct, and `NewDaemonManager()` factory
- [x] Create `internal/daemon/systemd.go` with `SystemdManager` struct
- [x] Implement `SystemdManager.Install(binaryPath, configPath)` -- write unit file, daemon-reload, enable, start
- [x] Implement `SystemdManager.Uninstall()` -- stop, disable, remove unit file, daemon-reload
- [x] Implement `SystemdManager.Status()` -- parse systemctl show output
- [x] Wire `cmd/pocket-trace/install.go` -- resolve binary path, call DaemonManager.Install
- [x] Wire `cmd/pocket-trace/uninstall.go` -- call DaemonManager.Uninstall
- [x] Wire `cmd/pocket-trace/status.go` -- call /api/status and DaemonManager.Status, format output
- [x] Wire `cmd/pocket-trace/purge.go` -- parse --older-than, send POST /api/purge to running daemon, print result
- [x] Write daemon manager tests (mock exec.Command)

## Phase 6: UI Application

Build all React pages and components.

### Foundation
- [x] Set up react-router-dom with route definitions (/ redirects to /services, /services, /search, /traces/:traceID, /dependencies)
- [x] Create `RootLayout` component with `Sidebar` and `Outlet`
- [x] Create `Sidebar` with `Navigation`, `NavItem` components, and `DaemonStatus`
- [x] Set up TanStack Query client provider
- [x] Create API client module (`ui/src/api/client.ts`) with typed fetch functions for all endpoints
- [x] Create custom hooks: `useServices`, `useTraces`, `useTrace`, `useDependencies`, `useStatus`

### Shared Components
- [x] Create `PageHeader` component
- [x] Create `ServiceBadge` component (deterministic color from service name hash)
- [x] Create `StatusBadge` component (OK/ERROR/UNSET)
- [x] Create `DurationBar` component (shared, used in search results and trace waterfall)
- [x] Create `ErrorBadge` component
- [x] Create `TimeDisplay` component (formats unix nanos)
- [x] Create `CopyButton` component

### Services Page
- [x] Create `ServicesPage` with `ServiceList` and `ServiceCard`
- [x] Wire to `useServices` hook
- [x] Click-through to search page with service filter

### Search Page
- [x] Create `SearchPage` with `SearchFilters` and `TraceList`
- [x] Create filter components: `ServiceSelect`, `SpanNameInput`, `DurationRange`, `TimeRange`
- [x] Create `TraceRow` component with duration bar, badges, timestamp
- [x] Wire to `useTraces` hook with filter params
- [x] Sync filter state to URL query params

### Trace Page
- [x] Create `TracePage` with `TraceTimeline` and `SpanDetail` panel
- [x] Create `SpanTree` with recursive `SpanRow` components (tree is pre-built by API, iterate `roots[]` and render each root's `children` recursively)
- [x] Create `TimeRuler` component for timeline header
- [x] Implement `DurationBar` positioning logic for trace waterfall (offset from trace start, width from span duration)
- [x] Create `CollapseToggle` for expand/collapse subtrees
- [x] Create `SpanDetail` panel with attributes table and events list
- [x] Wire to `useTrace` hook
- [x] Implement span row click to show/hide detail panel

### Dependencies Page
- [x] Create `DependenciesPage` with `LookbackSelect`
- [x] Create `DependencyGraph` component (list/table view for MVP: parent -> child with call count)
- [x] Wire to `useDependencies` hook

## Phase 7: Embedding and Integration

Embed the React app into the Go binary and run end-to-end.

- [x] Create `cmd/pocket-trace/embed.go` with `//go:embed all:ui/dist` directive (in cmd/, not root, to avoid import cycles)
- [x] Create `cmd/pocket-trace/embed_dev.go` with build tag for development (empty FS)
- [x] Update `server.New()` to accept `fs.FS` parameter for embedded UI assets
- [x] Add static file serving to Fiber routes (serve from embedded FS, SPA fallback to index.html)
- [x] Create `Makefile` or build script: `cd ui && npm run build && cd .. && go build -ldflags "-X main.version=..." ./cmd/pocket-trace`
- [x] Add `version` variable in `cmd/pocket-trace/main.go` set via `-ldflags` at build time
- [x] Update `examples/main.go` to work with new exporter
- [x] Run end-to-end test: build binary, start daemon, run example app, verify traces visible via API
- [x] Test static UI serving: fetch /, /services, /assets/*.js
- [x] Test SPA fallback: fetch /traces/some-id returns index.html
- [x] Verify purge command works against running daemon (via POST /api/purge)
- [x] Verify install/uninstall commands produce correct systemd unit file

## Notes

- The existing `trace.go` and `trace_test.go` do NOT change. The library API is stable. `FinishedSpan` has no `SpanKind` field -- the exporter hardcodes `1` (internal).
- The `exporter.go` (new) replaces `otlp/exporter.go` (deleted). Same pattern, JSON instead of protobuf. Must convert `SpanStatus` int → string and zero `ParentID` → empty string.
- Fiber v3 uses `fiber.Ctx` (interface) not `*fiber.Ctx` (pointer). JSON binding is `c.Bind().Body(&req)`.
- Fiber v3 removed `app.Static()` -- use the static middleware instead.
- modernc.org/sqlite registers as `"sqlite"` driver with `database/sql`. Import with blank identifier: `_ "modernc.org/sqlite"`.
- The UI build output (`ui/dist/`) should be gitignored. The `//go:embed` directive references it at build time.
