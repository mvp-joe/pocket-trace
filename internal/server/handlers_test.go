package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"pocket-trace/internal/store"

	"github.com/gofiber/fiber/v3"
	_ "modernc.org/sqlite"
)

// setupTestServer creates a test server with a real SQLite store and returns
// the Fiber app and a direct DB connection for verification.
func setupTestServer(t *testing.T) (*fiber.App, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	// Short flush interval for test responsiveness.
	buf := NewSpanBuffer(s, 100, 50*time.Millisecond)

	h := &Handlers{
		Store:     s,
		Buffer:    buf,
		StartTime: time.Now(),
		Version:   "test",
	}

	srv := New(s, buf, h, nil)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	t.Cleanup(func() {
		buf.Shutdown()
		db.Close()
		s.Close()
	})

	return srv.App(), db
}

func TestIngest_Success(t *testing.T) {
	t.Parallel()
	app, db := setupTestServer(t)

	payload := IngestRequest{
		ServiceName: "test-svc",
		Spans: []IngestSpan{
			{
				TraceID:    "aaaa",
				SpanID:     "bbbb",
				Name:       "handle-request",
				SpanKind:   2,
				StartTime:  1000000000000, // 1s in nanos
				EndTime:    1050000000000, // 1.05s in nanos
				StatusCode: "OK",
				Attributes: map[string]any{"http.method": "GET"},
				Events: []IngestEvent{
					{Name: "started", Time: 1000000000000, Attributes: map[string]any{"key": "val"}},
				},
			},
			{
				TraceID:      "aaaa",
				SpanID:       "cccc",
				ParentSpanID: "bbbb",
				Name:         "db-query",
				SpanKind:     3,
				StartTime:    1010000000000,
				EndTime:      1040000000000,
				StatusCode:   "OK",
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/api/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 202 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, respBody)
	}

	// Parse response body.
	var result APIResponse[IngestResult]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Data.Accepted != 2 {
		t.Errorf("accepted = %d, want 2", result.Data.Accepted)
	}

	// Wait for the buffer to flush.
	time.Sleep(200 * time.Millisecond)

	// Verify spans were written to the database.
	var count int
	err = db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM spans WHERE trace_id = ?", "aaaa").Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("span count = %d, want 2", count)
	}

	// Verify root span has NULL parent_span_id.
	var parentID sql.NullString
	err = db.QueryRowContext(context.Background(),
		"SELECT parent_span_id FROM spans WHERE span_id = ?", "bbbb").Scan(&parentID)
	if err != nil {
		t.Fatalf("scan parent: %v", err)
	}
	if parentID.Valid {
		t.Errorf("root span parent_span_id = %q, want NULL", parentID.String)
	}

	// Verify child span has correct parent.
	err = db.QueryRowContext(context.Background(),
		"SELECT parent_span_id FROM spans WHERE span_id = ?", "cccc").Scan(&parentID)
	if err != nil {
		t.Fatalf("scan child parent: %v", err)
	}
	if !parentID.Valid || parentID.String != "bbbb" {
		t.Errorf("child parent_span_id = %v, want bbbb", parentID)
	}

	// Verify service_name was set from request-level field.
	var svcName string
	err = db.QueryRowContext(context.Background(),
		"SELECT service_name FROM spans WHERE span_id = ?", "bbbb").Scan(&svcName)
	if err != nil {
		t.Fatalf("scan service: %v", err)
	}
	if svcName != "test-svc" {
		t.Errorf("service_name = %q, want test-svc", svcName)
	}

	// Verify duration_ms was computed correctly.
	// (1050000000000 - 1000000000000) / 1e6 = 50000
	var durationMs int64
	err = db.QueryRowContext(context.Background(),
		"SELECT duration_ms FROM spans WHERE span_id = ?", "bbbb").Scan(&durationMs)
	if err != nil {
		t.Fatalf("scan duration: %v", err)
	}
	if durationMs != 50000 {
		t.Errorf("duration_ms = %d, want 50000", durationMs)
	}

	// Verify attributes were stored as JSON.
	var attrsStr sql.NullString
	err = db.QueryRowContext(context.Background(),
		"SELECT attributes FROM spans WHERE span_id = ?", "bbbb").Scan(&attrsStr)
	if err != nil {
		t.Fatalf("scan attrs: %v", err)
	}
	if !attrsStr.Valid {
		t.Fatal("attributes is NULL, want JSON")
	}
	var attrs map[string]any
	if err := json.Unmarshal([]byte(attrsStr.String), &attrs); err != nil {
		t.Fatalf("unmarshal attrs: %v", err)
	}
	if attrs["http.method"] != "GET" {
		t.Errorf("attrs[http.method] = %v, want GET", attrs["http.method"])
	}

	// Verify events were stored as JSON.
	var eventsStr sql.NullString
	err = db.QueryRowContext(context.Background(),
		"SELECT events FROM spans WHERE span_id = ?", "bbbb").Scan(&eventsStr)
	if err != nil {
		t.Fatalf("scan events: %v", err)
	}
	if !eventsStr.Valid {
		t.Fatal("events is NULL, want JSON")
	}
	var events []map[string]any
	if err := json.Unmarshal([]byte(eventsStr.String), &events); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1", len(events))
	}
	if events[0]["name"] != "started" {
		t.Errorf("events[0].name = %v, want started", events[0]["name"])
	}
}

func TestIngest_MalformedJSON(t *testing.T) {
	t.Parallel()
	app, _ := setupTestServer(t)

	req, _ := http.NewRequest("POST", "/api/ingest", bytes.NewReader([]byte(`{invalid`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestIngest_EmptySpans(t *testing.T) {
	t.Parallel()
	app, _ := setupTestServer(t)

	payload := IngestRequest{
		ServiceName: "test-svc",
		Spans:       []IngestSpan{},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/api/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestIngest_MissingTraceID(t *testing.T) {
	t.Parallel()
	app, _ := setupTestServer(t)

	payload := IngestRequest{
		ServiceName: "test-svc",
		Spans: []IngestSpan{
			{
				SpanID:     "bbbb",
				Name:       "op",
				StartTime:  1000,
				EndTime:    2000,
				StatusCode: "OK",
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/api/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestIngest_TreeStructure(t *testing.T) {
	t.Parallel()
	app, db := setupTestServer(t)

	// 3 spans forming: root -> child1, root -> child2
	payload := IngestRequest{
		ServiceName: "test-svc",
		Spans: []IngestSpan{
			{
				TraceID:    "trace-tree",
				SpanID:     "root",
				Name:       "root-op",
				SpanKind:   1,
				StartTime:  1000000000,
				EndTime:    2000000000,
				StatusCode: "OK",
			},
			{
				TraceID:      "trace-tree",
				SpanID:       "child1",
				ParentSpanID: "root",
				Name:         "child1-op",
				SpanKind:     1,
				StartTime:    1100000000,
				EndTime:      1500000000,
				StatusCode:   "OK",
			},
			{
				TraceID:      "trace-tree",
				SpanID:       "child2",
				ParentSpanID: "root",
				Name:         "child2-op",
				SpanKind:     1,
				StartTime:    1500000000,
				EndTime:      1900000000,
				StatusCode:   "ERROR",
				StatusMsg:    "something failed",
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/api/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 202 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, respBody)
	}

	// Wait for flush.
	time.Sleep(200 * time.Millisecond)

	// Verify all 3 spans present.
	var count int
	err = db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM spans WHERE trace_id = ?", "trace-tree").Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("span count = %d, want 3", count)
	}

	// Verify root has NULL parent.
	var parentID sql.NullString
	err = db.QueryRowContext(context.Background(),
		"SELECT parent_span_id FROM spans WHERE span_id = ?", "root").Scan(&parentID)
	if err != nil {
		t.Fatalf("scan root parent: %v", err)
	}
	if parentID.Valid {
		t.Errorf("root parent = %q, want NULL", parentID.String)
	}

	// Verify children reference root.
	for _, childID := range []string{"child1", "child2"} {
		err = db.QueryRowContext(context.Background(),
			"SELECT parent_span_id FROM spans WHERE span_id = ?", childID).Scan(&parentID)
		if err != nil {
			t.Fatalf("scan %s parent: %v", childID, err)
		}
		if !parentID.Valid || parentID.String != "root" {
			t.Errorf("%s parent = %v, want root", childID, parentID)
		}
	}

	// Verify error span has status_code and status_message.
	var statusCode string
	var statusMsg sql.NullString
	err = db.QueryRowContext(context.Background(),
		"SELECT status_code, status_message FROM spans WHERE span_id = ?", "child2").Scan(&statusCode, &statusMsg)
	if err != nil {
		t.Fatalf("scan status: %v", err)
	}
	if statusCode != "ERROR" {
		t.Errorf("status_code = %q, want ERROR", statusCode)
	}
	if !statusMsg.Valid || statusMsg.String != "something failed" {
		t.Errorf("status_message = %v, want 'something failed'", statusMsg)
	}
}
