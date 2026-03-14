package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Span represents a stored span. JSON tags are used for API responses.
type Span struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId,omitempty"`
	ServiceName  string          `json:"serviceName"`
	SpanName     string          `json:"spanName"`
	SpanKind     int             `json:"spanKind"`
	StartTime    int64           `json:"startTime"`
	EndTime      int64           `json:"endTime"`
	DurationMs   int64           `json:"durationMs"`
	StatusCode   string          `json:"statusCode"`
	StatusMsg    string          `json:"statusMessage,omitempty"`
	Attributes   json.RawMessage `json:"attributes,omitempty"`
	Events       json.RawMessage `json:"events,omitempty"`
}

// TraceQuery defines search parameters for trace listing.
type TraceQuery struct {
	ServiceName string
	SpanName    string
	MinDuration int64 // ms, 0 = no minimum
	MaxDuration int64 // ms, 0 = no maximum
	Start       time.Time
	End         time.Time
	Limit       int // default 20, max 100
}

// ServiceSummary represents a service with aggregated stats.
type ServiceSummary struct {
	Name      string `json:"name"`
	SpanCount int64  `json:"spanCount"`
	LastSeen  int64  `json:"lastSeen"`
}

// TraceSummary represents a trace in search results.
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
type SpanNode struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId,omitempty"`
	ServiceName  string          `json:"serviceName"`
	SpanName     string          `json:"spanName"`
	SpanKind     int             `json:"spanKind"`
	StartTime    int64           `json:"startTime"`
	EndTime      int64           `json:"endTime"`
	DurationMs   int64           `json:"durationMs"`
	StatusCode   string          `json:"statusCode"`
	StatusMsg    string          `json:"statusMessage,omitempty"`
	Attributes   json.RawMessage `json:"attributes,omitempty"`
	Events       json.RawMessage `json:"events,omitempty"`
	Children     []SpanNode      `json:"children"`
}

// TraceDetail is the response for GET /api/traces/:traceID.
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
	SpanCount   int64 `json:"spanCount"`
	TraceCount  int64 `json:"traceCount"`
	DBSizeBytes int64 `json:"dbSizeBytes"`
	OldestSpan  int64 `json:"oldestSpan"`
	NewestSpan  int64 `json:"newestSpan"`
}

// Store provides span persistence and querying.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS spans (
	trace_id       TEXT NOT NULL,
	span_id        TEXT NOT NULL,
	parent_span_id TEXT,
	service_name   TEXT NOT NULL,
	span_name      TEXT NOT NULL,
	span_kind      INTEGER,
	start_time     INTEGER NOT NULL,
	end_time       INTEGER NOT NULL,
	duration_ms    INTEGER NOT NULL,
	status_code    TEXT,
	status_message TEXT,
	attributes     TEXT,
	events         TEXT,
	PRIMARY KEY (trace_id, span_id)
);

CREATE INDEX IF NOT EXISTS idx_spans_trace_id ON spans(trace_id);
CREATE INDEX IF NOT EXISTS idx_spans_service_time ON spans(service_name, start_time);
CREATE INDEX IF NOT EXISTS idx_spans_start_time ON spans(start_time);
`

// New opens a SQLite database at dbPath, configures PRAGMAs, and creates the schema.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Apply PRAGMAs.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=OFF",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	// Create schema.
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

const insertSQL = `INSERT OR REPLACE INTO spans (
	trace_id, span_id, parent_span_id, service_name, span_name, span_kind,
	start_time, end_time, duration_ms, status_code, status_message,
	attributes, events
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// InsertSpans inserts spans into the database within a single transaction.
// Uses INSERT OR REPLACE for idempotent upsert on the (trace_id, span_id) primary key.
func (s *Store) InsertSpans(ctx context.Context, spans []Span) error {
	if len(spans) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, span := range spans {
		// Convert empty ParentSpanID to SQL NULL for root spans.
		var parentSpanID any
		if span.ParentSpanID != "" {
			parentSpanID = span.ParentSpanID
		}

		// Convert json.RawMessage to string (or NULL if nil/empty).
		var attrs, events any
		if len(span.Attributes) > 0 {
			attrs = string(span.Attributes)
		}
		if len(span.Events) > 0 {
			events = string(span.Events)
		}

		_, err := stmt.ExecContext(ctx,
			span.TraceID, span.SpanID, parentSpanID,
			span.ServiceName, span.SpanName, span.SpanKind,
			span.StartTime, span.EndTime, span.DurationMs,
			span.StatusCode, span.StatusMsg,
			attrs, events,
		)
		if err != nil {
			return fmt.Errorf("insert span %s/%s: %w", span.TraceID, span.SpanID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
