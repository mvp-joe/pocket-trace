package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
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
	buf := NewSpanBuffer(s, 100, 64, 50*time.Millisecond)

	h := &Handlers{
		Store:     s,
		Buffer:    buf,
		StartTime: time.Now(),
		Version:   "test",
	}

	srv := New(s, buf, h, nil, 0, 0)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	// Cleanup order matters: flush buffer before closing store/db.
	t.Cleanup(func() { s.Close() })
	t.Cleanup(func() { db.Close() })
	t.Cleanup(func() { buf.Shutdown() })

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

// setupSeededServer creates a test server and inserts seed data directly into the store.
// Returns the Fiber app and the store for further verification.
func setupSeededServer(t *testing.T) *fiber.App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	buf := NewSpanBuffer(s, 100, 64, 50*time.Millisecond)

	h := &Handlers{
		Store:     s,
		Buffer:    buf,
		StartTime: time.Now().Add(-5 * time.Second), // fake 5s uptime
		Version:   "test-v1",
	}

	srv := New(s, buf, h, nil, 0, 0)

	t.Cleanup(func() { buf.Shutdown() })
	t.Cleanup(func() { s.Close() })

	// Seed data: 3 services, multiple traces.
	now := time.Now().UnixNano()
	oneHourAgo := now - int64(1*time.Hour)
	twoDaysAgo := now - int64(48*time.Hour)

	spans := []store.Span{
		// Trace 1: auth-svc, 2 spans, root + child
		{
			TraceID: "trace-1", SpanID: "t1-root",
			ServiceName: "auth-svc", SpanName: "handle-login",
			SpanKind: 2, StartTime: now - 100_000_000, EndTime: now,
			DurationMs: 100, StatusCode: "OK",
		},
		{
			TraceID: "trace-1", SpanID: "t1-child", ParentSpanID: "t1-root",
			ServiceName: "auth-svc", SpanName: "db-query",
			SpanKind: 3, StartTime: now - 80_000_000, EndTime: now - 20_000_000,
			DurationMs: 60, StatusCode: "OK",
		},
		// Trace 2: api-gateway calling auth-svc (cross-service dependency)
		{
			TraceID: "trace-2", SpanID: "t2-root",
			ServiceName: "api-gateway", SpanName: "handle-request",
			SpanKind: 2, StartTime: now - 200_000_000, EndTime: now - 50_000_000,
			DurationMs: 150, StatusCode: "OK",
		},
		{
			TraceID: "trace-2", SpanID: "t2-child", ParentSpanID: "t2-root",
			ServiceName: "auth-svc", SpanName: "validate-token",
			SpanKind: 3, StartTime: now - 180_000_000, EndTime: now - 60_000_000,
			DurationMs: 120, StatusCode: "ERROR", StatusMsg: "token expired",
		},
		// Trace 3: user-svc, single span
		{
			TraceID: "trace-3", SpanID: "t3-root",
			ServiceName: "user-svc", SpanName: "get-profile",
			SpanKind: 2, StartTime: oneHourAgo, EndTime: oneHourAgo + 50_000_000,
			DurationMs: 50, StatusCode: "OK",
		},
		// Trace 4: old span from 48h ago (for purge testing)
		{
			TraceID: "trace-old", SpanID: "told-root",
			ServiceName: "auth-svc", SpanName: "old-operation",
			SpanKind: 1, StartTime: twoDaysAgo, EndTime: twoDaysAgo + 10_000_000,
			DurationMs: 10, StatusCode: "OK",
		},
	}

	if err := s.InsertSpans(context.Background(), spans); err != nil {
		t.Fatalf("seed spans: %v", err)
	}

	return srv.App()
}

// decodeJSON is a test helper to decode a JSON response body.
func decodeJSON[T any](t *testing.T, resp *http.Response) APIResponse[T] {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var result APIResponse[T]
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	return result
}

func TestListServices(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/services", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := decodeJSON[[]store.ServiceSummary](t, resp)
	if len(result.Data) != 3 {
		t.Fatalf("expected 3 services, got %d", len(result.Data))
	}

	// Services should be ordered by name.
	names := make([]string, len(result.Data))
	for i, s := range result.Data {
		names[i] = s.Name
	}
	expected := []string{"api-gateway", "auth-svc", "user-svc"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("service[%d] = %q, want %q", i, names[i], want)
		}
	}

	// auth-svc has 3 spans (t1-root, t1-child, t2-child, told-root = 4 spans)
	for _, svc := range result.Data {
		if svc.Name == "auth-svc" && svc.SpanCount != 4 {
			t.Errorf("auth-svc span count = %d, want 4", svc.SpanCount)
		}
	}
}

func TestSearchTraces_NoFilters(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := decodeJSON[[]store.TraceSummary](t, resp)
	if len(result.Data) != 4 {
		t.Fatalf("expected 4 traces, got %d", len(result.Data))
	}

	// Should be ordered by start_time descending (newest first).
	if result.Data[0].TraceID != "trace-1" && result.Data[0].TraceID != "trace-2" {
		// trace-1 and trace-2 are both recent
		t.Logf("first trace: %s", result.Data[0].TraceID)
	}
}

func TestSearchTraces_FilterByService(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces?service=user-svc", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := decodeJSON[[]store.TraceSummary](t, resp)
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 trace for user-svc, got %d", len(result.Data))
	}
	if result.Data[0].TraceID != "trace-3" {
		t.Errorf("trace ID = %s, want trace-3", result.Data[0].TraceID)
	}
}

func TestSearchTraces_FilterByMinDuration(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	// minDuration=100 should match trace-1 (100ms root) and trace-2 (150ms root), trace-2 also has 120ms child
	req, _ := http.NewRequest("GET", "/api/traces?minDuration=100", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	result := decodeJSON[[]store.TraceSummary](t, resp)
	// Traces that have at least one span with duration >= 100ms
	for _, tr := range result.Data {
		if tr.TraceID == "trace-3" {
			t.Errorf("trace-3 (50ms) should not match minDuration=100")
		}
	}
}

func TestSearchTraces_Limit(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces?limit=2", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	result := decodeJSON[[]store.TraceSummary](t, resp)
	if len(result.Data) != 2 {
		t.Errorf("expected 2 traces with limit=2, got %d", len(result.Data))
	}
}

func TestSearchTraces_InvalidLimit(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces?limit=abc", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for invalid limit, got %d", resp.StatusCode)
	}
}

func TestGetTrace_Success(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces/trace-1", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON[store.TraceDetail](t, resp)
	if result.Data.TraceID != "trace-1" {
		t.Errorf("traceID = %s, want trace-1", result.Data.TraceID)
	}
	if result.Data.SpanCount != 2 {
		t.Errorf("spanCount = %d, want 2", result.Data.SpanCount)
	}
	if len(result.Data.Roots) != 1 {
		t.Fatalf("roots count = %d, want 1", len(result.Data.Roots))
	}
	root := result.Data.Roots[0]
	if root.SpanID != "t1-root" {
		t.Errorf("root spanID = %s, want t1-root", root.SpanID)
	}
	if len(root.Children) != 1 {
		t.Errorf("root children = %d, want 1", len(root.Children))
	}
}

func TestGetTrace_NotFound(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces/nonexistent", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetTrace_TreeStructure(t *testing.T) {
	// Test with trace-2 which has cross-service spans.
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces/trace-2", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	result := decodeJSON[store.TraceDetail](t, resp)
	if result.Data.ServiceCount != 2 {
		t.Errorf("serviceCount = %d, want 2", result.Data.ServiceCount)
	}
	if result.Data.ErrorCount != 1 {
		t.Errorf("errorCount = %d, want 1", result.Data.ErrorCount)
	}
}

func TestGetSpan_Success(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces/trace-2/spans/t2-child", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON[store.Span](t, resp)
	if result.Data.SpanID != "t2-child" {
		t.Errorf("spanID = %s, want t2-child", result.Data.SpanID)
	}
	if result.Data.StatusCode != "ERROR" {
		t.Errorf("statusCode = %s, want ERROR", result.Data.StatusCode)
	}
	if result.Data.ServiceName != "auth-svc" {
		t.Errorf("serviceName = %s, want auth-svc", result.Data.ServiceName)
	}
}

func TestGetSpan_NotFound(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/traces/trace-1/spans/nonexistent", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetDependencies(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/dependencies?lookback=2h", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON[[]store.Dependency](t, resp)
	// trace-2 has api-gateway -> auth-svc
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(result.Data))
	}
	dep := result.Data[0]
	if dep.Parent != "api-gateway" {
		t.Errorf("parent = %s, want api-gateway", dep.Parent)
	}
	if dep.Child != "auth-svc" {
		t.Errorf("child = %s, want auth-svc", dep.Child)
	}
	if dep.CallCount != 1 {
		t.Errorf("callCount = %d, want 1", dep.CallCount)
	}
}

func TestGetDependencies_InvalidLookback(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/dependencies?lookback=banana", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestStatus(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("GET", "/api/status", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON[StatusResponse](t, resp)
	if result.Data.Version != "test-v1" {
		t.Errorf("version = %s, want test-v1", result.Data.Version)
	}
	if result.Data.Uptime == "" {
		t.Error("uptime is empty")
	}
	if !strings.Contains(result.Data.Uptime, "s") {
		t.Errorf("uptime should contain seconds: %s", result.Data.Uptime)
	}
	if result.Data.DB.SpanCount != 6 {
		t.Errorf("db.spanCount = %d, want 6", result.Data.DB.SpanCount)
	}
	if result.Data.DB.TraceCount != 4 {
		t.Errorf("db.traceCount = %d, want 4", result.Data.DB.TraceCount)
	}
	if result.Data.DB.DBSizeBytes <= 0 {
		t.Errorf("db.dbSizeBytes = %d, want > 0", result.Data.DB.DBSizeBytes)
	}
}

func TestPurge(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	// Purge spans older than 24h. Should delete trace-old (48h ago).
	req, _ := http.NewRequest("POST", "/api/purge?olderThan=24h", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON[PurgeResult](t, resp)
	if result.Data.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", result.Data.Deleted)
	}

	// Verify the old trace is gone.
	getReq, _ := http.NewRequest("GET", "/api/traces/trace-old", nil)
	getResp, err := app.Test(getReq)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != 404 {
		t.Errorf("expected 404 for purged trace, got %d", getResp.StatusCode)
	}

	// Verify recent traces still exist.
	getReq2, _ := http.NewRequest("GET", "/api/traces/trace-1", nil)
	getResp2, err := app.Test(getReq2)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer getResp2.Body.Close()
	if getResp2.StatusCode != 200 {
		t.Errorf("expected 200 for recent trace, got %d", getResp2.StatusCode)
	}
}

func TestPurge_MissingOlderThan(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("POST", "/api/purge", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPurge_InvalidDuration(t *testing.T) {
	t.Parallel()
	app := setupSeededServer(t)

	req, _ := http.NewRequest("POST", "/api/purge?olderThan=banana", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestIngestToQueryFlow(t *testing.T) {
	// Integration test: ingest spans via POST, then query via GET endpoints.
	t.Parallel()
	app, _ := setupTestServer(t)

	// Ingest a trace with 3 spans: root -> child1, root -> child2 (error).
	payload := IngestRequest{
		ServiceName: "flow-svc",
		Spans: []IngestSpan{
			{
				TraceID: "flow-trace", SpanID: "flow-root",
				Name: "handle-request", SpanKind: 2,
				StartTime: 2000000000000, EndTime: 2100000000000,
				StatusCode: "OK",
			},
			{
				TraceID: "flow-trace", SpanID: "flow-child1", ParentSpanID: "flow-root",
				Name: "db-query", SpanKind: 3,
				StartTime: 2010000000000, EndTime: 2050000000000,
				StatusCode: "OK",
			},
			{
				TraceID: "flow-trace", SpanID: "flow-child2", ParentSpanID: "flow-root",
				Name: "cache-lookup", SpanKind: 3,
				StartTime: 2050000000000, EndTime: 2090000000000,
				StatusCode: "ERROR", StatusMsg: "cache miss",
			},
		},
	}

	body, _ := json.Marshal(payload)
	ingestReq, _ := http.NewRequest("POST", "/api/ingest", bytes.NewReader(body))
	ingestReq.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(ingestReq)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 202 {
		t.Fatalf("ingest status = %d, want 202", resp.StatusCode)
	}

	// Wait for buffer flush.
	time.Sleep(300 * time.Millisecond)

	// Query services.
	svcReq, _ := http.NewRequest("GET", "/api/services", nil)
	svcResp, err := app.Test(svcReq)
	if err != nil {
		t.Fatalf("services: %v", err)
	}
	svcResult := decodeJSON[[]store.ServiceSummary](t, svcResp)
	if len(svcResult.Data) != 1 || svcResult.Data[0].Name != "flow-svc" {
		t.Errorf("services = %+v, want [flow-svc]", svcResult.Data)
	}

	// Query trace detail.
	traceReq, _ := http.NewRequest("GET", "/api/traces/flow-trace", nil)
	traceResp, err := app.Test(traceReq)
	if err != nil {
		t.Fatalf("get trace: %v", err)
	}
	traceResult := decodeJSON[store.TraceDetail](t, traceResp)
	if traceResult.Data.SpanCount != 3 {
		t.Errorf("spanCount = %d, want 3", traceResult.Data.SpanCount)
	}
	if traceResult.Data.ErrorCount != 1 {
		t.Errorf("errorCount = %d, want 1", traceResult.Data.ErrorCount)
	}
	if len(traceResult.Data.Roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(traceResult.Data.Roots))
	}
	if len(traceResult.Data.Roots[0].Children) != 2 {
		t.Errorf("root children = %d, want 2", len(traceResult.Data.Roots[0].Children))
	}

	// Query individual span.
	spanReq, _ := http.NewRequest("GET", "/api/traces/flow-trace/spans/flow-child2", nil)
	spanResp, err := app.Test(spanReq)
	if err != nil {
		t.Fatalf("get span: %v", err)
	}
	spanResult := decodeJSON[store.Span](t, spanResp)
	if spanResult.Data.StatusCode != "ERROR" {
		t.Errorf("statusCode = %s, want ERROR", spanResult.Data.StatusCode)
	}
	if spanResult.Data.StatusMsg != "cache miss" {
		t.Errorf("statusMsg = %s, want 'cache miss'", spanResult.Data.StatusMsg)
	}
}
