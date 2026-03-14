# Interface Definitions

## Domain Types

Located in `internal/store/store.go`:

```go
// Span represents a stored span. JSON tags are used for API responses.
type Span struct {
    TraceID      string          `json:"traceId"`
    SpanID       string          `json:"spanId"`
    ParentSpanID string          `json:"parentSpanId,omitempty"`
    ServiceName  string          `json:"serviceName"`
    SpanName     string          `json:"spanName"`
    SpanKind     int             `json:"spanKind"`
    StartTime    int64           `json:"startTime"`    // unix nanos
    EndTime      int64           `json:"endTime"`      // unix nanos
    DurationMs   int64           `json:"durationMs"`
    StatusCode   string          `json:"statusCode"`   // "OK", "ERROR", "UNSET"
    StatusMsg    string          `json:"statusMessage,omitempty"`
    Attributes   json.RawMessage `json:"attributes,omitempty"`
    Events       json.RawMessage `json:"events,omitempty"`
}
```

## Ingest Types

Located in `internal/server/ingest.go`:

```go
// IngestRequest is the JSON body sent by the library exporter.
type IngestRequest struct {
    ServiceName string      `json:"serviceName"`
    Spans       []IngestSpan `json:"spans"`
}

// IngestSpan is a single span in the ingest payload.
type IngestSpan struct {
    TraceID      string `json:"traceId"`
    SpanID       string `json:"spanId"`
    ParentSpanID string `json:"parentSpanId,omitempty"`
    Name         string `json:"name"`
    SpanKind     int    `json:"spanKind"`
    StartTime    int64  `json:"startTime"`  // unix nanos
    EndTime      int64  `json:"endTime"`    // unix nanos
    StatusCode   string `json:"statusCode"`
    StatusMsg    string `json:"statusMessage,omitempty"`
    Attributes   map[string]any `json:"attributes,omitempty"`
    Events       []IngestEvent  `json:"events,omitempty"`
}

// IngestEvent is a span event in the ingest payload.
type IngestEvent struct {
    Name       string         `json:"name"`
    Time       int64          `json:"time"` // unix nanos
    Attributes map[string]any `json:"attributes,omitempty"`
}

// IngestResult is returned by POST /api/ingest.
type IngestResult struct {
    Accepted int `json:"accepted"`
}
```

## Store Interface

Located in `internal/store/store.go`:

```go
// Store provides span persistence and querying.
type Store struct {
    db *sql.DB
}

func New(dbPath string) (*Store, error)
func (s *Store) Close() error

// Write operations
func (s *Store) InsertSpans(ctx context.Context, spans []Span) error
func (s *Store) PurgeOlderThan(ctx context.Context, before time.Time) (int64, error)

// Query operations
func (s *Store) ListServices(ctx context.Context) ([]ServiceSummary, error)
func (s *Store) SearchTraces(ctx context.Context, q TraceQuery) ([]TraceSummary, error)
func (s *Store) GetTrace(ctx context.Context, traceID string) (*TraceDetail, error)
func (s *Store) GetSpan(ctx context.Context, traceID, spanID string) (*Span, error)
func (s *Store) GetDependencies(ctx context.Context, since time.Time) ([]Dependency, error)

// Stats
func (s *Store) Stats(ctx context.Context) (*DBStats, error)
```

`GetTrace` queries all spans for a trace ID, then builds the tree in-memory:
1. Query all spans by `trace_id`, get flat `[]Span`
2. Index spans by `spanID` in a map
3. Walk spans, appending each to its parent's `Children` slice
4. Root spans are those with `parent_span_id IS NULL`
5. **Orphan spans** (non-NULL `parent_span_id` referencing a span not in the result set) are promoted to roots. This handles partially purged traces, late-arriving spans, and cross-trace parent references. No spans are silently dropped — every span in the query result appears in the tree.
6. All root and orphan spans go into `TraceDetail.Roots`, sorted by `startTime`. Typically one element; multiple only when orphans exist.
7. Children sorted by `startTime` at each level
8. Wrap in `TraceDetail` with aggregate stats (span count, service count, duration, error count) computed from ALL spans regardless of tree position

## Query and Response Types

Located in `internal/store/store.go`:

```go
// TraceQuery defines search parameters for trace listing.
type TraceQuery struct {
    ServiceName string
    SpanName    string
    MinDuration int64  // ms, 0 = no minimum
    MaxDuration int64  // ms, 0 = no maximum
    Start       time.Time
    End         time.Time
    Limit       int    // default 20, max 100
}

// ServiceSummary represents a service with aggregated stats.
type ServiceSummary struct {
    Name      string `json:"name"`
    SpanCount int64  `json:"spanCount"`
    LastSeen  int64  `json:"lastSeen"` // unix nanos
}

// TraceSummary represents a trace in search results.
// RootSpan is the name of the earliest span with parent_span_id IS NULL.
// Service is the service name of that same root span.
type TraceSummary struct {
    TraceID    string `json:"traceId"`
    RootSpan   string `json:"rootSpan"`
    Service    string `json:"serviceName"`
    StartTime  int64  `json:"startTime"`
    DurationMs int64  `json:"durationMs"`
    SpanCount  int    `json:"spanCount"`
    ErrorCount int    `json:"errorCount"`
}

// SpanNode is a span with its children, forming a tree structure.
// Used by GET /api/traces/:traceID to return pre-built trace trees.
// Tree is built server-side so consumers (UI, LLMs) don't need to
// reconstruct parent-child relationships from a flat list.
type SpanNode struct {
    TraceID      string          `json:"traceId"`
    SpanID       string          `json:"spanId"`
    ParentSpanID string          `json:"parentSpanId,omitempty"`
    ServiceName  string          `json:"serviceName"`
    SpanName     string          `json:"spanName"`
    SpanKind     int             `json:"spanKind"`
    StartTime    int64           `json:"startTime"`    // unix nanos
    EndTime      int64           `json:"endTime"`      // unix nanos
    DurationMs   int64           `json:"durationMs"`
    StatusCode   string          `json:"statusCode"`
    StatusMsg    string          `json:"statusMessage,omitempty"`
    Attributes   json.RawMessage `json:"attributes,omitempty"`
    Events       json.RawMessage `json:"events,omitempty"`
    Children     []SpanNode      `json:"children"`
}

// TraceDetail is the response for GET /api/traces/:traceID.
// Contains the full trace as a tree. Roots is typically a single-element
// slice (one root span), but may contain multiple entries when orphan spans
// exist (parent was purged or references a span not in the result set).
// Orphan spans are promoted to roots rather than silently dropped.
type TraceDetail struct {
    TraceID      string     `json:"traceId"`
    Roots        []SpanNode `json:"roots"`
    SpanCount    int        `json:"spanCount"`
    ServiceCount int        `json:"serviceCount"`
    DurationMs   int64      `json:"durationMs"`
    ErrorCount   int        `json:"errorCount"`
}

// Dependency represents a service-to-service call.
type Dependency struct {
    Parent    string `json:"parent"`
    Child     string `json:"child"`
    CallCount int64  `json:"callCount"`
}

// DBStats contains database health information.
type DBStats struct {
    SpanCount   int64  `json:"spanCount"`
    TraceCount  int64  `json:"traceCount"`
    DBSizeBytes int64  `json:"dbSizeBytes"`
    OldestSpan  int64  `json:"oldestSpan"`  // unix nanos, 0 if empty
    NewestSpan  int64  `json:"newestSpan"`  // unix nanos, 0 if empty
}
```

## API Result Types

Located in `internal/server/handlers.go`:

```go
// PurgeResult is returned by POST /api/purge.
type PurgeResult struct {
    Deleted int64 `json:"deleted"`
}
```

## Config Types

Located in `internal/config/config.go`:

```go
// Config holds daemon configuration loaded from file or defaults.
type Config struct {
    Listen        string        `yaml:"listen"`          // default ":7070"
    DBPath        string        `yaml:"db_path"`         // default "/var/lib/pocket-trace/pocket-trace.db"
    Retention     time.Duration `yaml:"retention"`       // default 168h (7 days)
    PurgeInterval time.Duration `yaml:"purge_interval"`  // default 1h
    BufferSize    int           `yaml:"buffer_size"`     // default 4096 spans
    FlushInterval time.Duration `yaml:"flush_interval"`  // default 2s
    LogLevel      string        `yaml:"log_level"`       // default "info"
}

func Load(path string) (*Config, error)
func Default() *Config
```

Config file search order: flag `--config`, then `/etc/pocket-trace/config.yaml`. The systemd unit file should always pass `--config /etc/pocket-trace/config.yaml` explicitly. Searching relative to CWD (`./pocket-trace.yaml`) is unreliable for daemons. If no file found, use defaults.

## DaemonManager Interface

Located in `internal/daemon/daemon.go`:

```go
// DaemonManager handles platform-specific service lifecycle.
type DaemonManager interface {
    Install(binaryPath string, configPath string) error
    Uninstall() error
    Status() (*ServiceStatus, error)
}

// ServiceStatus reports the current state of the daemon service.
type ServiceStatus struct {
    Running    bool   `json:"running"`
    Enabled    bool   `json:"enabled"`
    PID        int    `json:"pid,omitempty"`
    Uptime     string `json:"uptime,omitempty"`
}

// NewDaemonManager returns the platform-appropriate DaemonManager.
func NewDaemonManager() (DaemonManager, error)
```

Located in `internal/daemon/systemd.go`:

```go
// SystemdManager implements DaemonManager for Linux systems using systemd.
type SystemdManager struct{}

func (m *SystemdManager) Install(binaryPath string, configPath string) error
func (m *SystemdManager) Uninstall() error
func (m *SystemdManager) Status() (*ServiceStatus, error)
```

## Buffer / Batch Writer

Located in `internal/server/ingest.go`:

```go
// SpanBuffer accumulates spans in memory and batch-writes them to the store.
type SpanBuffer struct {
    store         *store.Store
    spans         chan []store.Span // receives batches from ingest handler
    batchSize     int              // flush to DB when accumulated spans reach this count
    flushInterval time.Duration
    stop          chan struct{}
    wg            sync.WaitGroup
    stopOnce      sync.Once        // ensures Shutdown is safe to call multiple times
}

func NewSpanBuffer(store *store.Store, batchSize int, channelCap int, flushInterval time.Duration) *SpanBuffer
func (b *SpanBuffer) Add(spans []store.Span)
func (b *SpanBuffer) Shutdown()
```

## HTTP Exporter (Library Side)

Located in `exporter.go` (package root, alongside `trace.go`):

```go
// HTTPExporter sends finished spans to the pocket-trace daemon via JSON HTTP POST.
// It implements the Exporter interface from trace.go.
type HTTPExporter struct {
    endpoint      string
    client        *http.Client
    spans         chan *FinishedSpan
    stop          chan struct{}
    wg            sync.WaitGroup
    batchSize     int
    flushInterval time.Duration
}

// ExporterOption configures the HTTPExporter.
type ExporterOption func(*HTTPExporter)

func WithBatchSize(n int) ExporterOption
func WithFlushInterval(d time.Duration) ExporterOption

func NewHTTPExporter(endpoint string, opts ...ExporterOption) *HTTPExporter
func (e *HTTPExporter) ExportSpan(ctx context.Context, span *FinishedSpan)
func (e *HTTPExporter) Shutdown(ctx context.Context) error
```

### SpanStatus Conversion

The `HTTPExporter.flush()` method converts `FinishedSpan.Status` (a `SpanStatus int` enum) to the string representation used by `IngestSpan.StatusCode`:

| `SpanStatus` value | int | `StatusCode` string |
|---|---|---|
| `StatusUnset` | 0 | `"UNSET"` |
| `StatusOK` | 1 | `"OK"` |
| `StatusError` | 2 | `"ERROR"` |

### SpanKind

`FinishedSpan` does not have a `SpanKind` field. The `HTTPExporter` hardcodes `SpanKind: 1` (internal) for all spans. This matches the behavior of the existing OTLP exporter which hardcoded `SPAN_KIND_INTERNAL`. If span kinds are needed in the future, a `SpanKind` field can be added to `FinishedSpan` with a backward-compatible default.

### ParentSpanID Handling

`FinishedSpan.ParentID` is a `SpanID` (8-byte array). When zero (`SpanID{}`), the span is a root span. The exporter converts this to an empty string `""` in `IngestSpan.ParentSpanID` (omitted from JSON via `omitempty`). The ingest handler on the daemon side converts empty/missing `ParentSpanID` to SQL `NULL` when inserting into SQLite. All root span queries use `parent_span_id IS NULL`.

## API Response Envelope

Used across all API handlers in `internal/server/handlers.go`:

```go
// APIResponse wraps all API responses.
type APIResponse[T any] struct {
    Data  T      `json:"data"`
    Error string `json:"error,omitempty"`
}
```

## Status Response

Used by `GET /api/status` and the `status` CLI command:

```go
// StatusResponse reports daemon health.
type StatusResponse struct {
    Version   string        `json:"version"`
    Uptime    string        `json:"uptime"`
    DB        store.DBStats `json:"db"`
}
```
