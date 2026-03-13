package trace

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestStart_CreatesSpanWithID(t *testing.T) {
	ctx := context.Background()

	span, childCtx := Start(ctx, "test-operation")

	if span == nil {
		t.Fatal("expected span to be non-nil")
	}
	if span.name != "test-operation" {
		t.Errorf("expected name 'test-operation', got %q", span.name)
	}
	if span.spanID.IsZero() {
		t.Error("expected spanID to be set")
	}
	if !span.parentID.IsZero() {
		t.Errorf("expected no parentID for root span, got %q", span.parentID)
	}
	if childCtx == ctx {
		t.Error("expected childCtx to be different from parent ctx")
	}
}

func TestStart_NestedSpansHaveParentID(t *testing.T) {
	ctx := context.Background()

	parentSpan, parentCtx := Start(ctx, "parent-operation")
	childSpan, _ := Start(parentCtx, "child-operation")

	if childSpan.parentID != parentSpan.spanID {
		t.Errorf("expected child parentID %q to equal parent spanID %q",
			childSpan.parentID, parentSpan.spanID)
	}
}

func TestStart_NestedSpansShareTraceID(t *testing.T) {
	ctx := context.Background()

	parentSpan, parentCtx := Start(ctx, "parent")
	childSpan, childCtx := Start(parentCtx, "child")
	grandchildSpan, _ := Start(childCtx, "grandchild")

	if childSpan.traceID != parentSpan.traceID {
		t.Errorf("child traceID %q != parent traceID %q", childSpan.traceID, parentSpan.traceID)
	}
	if grandchildSpan.traceID != parentSpan.traceID {
		t.Errorf("grandchild traceID %q != parent traceID %q", grandchildSpan.traceID, parentSpan.traceID)
	}
}

func TestStart_RootSpansGetUniqueTraceIDs(t *testing.T) {
	ctx := context.Background()

	span1, _ := Start(ctx, "op1")
	span2, _ := Start(ctx, "op2")

	if span1.traceID == span2.traceID {
		t.Errorf("expected different traceIDs for independent root spans, got %q", span1.traceID)
	}
}

func TestStart_LogsEntryWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	span, _ := Start(ctx, "logged-op", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "→ logged-op") {
		t.Errorf("expected log to contain entry marker, got: %s", output)
	}
	if !strings.Contains(output, "span_id="+span.spanID.String()) {
		t.Errorf("expected log to contain span_id, got: %s", output)
	}
	if !strings.Contains(output, "trace_id="+span.traceID.String()) {
		t.Errorf("expected log to contain trace_id, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected log to contain custom attr, got: %s", output)
	}
}

func TestStart_NestedSpanLogsParentID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	parentSpan, parentCtx := Start(ctx, "parent")
	buf.Reset()

	Start(parentCtx, "child")

	output := buf.String()
	if !strings.Contains(output, "parent_id="+parentSpan.spanID.String()) {
		t.Errorf("expected log to contain parent_id, got: %s", output)
	}
}

func TestEvent_LogsWithElapsedTime(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	span, _ := Start(ctx, "test-op")
	buf.Reset()

	span.Event("checkpoint", "status", "ok")

	output := buf.String()
	if !strings.Contains(output, "• checkpoint") {
		t.Errorf("expected event marker, got: %s", output)
	}
	if !strings.Contains(output, "elapsed_ms=") {
		t.Errorf("expected elapsed_ms, got: %s", output)
	}
	if !strings.Contains(output, "status=ok") {
		t.Errorf("expected custom attr, got: %s", output)
	}
}

func TestEnd_LogsDuration(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	span, _ := Start(ctx, "test-op")
	buf.Reset()

	span.End()

	output := buf.String()
	if !strings.Contains(output, "← test-op") {
		t.Errorf("expected exit marker, got: %s", output)
	}
	if !strings.Contains(output, "duration_ms=") {
		t.Errorf("expected duration_ms, got: %s", output)
	}
	if !strings.Contains(output, "level=INFO") {
		t.Errorf("expected INFO level, got: %s", output)
	}
}

func TestEndErr_NilError_LogsInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	span, _ := Start(ctx, "test-op")
	buf.Reset()

	var err error
	span.EndErr(&err)

	output := buf.String()
	if !strings.Contains(output, "level=INFO") {
		t.Errorf("expected INFO level for nil error, got: %s", output)
	}
	if strings.Contains(output, "error=") {
		t.Errorf("expected no error attr for nil error, got: %s", output)
	}
}

func TestEndErr_WithError_LogsError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	span, _ := Start(ctx, "test-op")
	buf.Reset()

	err := errors.New("something went wrong")
	span.EndErr(&err)

	output := buf.String()
	if !strings.Contains(output, "level=ERROR") {
		t.Errorf("expected ERROR level, got: %s", output)
	}
	if !strings.Contains(output, "error=\"something went wrong\"") {
		t.Errorf("expected error message in log, got: %s", output)
	}
}

func TestEnd_NestedSpanLogsParentID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	parentSpan, parentCtx := Start(ctx, "parent")
	childSpan, _ := Start(parentCtx, "child")
	buf.Reset()

	childSpan.End()

	output := buf.String()
	if !strings.Contains(output, "parent_id="+parentSpan.spanID.String()) {
		t.Errorf("expected parent_id in end log, got: %s", output)
	}
}

func TestEvent_NestedSpanLogsParentID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	ctx := context.Background()
	parentSpan, parentCtx := Start(ctx, "parent")
	childSpan, _ := Start(parentCtx, "child")
	buf.Reset()

	childSpan.Event("test-event")

	output := buf.String()
	if !strings.Contains(output, "parent_id="+parentSpan.spanID.String()) {
		t.Errorf("expected parent_id in event log, got: %s", output)
	}
}

func TestSpanID_Format(t *testing.T) {
	id := newSpanID()
	s := id.String()

	// 8 bytes = 16 hex chars
	if len(s) != 16 {
		t.Errorf("expected 16 hex chars, got %d for %q", len(s), s)
	}

	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex character %q in span ID %q", c, s)
		}
	}
}

func TestTraceID_Format(t *testing.T) {
	id := newTraceID()
	s := id.String()

	// 16 bytes = 32 hex chars
	if len(s) != 32 {
		t.Errorf("expected 32 hex chars, got %d for %q", len(s), s)
	}
}

func TestSpanID_Unique(t *testing.T) {
	ids := make(map[SpanID]bool)
	for i := 0; i < 1000; i++ {
		id := newSpanID()
		if ids[id] {
			t.Fatalf("duplicate span ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestSpanID_ConcurrentUnique(t *testing.T) {
	const goroutines = 10
	const idsPerGoroutine = 100

	var mu sync.Mutex
	ids := make(map[SpanID]bool)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localIDs := make([]SpanID, idsPerGoroutine)
			for j := 0; j < idsPerGoroutine; j++ {
				localIDs[j] = newSpanID()
			}
			mu.Lock()
			for _, id := range localIDs {
				if ids[id] {
					t.Errorf("duplicate span ID: %s", id)
				}
				ids[id] = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
}

func TestExporter_ReceivesSpanOnEnd(t *testing.T) {
	var mu sync.Mutex
	var got []*FinishedSpan
	exp := &funcExporter{
		fn: func(_ context.Context, s *FinishedSpan) {
			mu.Lock()
			got = append(got, s)
			mu.Unlock()
		},
	}

	SetExporter(exp)
	defer SetExporter(nil)

	ctx := context.Background()
	span, _ := Start(ctx, "exported-op", "k", "v")
	span.Event("mid", "ek", "ev")
	span.End()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(got))
	}
	fs := got[0]
	if fs.Name != "exported-op" {
		t.Errorf("expected name 'exported-op', got %q", fs.Name)
	}
	if fs.TraceID != span.traceID {
		t.Errorf("traceID mismatch")
	}
	if fs.SpanID != span.spanID {
		t.Errorf("spanID mismatch")
	}
	if fs.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", fs.Status)
	}
	if len(fs.Attrs) != 1 || fs.Attrs[0].Key != "k" {
		t.Errorf("expected attrs [{k v}], got %v", fs.Attrs)
	}
	if len(fs.Events) != 1 || fs.Events[0].Name != "mid" {
		t.Errorf("expected 1 event 'mid', got %v", fs.Events)
	}
}

func TestExporter_ReceivesErrorStatus(t *testing.T) {
	var mu sync.Mutex
	var got []*FinishedSpan
	exp := &funcExporter{
		fn: func(_ context.Context, s *FinishedSpan) {
			mu.Lock()
			got = append(got, s)
			mu.Unlock()
		},
	}

	SetExporter(exp)
	defer SetExporter(nil)

	ctx := context.Background()
	span, _ := Start(ctx, "fail-op")
	err := errors.New("boom")
	span.EndErr(&err)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(got))
	}
	if got[0].Status != StatusError {
		t.Errorf("expected StatusError, got %d", got[0].Status)
	}
	if got[0].StatusMsg != "boom" {
		t.Errorf("expected status message 'boom', got %q", got[0].StatusMsg)
	}
}

// funcExporter is a test helper that calls a function for each exported span.
type funcExporter struct {
	fn func(context.Context, *FinishedSpan)
}

func (e *funcExporter) ExportSpan(ctx context.Context, s *FinishedSpan) { e.fn(ctx, s) }
func (e *funcExporter) Shutdown(context.Context) error                  { return nil }
