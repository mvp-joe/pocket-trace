package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

// ListServices returns all distinct services with span counts and last-seen times.
func (s *Store) ListServices(ctx context.Context) ([]ServiceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT service_name, COUNT(*) AS span_count, MAX(start_time) AS last_seen
		FROM spans
		GROUP BY service_name
		ORDER BY service_name`)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()

	services := make([]ServiceSummary, 0)
	for rows.Next() {
		var svc ServiceSummary
		if err := rows.Scan(&svc.Name, &svc.SpanCount, &svc.LastSeen); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		services = append(services, svc)
	}
	return services, rows.Err()
}

// SearchTraces searches for traces matching the given query filters.
// Results are grouped by trace_id and ordered by start_time descending.
func (s *Store) SearchTraces(ctx context.Context, q TraceQuery) ([]TraceSummary, error) {
	// Clamp limit.
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Build dynamic WHERE clause for span-level filtering.
	var conditions []string
	var args []any

	if q.ServiceName != "" {
		conditions = append(conditions, "service_name = ?")
		args = append(args, q.ServiceName)
	}
	if q.SpanName != "" {
		conditions = append(conditions, "span_name LIKE ?")
		args = append(args, "%"+q.SpanName+"%")
	}
	if q.MinDuration > 0 {
		conditions = append(conditions, "duration_ms >= ?")
		args = append(args, q.MinDuration)
	}
	if q.MaxDuration > 0 {
		conditions = append(conditions, "duration_ms <= ?")
		args = append(args, q.MaxDuration)
	}
	if !q.Start.IsZero() {
		conditions = append(conditions, "start_time >= ?")
		args = append(args, q.Start.UnixNano())
	}
	if !q.End.IsZero() {
		conditions = append(conditions, "start_time <= ?")
		args = append(args, q.End.UnixNano())
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Find trace_ids that have at least one span matching the filters,
	// then aggregate per trace. The root span is the earliest span with
	// parent_span_id IS NULL within each trace.
	query := fmt.Sprintf(`
		WITH matched_traces AS (
			SELECT DISTINCT trace_id FROM spans %s
		)
		SELECT
			s.trace_id,
			COUNT(*) AS span_count,
			SUM(CASE WHEN s.status_code = 'ERROR' THEN 1 ELSE 0 END) AS error_count,
			MIN(s.start_time) AS start_time,
			MAX(s.end_time) - MIN(s.start_time) AS duration_ns
		FROM spans s
		JOIN matched_traces mt ON s.trace_id = mt.trace_id
		GROUP BY s.trace_id
		ORDER BY start_time DESC
		LIMIT ?`, where)

	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search traces: %w", err)
	}
	defer rows.Close()

	type traceAgg struct {
		traceID    string
		spanCount  int
		errorCount int
		startTime  int64
		durationNs int64
	}

	var traces []traceAgg
	var traceIDs []string
	for rows.Next() {
		var t traceAgg
		if err := rows.Scan(&t.traceID, &t.spanCount, &t.errorCount, &t.startTime, &t.durationNs); err != nil {
			return nil, fmt.Errorf("scan trace: %w", err)
		}
		traces = append(traces, t)
		traceIDs = append(traceIDs, t.traceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate traces: %w", err)
	}

	if len(traces) == 0 {
		return []TraceSummary{}, nil
	}

	// Fetch root span info (earliest span with parent_span_id IS NULL) per trace.
	placeholders := strings.Repeat("?,", len(traceIDs))
	placeholders = placeholders[:len(placeholders)-1]

	rootQuery := fmt.Sprintf(`
		SELECT trace_id, span_name, service_name
		FROM spans
		WHERE trace_id IN (%s) AND parent_span_id IS NULL
		ORDER BY trace_id, start_time`, placeholders)

	rootArgs := make([]any, len(traceIDs))
	for i, id := range traceIDs {
		rootArgs[i] = id
	}

	rootRows, err := s.db.QueryContext(ctx, rootQuery, rootArgs...)
	if err != nil {
		return nil, fmt.Errorf("query root spans: %w", err)
	}
	defer rootRows.Close()

	// Take the first (earliest) root span per trace.
	type rootInfo struct {
		spanName    string
		serviceName string
	}
	rootMap := make(map[string]rootInfo, len(traceIDs))
	for rootRows.Next() {
		var traceID, spanName, serviceName string
		if err := rootRows.Scan(&traceID, &spanName, &serviceName); err != nil {
			return nil, fmt.Errorf("scan root span: %w", err)
		}
		if _, exists := rootMap[traceID]; !exists {
			rootMap[traceID] = rootInfo{spanName: spanName, serviceName: serviceName}
		}
	}
	if err := rootRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate root spans: %w", err)
	}

	results := make([]TraceSummary, 0, len(traces))
	for _, t := range traces {
		root := rootMap[t.traceID]
		results = append(results, TraceSummary{
			TraceID:    t.traceID,
			RootSpan:   root.spanName,
			Service:    root.serviceName,
			StartTime:  t.startTime,
			DurationMs: t.durationNs / 1_000_000,
			SpanCount:  t.spanCount,
			ErrorCount: t.errorCount,
		})
	}
	return results, nil
}

// scanSpan reads a span from a row scanner (Row or Rows).
func scanSpan(scanner interface {
	Scan(dest ...any) error
}) (Span, error) {
	var span Span
	var parentSpanID sql.NullString
	var statusMsg sql.NullString
	var attrsStr, eventsStr sql.NullString

	err := scanner.Scan(
		&span.TraceID, &span.SpanID, &parentSpanID,
		&span.ServiceName, &span.SpanName, &span.SpanKind,
		&span.StartTime, &span.EndTime, &span.DurationMs,
		&span.StatusCode, &statusMsg,
		&attrsStr, &eventsStr,
	)
	if err != nil {
		return Span{}, err
	}

	if parentSpanID.Valid {
		span.ParentSpanID = parentSpanID.String
	}
	if statusMsg.Valid {
		span.StatusMsg = statusMsg.String
	}
	if attrsStr.Valid {
		span.Attributes = json.RawMessage(attrsStr.String)
	}
	if eventsStr.Valid {
		span.Events = json.RawMessage(eventsStr.String)
	}
	return span, nil
}

const spanColumns = `trace_id, span_id, parent_span_id, service_name, span_name, span_kind,
	start_time, end_time, duration_ms, status_code, status_message, attributes, events`

// GetTrace retrieves all spans for a trace and builds a tree structure.
// Returns nil if no spans exist for the given trace ID.
func (s *Store) GetTrace(ctx context.Context, traceID string) (*TraceDetail, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+spanColumns+" FROM spans WHERE trace_id = ? ORDER BY start_time", traceID)
	if err != nil {
		return nil, fmt.Errorf("get trace %s: %w", traceID, err)
	}
	defer rows.Close()

	var spans []Span
	for rows.Next() {
		span, err := scanSpan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		spans = append(spans, span)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate spans: %w", err)
	}

	if len(spans) == 0 {
		return nil, nil
	}

	return buildTraceDetail(traceID, spans), nil
}

// buildTraceDetail constructs a TraceDetail tree from a flat slice of spans.
func buildTraceDetail(traceID string, spans []Span) *TraceDetail {
	// Index spans by spanID using pointer-based build nodes.
	nodes := make(map[string]*spanBuildNode, len(spans))
	for _, sp := range spans {
		nodes[sp.SpanID] = &spanBuildNode{span: sp}
	}

	// Build parent-child relationships. Roots are spans with no parent;
	// orphans (parent not in result set) are promoted to roots.
	var rootIDs []string
	for _, sp := range spans {
		if sp.ParentSpanID == "" {
			rootIDs = append(rootIDs, sp.SpanID)
		} else if parent, ok := nodes[sp.ParentSpanID]; ok {
			parent.children = append(parent.children, nodes[sp.SpanID])
		} else {
			rootIDs = append(rootIDs, sp.SpanID)
		}
	}

	// Sort roots by startTime.
	sort.Slice(rootIDs, func(i, j int) bool {
		return nodes[rootIDs[i]].span.StartTime < nodes[rootIDs[j]].span.StartTime
	})

	// Convert to SpanNode trees, sorting children at each level.
	roots := make([]SpanNode, 0, len(rootIDs))
	for _, id := range rootIDs {
		roots = append(roots, nodes[id].toSpanNode())
	}

	// Compute aggregate stats from ALL spans.
	services := make(map[string]struct{}, len(spans))
	var errorCount int
	var minStart, maxEnd int64
	for i, sp := range spans {
		services[sp.ServiceName] = struct{}{}
		if sp.StatusCode == "ERROR" {
			errorCount++
		}
		if i == 0 || sp.StartTime < minStart {
			minStart = sp.StartTime
		}
		if i == 0 || sp.EndTime > maxEnd {
			maxEnd = sp.EndTime
		}
	}

	return &TraceDetail{
		TraceID:      traceID,
		Roots:        roots,
		SpanCount:    len(spans),
		ServiceCount: len(services),
		DurationMs:   (maxEnd - minStart) / 1_000_000,
		ErrorCount:   errorCount,
	}
}

// spanBuildNode is an intermediate node used during tree construction.
type spanBuildNode struct {
	span     Span
	children []*spanBuildNode
}

// toSpanNode recursively converts a build node into a SpanNode value,
// sorting children by startTime at each level.
func (n *spanBuildNode) toSpanNode() SpanNode {
	sort.Slice(n.children, func(i, j int) bool {
		return n.children[i].span.StartTime < n.children[j].span.StartTime
	})

	children := make([]SpanNode, 0, len(n.children))
	for _, c := range n.children {
		children = append(children, c.toSpanNode())
	}

	return SpanNode{
		TraceID:      n.span.TraceID,
		SpanID:       n.span.SpanID,
		ParentSpanID: n.span.ParentSpanID,
		ServiceName:  n.span.ServiceName,
		SpanName:     n.span.SpanName,
		SpanKind:     n.span.SpanKind,
		StartTime:    n.span.StartTime,
		EndTime:      n.span.EndTime,
		DurationMs:   n.span.DurationMs,
		StatusCode:   n.span.StatusCode,
		StatusMsg:    n.span.StatusMsg,
		Attributes:   n.span.Attributes,
		Events:       n.span.Events,
		Children:     children,
	}
}

// GetSpan retrieves a single span by trace ID and span ID.
// Returns nil if the span does not exist.
func (s *Store) GetSpan(ctx context.Context, traceID, spanID string) (*Span, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+spanColumns+" FROM spans WHERE trace_id = ? AND span_id = ?",
		traceID, spanID)

	span, err := scanSpan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get span %s/%s: %w", traceID, spanID, err)
	}
	return &span, nil
}

// GetDependencies returns service-to-service call relationships since the given time.
func (s *Store) GetDependencies(ctx context.Context, since time.Time) ([]Dependency, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.service_name AS parent, c.service_name AS child, COUNT(*) AS call_count
		FROM spans c
		JOIN spans p ON c.parent_span_id = p.span_id AND c.trace_id = p.trace_id
		WHERE c.service_name != p.service_name
			AND c.start_time >= ?
		GROUP BY p.service_name, c.service_name
		ORDER BY call_count DESC`, since.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("get dependencies: %w", err)
	}
	defer rows.Close()

	deps := make([]Dependency, 0)
	for rows.Next() {
		var d Dependency
		if err := rows.Scan(&d.Parent, &d.Child, &d.CallCount); err != nil {
			return nil, fmt.Errorf("scan dependency: %w", err)
		}
		deps = append(deps, d)
	}
	return deps, rows.Err()
}

// Stats returns database health statistics.
func (s *Store) Stats(ctx context.Context) (*DBStats, error) {
	var stats DBStats
	var oldest, newest sql.NullInt64

	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT trace_id),
			MIN(start_time), MAX(start_time)
		FROM spans`).Scan(&stats.SpanCount, &stats.TraceCount, &oldest, &newest)
	if err != nil {
		return nil, fmt.Errorf("query stats: %w", err)
	}

	if oldest.Valid {
		stats.OldestSpan = oldest.Int64
	}
	if newest.Valid {
		stats.NewestSpan = newest.Int64
	}

	// DB size via PRAGMAs.
	var pageCount, pageSize int64
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return nil, fmt.Errorf("pragma page_count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return nil, fmt.Errorf("pragma page_size: %w", err)
	}
	stats.DBSizeBytes = pageCount * pageSize

	return &stats, nil
}

// PurgeOlderThan deletes spans with start_time before the given time.
// Returns the number of deleted spans.
func (s *Store) PurgeOlderThan(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM spans WHERE start_time < ?", before.UnixNano())
	if err != nil {
		return 0, fmt.Errorf("purge spans: %w", err)
	}
	return result.RowsAffected()
}
