package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertSpans_SingleSpan(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	attrs := json.RawMessage(`{"http.method":"GET","http.status_code":200}`)
	events := json.RawMessage(`[{"name":"started","time":1000}]`)

	span := Span{
		TraceID:      "aaaa",
		SpanID:       "bbbb",
		ParentSpanID: "",
		ServiceName:  "api-gateway",
		SpanName:     "handle-request",
		SpanKind:     2,
		StartTime:    1000000000,
		EndTime:      1050000000,
		DurationMs:   50,
		StatusCode:   "OK",
		StatusMsg:    "",
		Attributes:   attrs,
		Events:       events,
	}

	if err := s.InsertSpans(ctx, []Span{span}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// Verify by querying directly since GetTrace isn't implemented yet.
	row := s.db.QueryRowContext(ctx, `SELECT
		trace_id, span_id, parent_span_id, service_name, span_name, span_kind,
		start_time, end_time, duration_ms, status_code, status_message,
		attributes, events
		FROM spans WHERE trace_id = ? AND span_id = ?`, "aaaa", "bbbb")

	var got Span
	var parentSpanID sql.NullString
	var statusMsg sql.NullString
	var attrsStr, eventsStr sql.NullString

	err := row.Scan(
		&got.TraceID, &got.SpanID, &parentSpanID,
		&got.ServiceName, &got.SpanName, &got.SpanKind,
		&got.StartTime, &got.EndTime, &got.DurationMs,
		&got.StatusCode, &statusMsg,
		&attrsStr, &eventsStr,
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if parentSpanID.Valid {
		got.ParentSpanID = parentSpanID.String
	}
	if statusMsg.Valid {
		got.StatusMsg = statusMsg.String
	}
	if attrsStr.Valid {
		got.Attributes = json.RawMessage(attrsStr.String)
	}
	if eventsStr.Valid {
		got.Events = json.RawMessage(eventsStr.String)
	}

	// Verify all fields.
	if got.TraceID != span.TraceID {
		t.Errorf("TraceID = %q, want %q", got.TraceID, span.TraceID)
	}
	if got.SpanID != span.SpanID {
		t.Errorf("SpanID = %q, want %q", got.SpanID, span.SpanID)
	}
	if got.ParentSpanID != "" {
		t.Errorf("ParentSpanID = %q, want empty (root span stored as NULL)", got.ParentSpanID)
	}
	if got.ServiceName != span.ServiceName {
		t.Errorf("ServiceName = %q, want %q", got.ServiceName, span.ServiceName)
	}
	if got.SpanName != span.SpanName {
		t.Errorf("SpanName = %q, want %q", got.SpanName, span.SpanName)
	}
	if got.SpanKind != span.SpanKind {
		t.Errorf("SpanKind = %d, want %d", got.SpanKind, span.SpanKind)
	}
	if got.StartTime != span.StartTime {
		t.Errorf("StartTime = %d, want %d", got.StartTime, span.StartTime)
	}
	if got.EndTime != span.EndTime {
		t.Errorf("EndTime = %d, want %d", got.EndTime, span.EndTime)
	}
	if got.DurationMs != span.DurationMs {
		t.Errorf("DurationMs = %d, want %d", got.DurationMs, span.DurationMs)
	}
	if got.StatusCode != span.StatusCode {
		t.Errorf("StatusCode = %q, want %q", got.StatusCode, span.StatusCode)
	}
	if string(got.Attributes) != string(span.Attributes) {
		t.Errorf("Attributes = %s, want %s", got.Attributes, span.Attributes)
	}
	if string(got.Events) != string(span.Events) {
		t.Errorf("Events = %s, want %s", got.Events, span.Events)
	}
}

func TestInsertSpans_Batch100(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	// 100 spans across 10 traces (10 spans each).
	var spans []Span
	for trace := 0; trace < 10; trace++ {
		traceID := fmt.Sprintf("trace-%02d", trace)
		for span := 0; span < 10; span++ {
			spans = append(spans, Span{
				TraceID:     traceID,
				SpanID:      fmt.Sprintf("span-%02d-%02d", trace, span),
				ServiceName: "test-svc",
				SpanName:    "op",
				SpanKind:    1,
				StartTime:   int64(trace*1000 + span),
				EndTime:     int64(trace*1000 + span + 100),
				DurationMs:  100,
				StatusCode:  "OK",
			})
		}
	}

	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// Verify total count.
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans").Scan(&total); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if total != 100 {
		t.Errorf("total spans = %d, want 100", total)
	}

	// Verify per-trace count.
	for trace := 0; trace < 10; trace++ {
		traceID := fmt.Sprintf("trace-%02d", trace)
		var count int
		err := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM spans WHERE trace_id = ?", traceID).Scan(&count)
		if err != nil {
			t.Fatalf("COUNT for %s: %v", traceID, err)
		}
		if count != 10 {
			t.Errorf("trace %s span count = %d, want 10", traceID, count)
		}
	}
}

func TestInsertSpans_IdempotentUpsert(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	original := Span{
		TraceID:     "trace-1",
		SpanID:      "span-1",
		ServiceName: "svc-a",
		SpanName:    "original-op",
		SpanKind:    1,
		StartTime:   1000,
		EndTime:     2000,
		DurationMs:  1,
		StatusCode:  "UNSET",
		Attributes:  json.RawMessage(`{"version":"v1"}`),
	}

	if err := s.InsertSpans(ctx, []Span{original}); err != nil {
		t.Fatalf("first InsertSpans: %v", err)
	}

	// Insert again with same PK but different fields.
	updated := Span{
		TraceID:     "trace-1",
		SpanID:      "span-1",
		ServiceName: "svc-a",
		SpanName:    "updated-op",
		SpanKind:    2,
		StartTime:   1000,
		EndTime:     3000,
		DurationMs:  2,
		StatusCode:  "OK",
		Attributes:  json.RawMessage(`{"version":"v2"}`),
	}

	if err := s.InsertSpans(ctx, []Span{updated}); err != nil {
		t.Fatalf("second InsertSpans: %v", err)
	}

	// Should still be exactly one row.
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans WHERE trace_id = 'trace-1'").Scan(&count); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", count)
	}

	// Verify updated values took effect.
	var spanName, statusCode string
	var durationMs int64
	var attrsStr sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT span_name, status_code, duration_ms, attributes FROM spans WHERE trace_id = 'trace-1' AND span_id = 'span-1'",
	).Scan(&spanName, &statusCode, &durationMs, &attrsStr)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if spanName != "updated-op" {
		t.Errorf("span_name = %q, want %q", spanName, "updated-op")
	}
	if statusCode != "OK" {
		t.Errorf("status_code = %q, want %q", statusCode, "OK")
	}
	if durationMs != 2 {
		t.Errorf("duration_ms = %d, want 2", durationMs)
	}
	if !attrsStr.Valid || attrsStr.String != `{"version":"v2"}` {
		t.Errorf("attributes = %v, want %q", attrsStr, `{"version":"v2"}`)
	}
}

func TestInsertSpans_EmptySlice(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	// Should be a no-op, not an error.
	if err := s.InsertSpans(ctx, nil); err != nil {
		t.Fatalf("InsertSpans(nil): %v", err)
	}
	if err := s.InsertSpans(ctx, []Span{}); err != nil {
		t.Fatalf("InsertSpans([]): %v", err)
	}
}

func TestInsertSpans_ParentSpanIDNullForRootSpan(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	root := Span{
		TraceID:     "trace-root",
		SpanID:      "span-root",
		ServiceName: "svc",
		SpanName:    "root-op",
		SpanKind:    1,
		StartTime:   1000,
		EndTime:     2000,
		DurationMs:  1,
		StatusCode:  "OK",
	}
	child := Span{
		TraceID:      "trace-root",
		SpanID:       "span-child",
		ParentSpanID: "span-root",
		ServiceName:  "svc",
		SpanName:     "child-op",
		SpanKind:     1,
		StartTime:    1100,
		EndTime:      1900,
		DurationMs:   1,
		StatusCode:   "OK",
	}

	if err := s.InsertSpans(ctx, []Span{root, child}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// Root span should have NULL parent_span_id.
	var parentID sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT parent_span_id FROM spans WHERE span_id = 'span-root'").Scan(&parentID)
	if err != nil {
		t.Fatalf("Scan root: %v", err)
	}
	if parentID.Valid {
		t.Errorf("root span parent_span_id = %q, want NULL", parentID.String)
	}

	// Child span should have non-NULL parent_span_id.
	err = s.db.QueryRowContext(ctx,
		"SELECT parent_span_id FROM spans WHERE span_id = 'span-child'").Scan(&parentID)
	if err != nil {
		t.Fatalf("Scan child: %v", err)
	}
	if !parentID.Valid || parentID.String != "span-root" {
		t.Errorf("child span parent_span_id = %v, want %q", parentID, "span-root")
	}
}

func TestNew_SetsWALMode(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	var journalMode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want %q", journalMode, "wal")
	}
}

func TestNew_CreatesSchema(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	// Verify the spans table exists by querying it.
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM spans").Scan(&count); err != nil {
		t.Fatalf("spans table does not exist: %v", err)
	}

	// Verify indexes exist.
	rows, err := s.db.Query("SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='spans' ORDER BY name")
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer rows.Close()

	var indexes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		indexes = append(indexes, name)
	}

	expected := []string{
		"idx_spans_service_time",
		"idx_spans_start_time",
		"idx_spans_trace_id",
	}
	if len(indexes) < len(expected) {
		t.Errorf("indexes = %v, want at least %v", indexes, expected)
	}
	for _, want := range expected {
		found := false
		for _, got := range indexes {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing index %q in %v", want, indexes)
		}
	}
}
