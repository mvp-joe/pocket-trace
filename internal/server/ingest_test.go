package server

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"pocket-trace/internal/store"

	_ "modernc.org/sqlite"
)

// testDB bundles a store and a direct DB handle for verification queries.
type testDB struct {
	store *store.Store
	db    *sql.DB
	path  string
}

func newTestDB(t *testing.T) *testDB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New(%q): %v", dbPath, err)
	}

	// Open a second connection for verification queries.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		s.Close()
		t.Fatalf("sql.Open(%q): %v", dbPath, err)
	}

	t.Cleanup(func() {
		db.Close()
		s.Close()
	})

	return &testDB{store: s, db: db, path: dbPath}
}

func (td *testDB) countSpans(t *testing.T, traceID string) int {
	t.Helper()
	var count int
	err := td.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM spans WHERE trace_id = ?", traceID).Scan(&count)
	if err != nil {
		t.Fatalf("count spans for %q: %v", traceID, err)
	}
	return count
}

func spanID(i int) string {
	return "span-" + string(rune('A'+i/26)) + string(rune('a'+i%26))
}

func TestSpanBuffer_AddAppearsInStoreAfterFlush(t *testing.T) {
	t.Parallel()
	td := newTestDB(t)
	buf := NewSpanBuffer(td.store, 100, 64, 50*time.Millisecond)
	defer buf.Shutdown()

	buf.Add([]store.Span{
		{
			TraceID:     "trace-flush",
			SpanID:      "span-1",
			ServiceName: "svc",
			SpanName:    "op",
			SpanKind:    1,
			StartTime:   1000,
			EndTime:     2000,
			DurationMs:  1,
			StatusCode:  "OK",
		},
	})

	// Wait for flush interval to trigger.
	time.Sleep(200 * time.Millisecond)

	count := td.countSpans(t, "trace-flush")
	if count != 1 {
		t.Errorf("expected 1 span, got %d", count)
	}
}

func TestSpanBuffer_FlushTriggersAtBatchSize(t *testing.T) {
	t.Parallel()
	td := newTestDB(t)
	// Large flush interval so only batch size triggers flush.
	buf := NewSpanBuffer(td.store, 5, 64, 10*time.Second)
	defer buf.Shutdown()

	// Add 5 spans (one per Add call, accumulates to batchSize).
	for i := range 5 {
		buf.Add([]store.Span{
			{
				TraceID:     "trace-batch",
				SpanID:      spanID(i),
				ServiceName: "svc",
				SpanName:    "op",
				SpanKind:    1,
				StartTime:   int64(i * 1000),
				EndTime:     int64(i*1000 + 500),
				DurationMs:  1,
				StatusCode:  "OK",
			},
		})
	}

	// Give the goroutine time to process and flush.
	time.Sleep(100 * time.Millisecond)

	count := td.countSpans(t, "trace-batch")
	if count != 5 {
		t.Errorf("expected 5 spans, got %d", count)
	}
}

func TestSpanBuffer_ShutdownDrainsRemaining(t *testing.T) {
	t.Parallel()
	td := newTestDB(t)
	// Large flush interval and batch size so nothing flushes automatically.
	buf := NewSpanBuffer(td.store, 1000, 64, 10*time.Second)

	for i := range 3 {
		buf.Add([]store.Span{
			{
				TraceID:     "trace-shutdown",
				SpanID:      spanID(i),
				ServiceName: "svc",
				SpanName:    "op",
				SpanKind:    1,
				StartTime:   int64(i * 1000),
				EndTime:     int64(i*1000 + 500),
				DurationMs:  1,
				StatusCode:  "OK",
			},
		})
	}

	// Give channel sends a moment to be received.
	time.Sleep(50 * time.Millisecond)

	// Shutdown should drain and flush.
	buf.Shutdown()

	count := td.countSpans(t, "trace-shutdown")
	if count != 3 {
		t.Errorf("expected 3 spans after shutdown, got %d", count)
	}
}

func TestSpanBuffer_AddDoesNotPanicWhenFull(t *testing.T) {
	t.Parallel()
	td := newTestDB(t)
	// Small channel capacity (batchSize=2).
	buf := NewSpanBuffer(td.store, 2, 4, 10*time.Second)
	defer buf.Shutdown()

	// Flood with spans. Some will be dropped, but no panic should occur.
	for i := range 100 {
		buf.Add([]store.Span{
			{
				TraceID:     "trace-drop",
				SpanID:      spanID(i),
				ServiceName: "svc",
				SpanName:    "op",
				SpanKind:    1,
				StartTime:   int64(i * 1000),
				EndTime:     int64(i*1000 + 500),
				DurationMs:  1,
				StatusCode:  "OK",
			},
		})
	}
	// If we reach here without panic, the test passes.
}
