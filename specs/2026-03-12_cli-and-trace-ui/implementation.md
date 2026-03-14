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

- [ ] Create `exporter.go` at package root with `HTTPExporter` struct
- [ ] Implement `NewHTTPExporter(endpoint string, opts ...ExporterOption) *HTTPExporter`
- [ ] Implement `ExportSpan(ctx, *FinishedSpan)` -- non-blocking push to buffered channel
- [ ] Implement `Shutdown(ctx) error` -- drain channel, flush remaining, close
- [ ] Implement background `run()` goroutine with batch size and ticker-based flushing
- [ ] Implement `flush()` -- convert `[]*FinishedSpan` to `IngestRequest` JSON, POST to `{endpoint}/api/ingest`. Convert `SpanStatus` int to string ("UNSET"/"OK"/"ERROR"). Hardcode `SpanKind: 1` (internal) since `FinishedSpan` has no kind field. Convert zero `ParentID` to empty string (omitted via omitempty).
- [ ] Implement `ExporterOption` functions: `WithBatchSize`, `WithFlushInterval`
- [ ] Write unit tests: queuing, batching, flush on timer, flush on shutdown, drop on full buffer, error handling
- [ ] Update `examples/main.go` to use `trace.NewHTTPExporter("http://localhost:7070")` instead of `otlp.NewExporter`

## Phase 3: Daemon Core

Build the Fiber server, SQLite store, ingest endpoint with RAM buffer, and config loading.

### Config
- [ ] Create `internal/config/config.go` with `Config` struct and default values
- [ ] Implement `Load(path string) (*Config, error)` -- read YAML, merge with defaults
- [ ] Implement `Default() *Config`
- [ ] Write config unit tests

### Store
- [ ] Create `internal/store/store.go` with `Store` struct and domain types (`Span`, `ServiceSummary`, `TraceSummary`, `Dependency`, `DBStats`, `TraceQuery`)
- [ ] Implement `New(dbPath string) (*Store, error)` -- open DB, set PRAGMAs, create schema
- [ ] Implement `Close() error`
- [ ] Implement `InsertSpans(ctx, []Span) error` -- transaction with prepared INSERT OR REPLACE
- [ ] Write store insert tests (single, batch, idempotent upsert)

### SpanBuffer
- [ ] Create `internal/server/ingest.go` with `SpanBuffer` struct, `IngestRequest`/`IngestSpan`/`IngestEvent` types
- [ ] Implement `NewSpanBuffer(store, batchSize, flushInterval) *SpanBuffer`
- [ ] Implement `Add([]store.Span)` -- push to buffered channel
- [ ] Implement background flush goroutine (same pattern as exporter)
- [ ] Implement `Shutdown()` -- drain and flush
- [ ] Write buffer unit tests

### Server
- [ ] Create `internal/server/server.go` with `Server` struct
- [ ] Initialize Fiber app with recovery middleware and JSON error handler
- [ ] Create `internal/server/routes.go` with `RegisterRoutes`
- [ ] Create `internal/server/handlers.go` with `Handlers` struct
- [ ] Implement `POST /api/ingest` handler -- parse JSON, convert to store.Span (convert empty ParentSpanID to SQL NULL for root spans), push to buffer, return 202
- [ ] Wire root command to load config, open store, create server, start listening
- [ ] Add SIGINT/SIGTERM handling for graceful shutdown (flush buffer, close store)
- [ ] Write ingest integration test (POST spans, verify in DB)

## Phase 4: Query API

Implement all query endpoints and the store methods that back them.

- [ ] Implement `Store.ListServices(ctx) ([]ServiceSummary, error)`
- [ ] Implement `GET /api/services` handler
- [ ] Write services query tests
- [ ] Implement `Store.SearchTraces(ctx, TraceQuery) ([]TraceSummary, error)` -- dynamic WHERE clause building
- [ ] Implement `GET /api/traces` handler with query param parsing
- [ ] Write search traces tests (all filter combinations, limit, ordering)
- [ ] Implement `Store.GetTrace(ctx, traceID) (*TraceDetail, error)` -- query flat spans, build tree in-memory (index by spanID, walk to build children, roots = `parent_span_id IS NULL` + orphans whose parent is missing, sort children and roots by startTime), compute aggregate stats from all spans
- [ ] Implement `GET /api/traces/:traceID` handler -- returns `TraceDetail` with `Roots []SpanNode`
- [ ] Write get trace tests (verify tree structure, children ordering, root detection, orphan promotion, aggregate stats)
- [ ] Implement `Store.GetSpan(ctx, traceID, spanID) (*Span, error)`
- [ ] Implement `GET /api/traces/:traceID/spans/:spanID` handler
- [ ] Write get span tests
- [ ] Implement `Store.GetDependencies(ctx, since time.Time) ([]Dependency, error)` -- self-join on spans by trace_id where service_name differs
- [ ] Implement `GET /api/dependencies` handler with lookback parsing
- [ ] Write dependency tests
- [ ] Implement `Store.Stats(ctx) (*DBStats, error)`
- [ ] Implement `GET /api/status` handler
- [ ] Write status tests
- [ ] Implement `Store.PurgeOlderThan(ctx, before time.Time) (int64, error)`
- [ ] Implement `POST /api/purge` handler with `olderThan` query param parsing
- [ ] Write purge tests
- [ ] Add retention purger ticker to server startup (uses `Config.PurgeInterval`, calls PurgeOlderThan)

## Phase 5: Daemon Management

Implement DaemonManager interface, systemd implementation, and CLI commands.

- [ ] Create `internal/daemon/daemon.go` with `DaemonManager` interface, `ServiceStatus` struct, and `NewDaemonManager()` factory
- [ ] Create `internal/daemon/systemd.go` with `SystemdManager` struct
- [ ] Implement `SystemdManager.Install(binaryPath, configPath)` -- write unit file, daemon-reload, enable, start
- [ ] Implement `SystemdManager.Uninstall()` -- stop, disable, remove unit file, daemon-reload
- [ ] Implement `SystemdManager.Status()` -- parse systemctl show output
- [ ] Wire `cmd/pocket-trace/install.go` -- resolve binary path, call DaemonManager.Install
- [ ] Wire `cmd/pocket-trace/uninstall.go` -- call DaemonManager.Uninstall
- [ ] Wire `cmd/pocket-trace/status.go` -- call /api/status and DaemonManager.Status, format output
- [ ] Wire `cmd/pocket-trace/purge.go` -- parse --older-than, send POST /api/purge to running daemon, print result
- [ ] Write daemon manager tests (mock exec.Command)

## Phase 6: UI Application

Build all React pages and components.

### Foundation
- [ ] Set up react-router-dom with route definitions (/ redirects to /services, /services, /search, /traces/:traceID, /dependencies)
- [ ] Create `RootLayout` component with `Sidebar` and `Outlet`
- [ ] Create `Sidebar` with `Navigation`, `NavItem` components, and `DaemonStatus`
- [ ] Set up TanStack Query client provider
- [ ] Create API client module (`ui/src/api/client.ts`) with typed fetch functions for all endpoints
- [ ] Create custom hooks: `useServices`, `useTraces`, `useTrace`, `useDependencies`, `useStatus`

### Shared Components
- [ ] Create `PageHeader` component
- [ ] Create `ServiceBadge` component (deterministic color from service name hash)
- [ ] Create `StatusBadge` component (OK/ERROR/UNSET)
- [ ] Create `DurationBar` component (shared, used in search results and trace waterfall)
- [ ] Create `ErrorBadge` component
- [ ] Create `TimeDisplay` component (formats unix nanos)
- [ ] Create `CopyButton` component

### Services Page
- [ ] Create `ServicesPage` with `ServiceList` and `ServiceCard`
- [ ] Wire to `useServices` hook
- [ ] Click-through to search page with service filter

### Search Page
- [ ] Create `SearchPage` with `SearchFilters` and `TraceList`
- [ ] Create filter components: `ServiceSelect`, `SpanNameInput`, `DurationRange`, `TimeRange`
- [ ] Create `TraceRow` component with duration bar, badges, timestamp
- [ ] Wire to `useTraces` hook with filter params
- [ ] Sync filter state to URL query params

### Trace Page
- [ ] Create `TracePage` with `TraceTimeline` and `SpanDetail` panel
- [ ] Create `SpanTree` with recursive `SpanRow` components (tree is pre-built by API, iterate `roots[]` and render each root's `children` recursively)
- [ ] Create `TimeRuler` component for timeline header
- [ ] Implement `DurationBar` positioning logic for trace waterfall (offset from trace start, width from span duration)
- [ ] Create `CollapseToggle` for expand/collapse subtrees
- [ ] Create `SpanDetail` panel with attributes table and events list
- [ ] Wire to `useTrace` hook
- [ ] Implement span row click to show/hide detail panel

### Dependencies Page
- [ ] Create `DependenciesPage` with `LookbackSelect`
- [ ] Create `DependencyGraph` component (list/table view for MVP: parent -> child with call count)
- [ ] Wire to `useDependencies` hook

## Phase 7: Embedding and Integration

Embed the React app into the Go binary and run end-to-end.

- [ ] Create `cmd/pocket-trace/embed.go` with `//go:embed all:ui/dist` directive (in cmd/, not root, to avoid import cycles)
- [ ] Create `cmd/pocket-trace/embed_dev.go` with build tag for development (empty FS)
- [ ] Update `server.New()` to accept `fs.FS` parameter for embedded UI assets
- [ ] Add static file serving to Fiber routes (serve from embedded FS, SPA fallback to index.html)
- [ ] Create `Makefile` or build script: `cd ui && npm run build && cd .. && go build -ldflags "-X main.version=..." ./cmd/pocket-trace`
- [ ] Add `version` variable in `cmd/pocket-trace/main.go` set via `-ldflags` at build time
- [ ] Update `examples/main.go` to work with new exporter
- [ ] Run end-to-end test: build binary, start daemon, run example app, verify traces visible via API
- [ ] Test static UI serving: fetch /, /services, /assets/*.js
- [ ] Test SPA fallback: fetch /traces/some-id returns index.html
- [ ] Verify purge command works against running daemon (via POST /api/purge)
- [ ] Verify install/uninstall commands produce correct systemd unit file

## Notes

- The existing `trace.go` and `trace_test.go` do NOT change. The library API is stable. `FinishedSpan` has no `SpanKind` field -- the exporter hardcodes `1` (internal).
- The `exporter.go` (new) replaces `otlp/exporter.go` (deleted). Same pattern, JSON instead of protobuf. Must convert `SpanStatus` int → string and zero `ParentID` → empty string.
- Fiber v3 uses `fiber.Ctx` (interface) not `*fiber.Ctx` (pointer). JSON binding is `c.Bind().Body(&req)`.
- Fiber v3 removed `app.Static()` -- use the static middleware instead.
- modernc.org/sqlite registers as `"sqlite"` driver with `database/sql`. Import with blank identifier: `_ "modernc.org/sqlite"`.
- The UI build output (`ui/dist/`) should be gitignored. The `//go:embed` directive references it at build time.
