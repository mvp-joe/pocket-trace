package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"pocket-trace/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestTools creates a tools instance backed by a real SQLite store in a temp directory.
func newTestTools(t *testing.T) *tools {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return &tools{store: s}
}

// seedSpans inserts spans into the store as test data.
func seedSpans(t *testing.T, tt *tools, spans []store.Span) {
	t.Helper()
	if err := tt.store.InsertSpans(context.Background(), spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}
}

// resultText extracts the text string from a CallToolResult's first TextContent.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *mcp.TextContent", result.Content[0])
	}
	return tc.Text
}

// unmarshalResult extracts and unmarshals the JSON text from a CallToolResult.
func unmarshalResult[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	text := resultText(t, result)
	var v T
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("unmarshal result: %v\ntext: %s", err, text)
	}
	return v
}

// --- Seed data helpers ---

// seedStandardData inserts a standard set of test spans used by multiple tests.
// Returns the current time used as the reference point.
//
// Data:
//   - trace-1: auth-svc, 2 spans (root + child), no errors
//   - trace-2: api-gateway -> auth-svc (cross-service), 1 error span
//   - trace-3: user-svc, 1 span, no errors
//   - trace-old: auth-svc, 1 span, 48h ago
func seedStandardData(t *testing.T, tt *tools) time.Time {
	t.Helper()
	now := time.Now()
	nowNano := now.UnixNano()
	oneHourAgo := nowNano - int64(1*time.Hour)
	twoDaysAgo := nowNano - int64(48*time.Hour)

	spans := []store.Span{
		// Trace 1: auth-svc, root + child
		{
			TraceID: "trace-1", SpanID: "t1-root",
			ServiceName: "auth-svc", SpanName: "handle-login",
			SpanKind: 2, StartTime: nowNano - 100_000_000, EndTime: nowNano,
			DurationMs: 100, StatusCode: "OK",
		},
		{
			TraceID: "trace-1", SpanID: "t1-child", ParentSpanID: "t1-root",
			ServiceName: "auth-svc", SpanName: "db-query",
			SpanKind: 3, StartTime: nowNano - 80_000_000, EndTime: nowNano - 20_000_000,
			DurationMs: 60, StatusCode: "OK",
		},
		// Trace 2: api-gateway -> auth-svc (cross-service, with error)
		{
			TraceID: "trace-2", SpanID: "t2-root",
			ServiceName: "api-gateway", SpanName: "handle-request",
			SpanKind: 2, StartTime: nowNano - 200_000_000, EndTime: nowNano - 50_000_000,
			DurationMs: 150, StatusCode: "OK",
		},
		{
			TraceID: "trace-2", SpanID: "t2-child", ParentSpanID: "t2-root",
			ServiceName: "auth-svc", SpanName: "validate-token",
			SpanKind: 3, StartTime: nowNano - 180_000_000, EndTime: nowNano - 60_000_000,
			DurationMs: 120, StatusCode: "ERROR", StatusMsg: "token expired",
		},
		// Trace 3: user-svc, single span
		{
			TraceID: "trace-3", SpanID: "t3-root",
			ServiceName: "user-svc", SpanName: "get-profile",
			SpanKind: 2, StartTime: oneHourAgo, EndTime: oneHourAgo + 50_000_000,
			DurationMs: 50, StatusCode: "OK",
		},
		// Trace old: auth-svc, 48h ago
		{
			TraceID: "trace-old", SpanID: "told-root",
			ServiceName: "auth-svc", SpanName: "old-operation",
			SpanKind: 1, StartTime: twoDaysAgo, EndTime: twoDaysAgo + 10_000_000,
			DurationMs: 10, StatusCode: "OK",
		},
	}
	seedSpans(t, tt, spans)
	return now
}

// --- list_services tests ---

func TestListServices_EmptyDB(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	result, _, err := tt.listServices(ctx, nil, struct{}{})
	if err != nil {
		t.Fatalf("listServices: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected IsError")
	}

	services := unmarshalResult[[]store.ServiceSummary](t, result)
	if len(services) != 0 {
		t.Errorf("len(services) = %d, want 0", len(services))
	}
}

func TestListServices_WithData(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	result, _, err := tt.listServices(ctx, nil, struct{}{})
	if err != nil {
		t.Fatalf("listServices: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected IsError")
	}

	services := unmarshalResult[[]store.ServiceSummary](t, result)
	if len(services) != 3 {
		t.Fatalf("len(services) = %d, want 3", len(services))
	}

	// Ordered by name: api-gateway, auth-svc, user-svc
	wantNames := []string{"api-gateway", "auth-svc", "user-svc"}
	for i, want := range wantNames {
		if services[i].Name != want {
			t.Errorf("services[%d].Name = %q, want %q", i, services[i].Name, want)
		}
	}

	// auth-svc has 4 spans (t1-root, t1-child, t2-child, told-root)
	for _, svc := range services {
		switch svc.Name {
		case "auth-svc":
			if svc.SpanCount != 4 {
				t.Errorf("auth-svc span count = %d, want 4", svc.SpanCount)
			}
		case "api-gateway":
			if svc.SpanCount != 1 {
				t.Errorf("api-gateway span count = %d, want 1", svc.SpanCount)
			}
		case "user-svc":
			if svc.SpanCount != 1 {
				t.Errorf("user-svc span count = %d, want 1", svc.SpanCount)
			}
		}
		if svc.LastSeen <= 0 {
			t.Errorf("%s lastSeen = %d, want > 0", svc.Name, svc.LastSeen)
		}
	}
}

// --- search_traces tests ---

func TestSearchTraces_NoFilters(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	result, _, err := tt.searchTraces(ctx, nil, SearchTracesInput{})
	if err != nil {
		t.Fatalf("searchTraces: %v", err)
	}

	traces := unmarshalResult[[]store.TraceSummary](t, result)
	if len(traces) != 4 {
		t.Fatalf("len(traces) = %d, want 4", len(traces))
	}

	// Should be ordered by start_time descending (newest first).
	for i := 1; i < len(traces); i++ {
		if traces[i].StartTime > traces[i-1].StartTime {
			t.Errorf("traces not sorted descending: [%d].StartTime=%d > [%d].StartTime=%d",
				i, traces[i].StartTime, i-1, traces[i-1].StartTime)
		}
	}
}

func TestSearchTraces_FilterByService(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	result, _, err := tt.searchTraces(ctx, nil, SearchTracesInput{Service: "user-svc"})
	if err != nil {
		t.Fatalf("searchTraces: %v", err)
	}

	traces := unmarshalResult[[]store.TraceSummary](t, result)
	if len(traces) != 1 {
		t.Fatalf("len(traces) = %d, want 1", len(traces))
	}
	if traces[0].TraceID != "trace-3" {
		t.Errorf("traceID = %s, want trace-3", traces[0].TraceID)
	}
}

func TestSearchTraces_FilterByDuration(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	// MinDuration=100 should exclude trace-3 (50ms) and trace-old (10ms) spans,
	// but include trace-1 (100ms root) and trace-2 (150ms root, 120ms child).
	result, _, err := tt.searchTraces(ctx, nil, SearchTracesInput{MinDurationMs: 100, MaxDurationMs: 130})
	if err != nil {
		t.Fatalf("searchTraces: %v", err)
	}

	traces := unmarshalResult[[]store.TraceSummary](t, result)
	// trace-1 has a 100ms root and 60ms child. 100ms matches.
	// trace-2 has 150ms root (no match) but 120ms child (match).
	// So both trace-1 and trace-2 should match.
	for _, tr := range traces {
		if tr.TraceID == "trace-3" || tr.TraceID == "trace-old" {
			t.Errorf("unexpected trace %s in duration-filtered results", tr.TraceID)
		}
	}
}

func TestSearchTraces_LimitDefault(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	// Insert 25 traces to verify default limit of 20.
	now := time.Now().UnixNano()
	for i := 0; i < 25; i++ {
		seedSpans(t, tt, []store.Span{
			{
				TraceID: fmt.Sprintf("t%02d", i), SpanID: fmt.Sprintf("s%02d", i),
				ServiceName: "svc", SpanName: "op", SpanKind: 1,
				StartTime: now + int64(i)*1_000_000, EndTime: now + int64(i)*1_000_000 + 1_000_000,
				DurationMs: 1, StatusCode: "OK",
			},
		})
	}

	result, _, err := tt.searchTraces(ctx, nil, SearchTracesInput{})
	if err != nil {
		t.Fatalf("searchTraces: %v", err)
	}

	traces := unmarshalResult[[]store.TraceSummary](t, result)
	if len(traces) != 20 {
		t.Errorf("len(traces) = %d, want 20 (default limit)", len(traces))
	}
}

func TestSearchTraces_LimitClamped(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	// Limit of 200 should be clamped to 100. We only have 4 traces so we get 4.
	result, _, err := tt.searchTraces(ctx, nil, SearchTracesInput{Limit: 200})
	if err != nil {
		t.Fatalf("searchTraces: %v", err)
	}

	traces := unmarshalResult[[]store.TraceSummary](t, result)
	// With only 4 traces, we get all 4 (clamped to 100, but only 4 exist).
	if len(traces) != 4 {
		t.Errorf("len(traces) = %d, want 4", len(traces))
	}
}

// --- get_trace tests ---

func TestGetTrace_Success(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	result, _, err := tt.getTrace(ctx, nil, GetTraceInput{TraceID: "trace-1"})
	if err != nil {
		t.Fatalf("getTrace: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected IsError")
	}

	detail := unmarshalResult[store.TraceDetail](t, result)
	if detail.TraceID != "trace-1" {
		t.Errorf("traceID = %s, want trace-1", detail.TraceID)
	}
	if detail.SpanCount != 2 {
		t.Errorf("spanCount = %d, want 2", detail.SpanCount)
	}
	if len(detail.Roots) != 1 {
		t.Fatalf("len(roots) = %d, want 1", len(detail.Roots))
	}
	root := detail.Roots[0]
	if root.SpanID != "t1-root" {
		t.Errorf("root spanID = %s, want t1-root", root.SpanID)
	}
	if len(root.Children) != 1 {
		t.Errorf("root children = %d, want 1", len(root.Children))
	}
}

func TestGetTrace_NotFound(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	result, _, err := tt.getTrace(ctx, nil, GetTraceInput{TraceID: "nonexistent"})
	if err != nil {
		t.Fatalf("getTrace returned Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError = true for nonexistent trace")
	}
	text := resultText(t, result)
	if text == "" {
		t.Error("expected non-empty error text")
	}
}

// --- get_span tests ---

func TestGetSpan_Success(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	result, _, err := tt.getSpan(ctx, nil, GetSpanInput{TraceID: "trace-2", SpanID: "t2-child"})
	if err != nil {
		t.Fatalf("getSpan: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected IsError")
	}

	span := unmarshalResult[store.Span](t, result)
	if span.SpanID != "t2-child" {
		t.Errorf("spanID = %s, want t2-child", span.SpanID)
	}
	if span.TraceID != "trace-2" {
		t.Errorf("traceID = %s, want trace-2", span.TraceID)
	}
	if span.ServiceName != "auth-svc" {
		t.Errorf("serviceName = %s, want auth-svc", span.ServiceName)
	}
	if span.StatusCode != "ERROR" {
		t.Errorf("statusCode = %s, want ERROR", span.StatusCode)
	}
	if span.StatusMsg != "token expired" {
		t.Errorf("statusMsg = %s, want 'token expired'", span.StatusMsg)
	}
	if span.ParentSpanID != "t2-root" {
		t.Errorf("parentSpanID = %s, want t2-root", span.ParentSpanID)
	}
	if span.SpanName != "validate-token" {
		t.Errorf("spanName = %s, want validate-token", span.SpanName)
	}
	if span.DurationMs != 120 {
		t.Errorf("durationMs = %d, want 120", span.DurationMs)
	}
}

func TestGetSpan_NotFound(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	result, _, err := tt.getSpan(ctx, nil, GetSpanInput{TraceID: "nope", SpanID: "nope"})
	if err != nil {
		t.Fatalf("getSpan returned Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError = true for nonexistent span")
	}
	text := resultText(t, result)
	if text == "" {
		t.Error("expected non-empty error text")
	}
}

// --- find_error_traces tests ---

func TestFindErrorTraces_ReturnsOnlyErrors(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	result, _, err := tt.findErrorTraces(ctx, nil, FindErrorTracesInput{})
	if err != nil {
		t.Fatalf("findErrorTraces: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected IsError")
	}

	// Only trace-2 has errors.
	details := unmarshalResult[[]*store.TraceDetail](t, result)
	if len(details) != 1 {
		t.Fatalf("len(details) = %d, want 1", len(details))
	}
	if details[0].TraceID != "trace-2" {
		t.Errorf("traceID = %s, want trace-2", details[0].TraceID)
	}
	// Verify it's a full TraceDetail with span tree.
	if details[0].SpanCount != 2 {
		t.Errorf("spanCount = %d, want 2", details[0].SpanCount)
	}
	if len(details[0].Roots) != 1 {
		t.Fatalf("len(roots) = %d, want 1", len(details[0].Roots))
	}
	if details[0].ErrorCount != 1 {
		t.Errorf("errorCount = %d, want 1", details[0].ErrorCount)
	}
}

func TestFindErrorTraces_ServiceFilter(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	now := time.Now().UnixNano()
	seedSpans(t, tt, []store.Span{
		// Error trace in svc-a
		{TraceID: "err-a", SpanID: "ea-root", ServiceName: "svc-a", SpanName: "op",
			SpanKind: 1, StartTime: now - 100_000_000, EndTime: now,
			DurationMs: 100, StatusCode: "ERROR", StatusMsg: "fail"},
		// Error trace in svc-b
		{TraceID: "err-b", SpanID: "eb-root", ServiceName: "svc-b", SpanName: "op",
			SpanKind: 1, StartTime: now - 200_000_000, EndTime: now - 100_000_000,
			DurationMs: 100, StatusCode: "ERROR", StatusMsg: "fail"},
	})

	result, _, err := tt.findErrorTraces(ctx, nil, FindErrorTracesInput{Service: "svc-a"})
	if err != nil {
		t.Fatalf("findErrorTraces: %v", err)
	}

	details := unmarshalResult[[]*store.TraceDetail](t, result)
	if len(details) != 1 {
		t.Fatalf("len(details) = %d, want 1", len(details))
	}
	if details[0].TraceID != "err-a" {
		t.Errorf("traceID = %s, want err-a", details[0].TraceID)
	}
}

func TestFindErrorTraces_LimitDefault(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	now := time.Now().UnixNano()
	// Insert 10 error traces.
	for i := 0; i < 10; i++ {
		seedSpans(t, tt, []store.Span{
			{
				TraceID: fmt.Sprintf("err-%02d", i), SpanID: fmt.Sprintf("s-%02d", i),
				ServiceName: "svc", SpanName: "op", SpanKind: 1,
				StartTime: now - int64(i)*1_000_000_000, EndTime: now - int64(i)*1_000_000_000 + 1_000_000,
				DurationMs: 1, StatusCode: "ERROR", StatusMsg: "fail",
			},
		})
	}

	result, _, err := tt.findErrorTraces(ctx, nil, FindErrorTracesInput{})
	if err != nil {
		t.Fatalf("findErrorTraces: %v", err)
	}

	details := unmarshalResult[[]*store.TraceDetail](t, result)
	if len(details) != 5 {
		t.Errorf("len(details) = %d, want 5 (default limit)", len(details))
	}
}

func TestFindErrorTraces_NoErrors(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	now := time.Now().UnixNano()
	seedSpans(t, tt, []store.Span{
		{TraceID: "ok-1", SpanID: "s1", ServiceName: "svc", SpanName: "op",
			SpanKind: 1, StartTime: now, EndTime: now + 1_000_000,
			DurationMs: 1, StatusCode: "OK"},
	})

	result, _, err := tt.findErrorTraces(ctx, nil, FindErrorTracesInput{})
	if err != nil {
		t.Fatalf("findErrorTraces: %v", err)
	}

	details := unmarshalResult[[]*store.TraceDetail](t, result)
	if len(details) != 0 {
		t.Errorf("len(details) = %d, want 0", len(details))
	}
}

func TestFindErrorTraces_EmptyDB(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	result, _, err := tt.findErrorTraces(ctx, nil, FindErrorTracesInput{})
	if err != nil {
		t.Fatalf("findErrorTraces: %v", err)
	}

	details := unmarshalResult[[]*store.TraceDetail](t, result)
	if len(details) != 0 {
		t.Errorf("len(details) = %d, want 0", len(details))
	}
}

// --- get_dependencies tests ---

func TestGetDependencies_CrossService(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	// Default sinceHours = 24, so trace-2 (recent) should show api-gateway -> auth-svc.
	result, _, err := tt.getDependencies(ctx, nil, GetDependenciesInput{})
	if err != nil {
		t.Fatalf("getDependencies: %v", err)
	}

	deps := unmarshalResult[[]store.Dependency](t, result)
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d, want 1", len(deps))
	}
	if deps[0].Parent != "api-gateway" {
		t.Errorf("parent = %s, want api-gateway", deps[0].Parent)
	}
	if deps[0].Child != "auth-svc" {
		t.Errorf("child = %s, want auth-svc", deps[0].Child)
	}
	if deps[0].CallCount != 1 {
		t.Errorf("callCount = %d, want 1", deps[0].CallCount)
	}
}

func TestGetDependencies_DefaultSinceHours(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	now := time.Now()
	// One dependency from 2 hours ago (within 24h default).
	seedSpans(t, tt, []store.Span{
		{TraceID: "t1", SpanID: "p1", ServiceName: "svc-a", SpanName: "op",
			SpanKind: 2, StartTime: now.Add(-2 * time.Hour).UnixNano(),
			EndTime: now.Add(-2*time.Hour + time.Second).UnixNano(),
			DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t1", SpanID: "c1", ParentSpanID: "p1", ServiceName: "svc-b", SpanName: "op",
			SpanKind: 1, StartTime: now.Add(-2*time.Hour + 100*time.Millisecond).UnixNano(),
			EndTime: now.Add(-2*time.Hour + 500*time.Millisecond).UnixNano(),
			DurationMs: 400, StatusCode: "OK"},
	})

	result, _, err := tt.getDependencies(ctx, nil, GetDependenciesInput{})
	if err != nil {
		t.Fatalf("getDependencies: %v", err)
	}

	deps := unmarshalResult[[]store.Dependency](t, result)
	if len(deps) != 1 {
		t.Errorf("len(deps) = %d, want 1 (within default 24h window)", len(deps))
	}
}

func TestGetDependencies_CustomSinceHours(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	now := time.Now()
	// One dependency from 4 hours ago.
	seedSpans(t, tt, []store.Span{
		{TraceID: "t-old", SpanID: "p-old", ServiceName: "svc-a", SpanName: "op",
			SpanKind: 2, StartTime: now.Add(-4 * time.Hour).UnixNano(),
			EndTime: now.Add(-4*time.Hour + time.Second).UnixNano(),
			DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t-old", SpanID: "c-old", ParentSpanID: "p-old", ServiceName: "svc-b", SpanName: "op",
			SpanKind: 1, StartTime: now.Add(-4*time.Hour + 100*time.Millisecond).UnixNano(),
			EndTime: now.Add(-4*time.Hour + 500*time.Millisecond).UnixNano(),
			DurationMs: 400, StatusCode: "OK"},
		// One dependency from 30 minutes ago.
		{TraceID: "t-new", SpanID: "p-new", ServiceName: "svc-c", SpanName: "op",
			SpanKind: 2, StartTime: now.Add(-30 * time.Minute).UnixNano(),
			EndTime: now.Add(-30*time.Minute + time.Second).UnixNano(),
			DurationMs: 1000, StatusCode: "OK"},
		{TraceID: "t-new", SpanID: "c-new", ParentSpanID: "p-new", ServiceName: "svc-d", SpanName: "op",
			SpanKind: 1, StartTime: now.Add(-30*time.Minute + 100*time.Millisecond).UnixNano(),
			EndTime: now.Add(-30*time.Minute + 500*time.Millisecond).UnixNano(),
			DurationMs: 400, StatusCode: "OK"},
	})

	// sinceHours=2 should only include the 30-minute-old dependency, not the 4-hour-old one.
	result, _, err := tt.getDependencies(ctx, nil, GetDependenciesInput{SinceHours: 2})
	if err != nil {
		t.Fatalf("getDependencies: %v", err)
	}

	deps := unmarshalResult[[]store.Dependency](t, result)
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d, want 1", len(deps))
	}
	if deps[0].Parent != "svc-c" {
		t.Errorf("parent = %s, want svc-c", deps[0].Parent)
	}
	if deps[0].Child != "svc-d" {
		t.Errorf("child = %s, want svc-d", deps[0].Child)
	}
}

// --- get_status tests ---

func TestGetStatus(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()
	seedStandardData(t, tt)

	result, _, err := tt.getStatus(ctx, nil, struct{}{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected IsError")
	}

	stats := unmarshalResult[store.DBStats](t, result)
	if stats.SpanCount != 6 {
		t.Errorf("spanCount = %d, want 6", stats.SpanCount)
	}
	if stats.TraceCount != 4 {
		t.Errorf("traceCount = %d, want 4", stats.TraceCount)
	}
	if stats.DBSizeBytes <= 0 {
		t.Errorf("dbSizeBytes = %d, want > 0", stats.DBSizeBytes)
	}
}

func TestGetStatus_EmptyDB(t *testing.T) {
	t.Parallel()
	tt := newTestTools(t)
	ctx := context.Background()

	result, _, err := tt.getStatus(ctx, nil, struct{}{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}

	stats := unmarshalResult[store.DBStats](t, result)
	if stats.SpanCount != 0 {
		t.Errorf("spanCount = %d, want 0", stats.SpanCount)
	}
	if stats.TraceCount != 0 {
		t.Errorf("traceCount = %d, want 0", stats.TraceCount)
	}
}
