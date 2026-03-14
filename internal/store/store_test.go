package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
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

// --- ListServices tests ---

func TestListServices_DistinctWithCounts(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	spans := []Span{
		{TraceID: "t1", SpanID: "s1", ServiceName: "auth-svc", SpanName: "op", SpanKind: 1, StartTime: 1000, EndTime: 2000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s2", ServiceName: "auth-svc", SpanName: "op", SpanKind: 1, StartTime: 3000, EndTime: 4000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s3", ServiceName: "auth-svc", SpanName: "op", SpanKind: 1, StartTime: 5000, EndTime: 6000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t2", SpanID: "s4", ServiceName: "api-gateway", SpanName: "op", SpanKind: 1, StartTime: 2000, EndTime: 3000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t2", SpanID: "s5", ServiceName: "api-gateway", SpanName: "op", SpanKind: 1, StartTime: 7000, EndTime: 8000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t3", SpanID: "s6", ServiceName: "user-svc", SpanName: "op", SpanKind: 1, StartTime: 9000, EndTime: 10000, DurationMs: 1, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	services, err := s.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}

	if len(services) != 3 {
		t.Fatalf("len(services) = %d, want 3", len(services))
	}

	// Results ordered by service_name alphabetically.
	want := []struct {
		name      string
		spanCount int64
		lastSeen  int64
	}{
		{"api-gateway", 2, 7000},
		{"auth-svc", 3, 5000},
		{"user-svc", 1, 9000},
	}

	for i, w := range want {
		if services[i].Name != w.name {
			t.Errorf("services[%d].Name = %q, want %q", i, services[i].Name, w.name)
		}
		if services[i].SpanCount != w.spanCount {
			t.Errorf("services[%d].SpanCount = %d, want %d", i, services[i].SpanCount, w.spanCount)
		}
		if services[i].LastSeen != w.lastSeen {
			t.Errorf("services[%d].LastSeen = %d, want %d", i, services[i].LastSeen, w.lastSeen)
		}
	}
}

func TestListServices_EmptyDB(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	services, err := s.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if services == nil {
		t.Fatal("ListServices returned nil, want empty slice")
	}
	if len(services) != 0 {
		t.Errorf("len(services) = %d, want 0", len(services))
	}
}

// --- SearchTraces tests ---

func insertTestTrace(t *testing.T, s *Store, traceID, service, spanName string, startNanos int64, durationMs int64, hasError bool) {
	t.Helper()
	status := "OK"
	if hasError {
		status = "ERROR"
	}
	endNanos := startNanos + durationMs*1_000_000
	spans := []Span{
		{TraceID: traceID, SpanID: traceID + "-root", ServiceName: service, SpanName: spanName, SpanKind: 1, StartTime: startNanos, EndTime: endNanos, DurationMs: durationMs, StatusCode: status},
	}
	if err := s.InsertSpans(context.Background(), spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}
}

func TestSearchTraces_FilterByService(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	insertTestTrace(t, s, "t1", "svc-a", "op-a", 1_000_000_000, 10, false)
	insertTestTrace(t, s, "t2", "svc-b", "op-b", 2_000_000_000, 10, false)
	insertTestTrace(t, s, "t3", "svc-c", "op-c", 3_000_000_000, 10, false)

	results, err := s.SearchTraces(ctx, TraceQuery{ServiceName: "svc-a"})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].TraceID != "t1" {
		t.Errorf("TraceID = %q, want %q", results[0].TraceID, "t1")
	}
	if results[0].Service != "svc-a" {
		t.Errorf("Service = %q, want %q", results[0].Service, "svc-a")
	}
}

func TestSearchTraces_FilterBySpanNameSubstring(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	insertTestTrace(t, s, "t1", "svc", "handle-request", 1_000_000_000, 10, false)
	insertTestTrace(t, s, "t2", "svc", "db-query", 2_000_000_000, 10, false)
	insertTestTrace(t, s, "t3", "svc", "handle-response", 3_000_000_000, 10, false)

	results, err := s.SearchTraces(ctx, TraceQuery{SpanName: "handle"})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
}

func TestSearchTraces_FilterByDurationRange(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	insertTestTrace(t, s, "t1", "svc", "fast", 1_000_000_000, 10, false)
	insertTestTrace(t, s, "t2", "svc", "medium", 2_000_000_000, 50, false)
	insertTestTrace(t, s, "t3", "svc", "slow", 3_000_000_000, 200, false)

	results, err := s.SearchTraces(ctx, TraceQuery{MinDuration: 20, MaxDuration: 100})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].TraceID != "t2" {
		t.Errorf("TraceID = %q, want %q", results[0].TraceID, "t2")
	}
}

func TestSearchTraces_FilterByTimeRange(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	insertTestTrace(t, s, "t1", "svc", "op", base.UnixNano(), 10, false)
	insertTestTrace(t, s, "t2", "svc", "op", base.Add(1*time.Hour).UnixNano(), 10, false)
	insertTestTrace(t, s, "t3", "svc", "op", base.Add(2*time.Hour).UnixNano(), 10, false)

	results, err := s.SearchTraces(ctx, TraceQuery{
		Start: base.Add(30 * time.Minute),
		End:   base.Add(90 * time.Minute),
	})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].TraceID != "t2" {
		t.Errorf("TraceID = %q, want %q", results[0].TraceID, "t2")
	}
}

func TestSearchTraces_RespectsLimit(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		insertTestTrace(t, s, fmt.Sprintf("t%02d", i), "svc", "op", int64(i)*1_000_000_000, 10, false)
	}

	results, err := s.SearchTraces(ctx, TraceQuery{Limit: 10})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 10 {
		t.Errorf("len(results) = %d, want 10", len(results))
	}
}

func TestSearchTraces_OrderByStartTimeDescending(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	insertTestTrace(t, s, "t1", "svc", "op", 1_000_000_000, 10, false)
	insertTestTrace(t, s, "t2", "svc", "op", 2_000_000_000, 10, false)
	insertTestTrace(t, s, "t3", "svc", "op", 3_000_000_000, 10, false)

	results, err := s.SearchTraces(ctx, TraceQuery{})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}

	// Should be newest first: t3, t2, t1.
	if results[0].TraceID != "t3" {
		t.Errorf("results[0].TraceID = %q, want %q", results[0].TraceID, "t3")
	}
	if results[1].TraceID != "t2" {
		t.Errorf("results[1].TraceID = %q, want %q", results[1].TraceID, "t2")
	}
	if results[2].TraceID != "t1" {
		t.Errorf("results[2].TraceID = %q, want %q", results[2].TraceID, "t1")
	}
}

func TestSearchTraces_IdentifiesRootSpan(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	spans := []Span{
		{TraceID: "t1", SpanID: "root", ServiceName: "svc", SpanName: "root-op", SpanKind: 1, StartTime: 1_000_000_000, EndTime: 2_000_000_000, DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "child1", ParentSpanID: "root", ServiceName: "svc", SpanName: "child-op-1", SpanKind: 1, StartTime: 1_100_000_000, EndTime: 1_500_000_000, DurationMs: 400, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "child2", ParentSpanID: "root", ServiceName: "svc", SpanName: "child-op-2", SpanKind: 1, StartTime: 1_500_000_000, EndTime: 1_900_000_000, DurationMs: 400, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	results, err := s.SearchTraces(ctx, TraceQuery{})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].RootSpan != "root-op" {
		t.Errorf("RootSpan = %q, want %q", results[0].RootSpan, "root-op")
	}
	if results[0].SpanCount != 3 {
		t.Errorf("SpanCount = %d, want 3", results[0].SpanCount)
	}
}

func TestSearchTraces_PicksEarliestRoot(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	// Two root spans (both parent_span_id IS NULL) in the same trace.
	spans := []Span{
		{TraceID: "t1", SpanID: "root-late", ServiceName: "svc", SpanName: "late-root", SpanKind: 1, StartTime: 2_000_000_000, EndTime: 3_000_000_000, DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "root-early", ServiceName: "svc", SpanName: "early-root", SpanKind: 1, StartTime: 1_000_000_000, EndTime: 2_000_000_000, DurationMs: 1000, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	results, err := s.SearchTraces(ctx, TraceQuery{})
	if err != nil {
		t.Fatalf("SearchTraces: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].RootSpan != "early-root" {
		t.Errorf("RootSpan = %q, want %q (earliest root)", results[0].RootSpan, "early-root")
	}
}

// --- GetTrace tests ---

func TestGetTrace_TreeStructure(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	// root -> A, root -> B, A -> C, A -> D
	spans := []Span{
		{TraceID: "t1", SpanID: "root", ServiceName: "svc", SpanName: "root-op", SpanKind: 1, StartTime: 1000, EndTime: 5000, DurationMs: 4, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "A", ParentSpanID: "root", ServiceName: "svc", SpanName: "op-A", SpanKind: 1, StartTime: 1100, EndTime: 3000, DurationMs: 2, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "B", ParentSpanID: "root", ServiceName: "svc", SpanName: "op-B", SpanKind: 1, StartTime: 3100, EndTime: 4900, DurationMs: 2, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "C", ParentSpanID: "A", ServiceName: "svc", SpanName: "op-C", SpanKind: 1, StartTime: 1200, EndTime: 2000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "D", ParentSpanID: "A", ServiceName: "svc", SpanName: "op-D", SpanKind: 1, StartTime: 2100, EndTime: 2900, DurationMs: 1, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	detail, err := s.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if detail == nil {
		t.Fatal("GetTrace returned nil")
	}

	if len(detail.Roots) != 1 {
		t.Fatalf("len(Roots) = %d, want 1", len(detail.Roots))
	}

	root := detail.Roots[0]
	if root.SpanID != "root" {
		t.Errorf("root SpanID = %q, want %q", root.SpanID, "root")
	}
	if len(root.Children) != 2 {
		t.Fatalf("root has %d children, want 2", len(root.Children))
	}

	// Children of root: A, B.
	if root.Children[0].SpanID != "A" {
		t.Errorf("root.Children[0].SpanID = %q, want %q", root.Children[0].SpanID, "A")
	}
	if root.Children[1].SpanID != "B" {
		t.Errorf("root.Children[1].SpanID = %q, want %q", root.Children[1].SpanID, "B")
	}

	// Children of A: C, D.
	a := root.Children[0]
	if len(a.Children) != 2 {
		t.Fatalf("A has %d children, want 2", len(a.Children))
	}
	if a.Children[0].SpanID != "C" {
		t.Errorf("A.Children[0].SpanID = %q, want %q", a.Children[0].SpanID, "C")
	}
	if a.Children[1].SpanID != "D" {
		t.Errorf("A.Children[1].SpanID = %q, want %q", a.Children[1].SpanID, "D")
	}

	// B has no children.
	b := root.Children[1]
	if len(b.Children) != 0 {
		t.Errorf("B has %d children, want 0", len(b.Children))
	}
}

func TestGetTrace_SortsChildrenByStartTime(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	spans := []Span{
		{TraceID: "t1", SpanID: "root", ServiceName: "svc", SpanName: "root", SpanKind: 1, StartTime: 1000, EndTime: 5000, DurationMs: 4, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "c3", ParentSpanID: "root", ServiceName: "svc", SpanName: "third", SpanKind: 1, StartTime: 3000, EndTime: 4000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "c1", ParentSpanID: "root", ServiceName: "svc", SpanName: "first", SpanKind: 1, StartTime: 1100, EndTime: 2000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "c2", ParentSpanID: "root", ServiceName: "svc", SpanName: "second", SpanKind: 1, StartTime: 2100, EndTime: 3000, DurationMs: 1, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	detail, err := s.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}

	children := detail.Roots[0].Children
	if len(children) != 3 {
		t.Fatalf("len(children) = %d, want 3", len(children))
	}
	if children[0].SpanName != "first" {
		t.Errorf("children[0] = %q, want %q", children[0].SpanName, "first")
	}
	if children[1].SpanName != "second" {
		t.Errorf("children[1] = %q, want %q", children[1].SpanName, "second")
	}
	if children[2].SpanName != "third" {
		t.Errorf("children[2] = %q, want %q", children[2].SpanName, "third")
	}
}

func TestGetTrace_AggregateStats(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	// 5 spans, 2 services, 1 error.
	spans := []Span{
		{TraceID: "t1", SpanID: "root", ServiceName: "svc-a", SpanName: "root", SpanKind: 1, StartTime: 1_000_000_000, EndTime: 6_000_000_000, DurationMs: 5000, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s2", ParentSpanID: "root", ServiceName: "svc-a", SpanName: "op2", SpanKind: 1, StartTime: 1_100_000_000, EndTime: 2_000_000_000, DurationMs: 900, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s3", ParentSpanID: "root", ServiceName: "svc-b", SpanName: "op3", SpanKind: 1, StartTime: 2_100_000_000, EndTime: 3_000_000_000, DurationMs: 900, StatusCode: "ERROR"},
		{TraceID: "t1", SpanID: "s4", ParentSpanID: "s2", ServiceName: "svc-a", SpanName: "op4", SpanKind: 1, StartTime: 1_200_000_000, EndTime: 1_800_000_000, DurationMs: 600, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s5", ParentSpanID: "s3", ServiceName: "svc-b", SpanName: "op5", SpanKind: 1, StartTime: 2_200_000_000, EndTime: 2_800_000_000, DurationMs: 600, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	detail, err := s.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}

	if detail.SpanCount != 5 {
		t.Errorf("SpanCount = %d, want 5", detail.SpanCount)
	}
	if detail.ServiceCount != 2 {
		t.Errorf("ServiceCount = %d, want 2", detail.ServiceCount)
	}
	if detail.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", detail.ErrorCount)
	}
	// Duration: (6_000_000_000 - 1_000_000_000) / 1_000_000 = 5000ms
	if detail.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", detail.DurationMs)
	}
}

func TestGetTrace_PromotesOrphansToRoots(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	// A is root, B has parent A, C has parent X (which doesn't exist = orphan).
	spans := []Span{
		{TraceID: "t1", SpanID: "A", ServiceName: "svc", SpanName: "root-A", SpanKind: 1, StartTime: 1000, EndTime: 5000, DurationMs: 4, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "B", ParentSpanID: "A", ServiceName: "svc", SpanName: "child-B", SpanKind: 1, StartTime: 1100, EndTime: 3000, DurationMs: 2, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "C", ParentSpanID: "X", ServiceName: "svc", SpanName: "orphan-C", SpanKind: 1, StartTime: 6000, EndTime: 7000, DurationMs: 1, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	detail, err := s.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}

	// Should have 2 roots: A and C (orphan promoted).
	if len(detail.Roots) != 2 {
		t.Fatalf("len(Roots) = %d, want 2", len(detail.Roots))
	}

	// Sorted by startTime: A first, then C.
	if detail.Roots[0].SpanID != "A" {
		t.Errorf("Roots[0].SpanID = %q, want %q", detail.Roots[0].SpanID, "A")
	}
	if detail.Roots[1].SpanID != "C" {
		t.Errorf("Roots[1].SpanID = %q, want %q", detail.Roots[1].SpanID, "C")
	}

	// A should have B as child.
	if len(detail.Roots[0].Children) != 1 {
		t.Fatalf("A has %d children, want 1", len(detail.Roots[0].Children))
	}
	if detail.Roots[0].Children[0].SpanID != "B" {
		t.Errorf("A.Children[0].SpanID = %q, want %q", detail.Roots[0].Children[0].SpanID, "B")
	}

	// All 3 spans accounted for.
	if detail.SpanCount != 3 {
		t.Errorf("SpanCount = %d, want 3", detail.SpanCount)
	}
}

func TestGetTrace_UnknownTraceID(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	detail, err := s.GetTrace(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if detail != nil {
		t.Errorf("GetTrace returned %+v, want nil", detail)
	}
}

// --- GetSpan tests ---

func TestGetSpan_ReturnsSingleSpan(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	attrs := json.RawMessage(`{"key":"value"}`)
	events := json.RawMessage(`[{"name":"evt"}]`)
	input := Span{
		TraceID:      "t1",
		SpanID:       "s1",
		ParentSpanID: "parent",
		ServiceName:  "svc",
		SpanName:     "op",
		SpanKind:     2,
		StartTime:    1000,
		EndTime:      2000,
		DurationMs:   1,
		StatusCode:   "OK",
		StatusMsg:    "all good",
		Attributes:   attrs,
		Events:       events,
	}
	if err := s.InsertSpans(ctx, []Span{input}); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	got, err := s.GetSpan(ctx, "t1", "s1")
	if err != nil {
		t.Fatalf("GetSpan: %v", err)
	}
	if got == nil {
		t.Fatal("GetSpan returned nil")
	}

	if got.TraceID != "t1" {
		t.Errorf("TraceID = %q, want %q", got.TraceID, "t1")
	}
	if got.SpanID != "s1" {
		t.Errorf("SpanID = %q, want %q", got.SpanID, "s1")
	}
	if got.ParentSpanID != "parent" {
		t.Errorf("ParentSpanID = %q, want %q", got.ParentSpanID, "parent")
	}
	if got.ServiceName != "svc" {
		t.Errorf("ServiceName = %q, want %q", got.ServiceName, "svc")
	}
	if got.SpanName != "op" {
		t.Errorf("SpanName = %q, want %q", got.SpanName, "op")
	}
	if got.SpanKind != 2 {
		t.Errorf("SpanKind = %d, want 2", got.SpanKind)
	}
	if got.DurationMs != 1 {
		t.Errorf("DurationMs = %d, want 1", got.DurationMs)
	}
	if got.StatusCode != "OK" {
		t.Errorf("StatusCode = %q, want %q", got.StatusCode, "OK")
	}
	if got.StatusMsg != "all good" {
		t.Errorf("StatusMsg = %q, want %q", got.StatusMsg, "all good")
	}
	if string(got.Attributes) != string(attrs) {
		t.Errorf("Attributes = %s, want %s", got.Attributes, attrs)
	}
	if string(got.Events) != string(events) {
		t.Errorf("Events = %s, want %s", got.Events, events)
	}
}

func TestGetSpan_UnknownSpan(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetSpan(ctx, "nonexistent", "nope")
	if err != nil {
		t.Fatalf("GetSpan: %v", err)
	}
	if got != nil {
		t.Errorf("GetSpan returned %+v, want nil", got)
	}
}

// --- GetDependencies tests ---

func TestGetDependencies_CrossServiceCalls(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	spans := []Span{
		{TraceID: "t1", SpanID: "parent1", ServiceName: "api-gateway", SpanName: "handle", SpanKind: 2, StartTime: 1000, EndTime: 5000, DurationMs: 4, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "child1", ParentSpanID: "parent1", ServiceName: "auth-svc", SpanName: "verify", SpanKind: 1, StartTime: 1100, EndTime: 2000, DurationMs: 1, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	deps, err := s.GetDependencies(ctx, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d, want 1", len(deps))
	}
	if deps[0].Parent != "api-gateway" {
		t.Errorf("Parent = %q, want %q", deps[0].Parent, "api-gateway")
	}
	if deps[0].Child != "auth-svc" {
		t.Errorf("Child = %q, want %q", deps[0].Child, "auth-svc")
	}
	if deps[0].CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", deps[0].CallCount)
	}
}

func TestGetDependencies_ExcludesSameService(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	spans := []Span{
		{TraceID: "t1", SpanID: "parent1", ServiceName: "svc-a", SpanName: "handle", SpanKind: 2, StartTime: 1000, EndTime: 5000, DurationMs: 4, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "child1", ParentSpanID: "parent1", ServiceName: "svc-a", SpanName: "internal", SpanKind: 1, StartTime: 1100, EndTime: 2000, DurationMs: 1, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	deps, err := s.GetDependencies(ctx, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("len(deps) = %d, want 0 (same-service calls excluded)", len(deps))
	}
}

func TestGetDependencies_RespectsTimeRange(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	old := now.Add(-2 * time.Hour)
	recent := now.Add(-30 * time.Minute)

	spans := []Span{
		// Old cross-service call.
		{TraceID: "t1", SpanID: "p-old", ServiceName: "svc-a", SpanName: "handle", SpanKind: 2, StartTime: old.UnixNano(), EndTime: old.Add(time.Second).UnixNano(), DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "c-old", ParentSpanID: "p-old", ServiceName: "svc-b", SpanName: "call", SpanKind: 1, StartTime: old.Add(100 * time.Millisecond).UnixNano(), EndTime: old.Add(500 * time.Millisecond).UnixNano(), DurationMs: 400, StatusCode: "OK"},
		// Recent cross-service call.
		{TraceID: "t2", SpanID: "p-new", ServiceName: "svc-a", SpanName: "handle", SpanKind: 2, StartTime: recent.UnixNano(), EndTime: recent.Add(time.Second).UnixNano(), DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t2", SpanID: "c-new", ParentSpanID: "p-new", ServiceName: "svc-b", SpanName: "call", SpanKind: 1, StartTime: recent.Add(100 * time.Millisecond).UnixNano(), EndTime: recent.Add(500 * time.Millisecond).UnixNano(), DurationMs: 400, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// Query with since = 1 hour ago, should only get the recent call.
	deps, err := s.GetDependencies(ctx, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d, want 1", len(deps))
	}
	if deps[0].CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", deps[0].CallCount)
	}
}

// --- PurgeOlderThan tests ---

func TestPurgeOlderThan_DeletesOldSpans(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	old := now.Add(-1 * time.Hour)

	spans := []Span{
		{TraceID: "t1", SpanID: "old1", ServiceName: "svc", SpanName: "op", SpanKind: 1, StartTime: old.UnixNano(), EndTime: old.Add(time.Second).UnixNano(), DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "old2", ServiceName: "svc", SpanName: "op", SpanKind: 1, StartTime: old.Add(time.Second).UnixNano(), EndTime: old.Add(2 * time.Second).UnixNano(), DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t2", SpanID: "new1", ServiceName: "svc", SpanName: "op", SpanKind: 1, StartTime: now.UnixNano(), EndTime: now.Add(time.Second).UnixNano(), DurationMs: 1000, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	// Purge older than 30 minutes.
	deleted, err := s.PurgeOlderThan(ctx, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	// Verify new span remains.
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans").Scan(&count); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if count != 1 {
		t.Errorf("remaining spans = %d, want 1", count)
	}
}

func TestPurgeOlderThan_ReturnsCount(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	old := time.Now().Add(-2 * time.Hour)
	var spans []Span
	for i := 0; i < 10; i++ {
		spans = append(spans, Span{
			TraceID:    fmt.Sprintf("t%d", i),
			SpanID:     fmt.Sprintf("s%d", i),
			ServiceName: "svc",
			SpanName:   "op",
			SpanKind:   1,
			StartTime:  old.Add(time.Duration(i) * time.Millisecond).UnixNano(),
			EndTime:    old.Add(time.Duration(i)*time.Millisecond + time.Second).UnixNano(),
			DurationMs: 1000,
			StatusCode: "OK",
		})
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	deleted, err := s.PurgeOlderThan(ctx, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if deleted != 10 {
		t.Errorf("deleted = %d, want 10", deleted)
	}
}

// --- Stats tests ---

func TestStats_ReturnsCorrectCounts(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	spans := []Span{
		{TraceID: "t1", SpanID: "s1", ServiceName: "svc", SpanName: "op", SpanKind: 1, StartTime: 1000, EndTime: 2000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "s2", ServiceName: "svc", SpanName: "op", SpanKind: 1, StartTime: 3000, EndTime: 4000, DurationMs: 1, StatusCode: "OK"},
		{TraceID: "t2", SpanID: "s3", ServiceName: "svc", SpanName: "op", SpanKind: 1, StartTime: 5000, EndTime: 6000, DurationMs: 1, StatusCode: "OK"},
	}
	if err := s.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if stats.SpanCount != 3 {
		t.Errorf("SpanCount = %d, want 3", stats.SpanCount)
	}
	if stats.TraceCount != 2 {
		t.Errorf("TraceCount = %d, want 2", stats.TraceCount)
	}
	if stats.OldestSpan != 1000 {
		t.Errorf("OldestSpan = %d, want 1000", stats.OldestSpan)
	}
	if stats.NewestSpan != 5000 {
		t.Errorf("NewestSpan = %d, want 5000", stats.NewestSpan)
	}
	if stats.DBSizeBytes <= 0 {
		t.Errorf("DBSizeBytes = %d, want > 0", stats.DBSizeBytes)
	}
}
