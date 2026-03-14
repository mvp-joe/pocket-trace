package trace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// collectServer creates a test HTTP server that collects ingest requests.
// Returns the server, a function to retrieve collected requests, and a function to wait for N spans.
func collectServer(t *testing.T) (*httptest.Server, func() []ingestRequest, func(int)) {
	t.Helper()
	var mu sync.Mutex
	var reqs []ingestRequest
	var totalSpans int
	notify := make(chan struct{}, 256)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ingest" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
			http.Error(w, "bad", 500)
			return
		}
		var req ingestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshaling: %v", err)
			http.Error(w, "bad", 400)
			return
		}
		mu.Lock()
		reqs = append(reqs, req)
		totalSpans += len(req.Spans)
		spanCount := totalSpans
		mu.Unlock()

		// Signal for each span received.
		for range len(req.Spans) {
			select {
			case notify <- struct{}{}:
			default:
			}
		}
		_ = spanCount

		w.WriteHeader(202)
		w.Write([]byte(`{"accepted":` + string(rune('0'+len(req.Spans))) + `}`))
	}))

	getReqs := func() []ingestRequest {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]ingestRequest, len(reqs))
		copy(cp, reqs)
		return cp
	}

	waitForSpans := func(n int) {
		t.Helper()
		for range n {
			select {
			case <-notify:
			case <-time.After(5 * time.Second):
				mu.Lock()
				got := totalSpans
				mu.Unlock()
				t.Fatalf("timed out waiting for spans: got %d, want %d", got, n)
			}
		}
	}

	t.Cleanup(srv.Close)
	return srv, getReqs, waitForSpans
}

func makeTestSpan(name string) *FinishedSpan {
	now := time.Now()
	return &FinishedSpan{
		TraceID:   newTraceID(),
		SpanID:    newSpanID(),
		Name:      name,
		Start:     now,
		End:       now.Add(10 * time.Millisecond),
		Status:    StatusOK,
		StatusMsg: "",
	}
}

func TestHTTPExporter_ExportSpanQueues(t *testing.T) {
	srv, getReqs, waitForSpans := collectServer(t)

	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))
	defer e.Shutdown(context.Background())

	span := makeTestSpan("queued-span")
	e.ExportSpan(context.Background(), span)

	waitForSpans(1)

	reqs := getReqs()
	if len(reqs) == 0 {
		t.Fatal("expected at least one request")
	}
	if len(reqs[0].Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(reqs[0].Spans))
	}
	if reqs[0].Spans[0].Name != "queued-span" {
		t.Errorf("expected span name 'queued-span', got %q", reqs[0].Spans[0].Name)
	}
}

func TestHTTPExporter_FlushSendsCorrectJSON(t *testing.T) {
	srv, getReqs, waitForSpans := collectServer(t)

	SetServiceName("test-svc")
	defer SetServiceName("unknown")

	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))
	defer e.Shutdown(context.Background())

	now := time.Now()
	span := &FinishedSpan{
		TraceID:   newTraceID(),
		SpanID:    newSpanID(),
		ParentID:  newSpanID(), // non-zero parent
		Name:      "test-op",
		Start:     now,
		End:       now.Add(42 * time.Millisecond),
		Status:    StatusError,
		StatusMsg: "something broke",
		Attrs:     []Attr{{Key: "http.method", Value: "GET"}, {Key: "http.status", Value: 200}},
		Events: []SpanEvent{
			{Name: "cache-miss", Time: now.Add(5 * time.Millisecond), Attrs: []Attr{{Key: "key", Value: "users"}}},
		},
	}

	e.ExportSpan(context.Background(), span)
	waitForSpans(1)

	reqs := getReqs()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	req := reqs[0]
	if req.ServiceName != "test-svc" {
		t.Errorf("expected serviceName 'test-svc', got %q", req.ServiceName)
	}

	s := req.Spans[0]
	if s.TraceID != span.TraceID.String() {
		t.Errorf("traceId mismatch: got %q, want %q", s.TraceID, span.TraceID.String())
	}
	if s.SpanID != span.SpanID.String() {
		t.Errorf("spanId mismatch")
	}
	if s.ParentSpanID != span.ParentID.String() {
		t.Errorf("parentSpanId mismatch: got %q, want %q", s.ParentSpanID, span.ParentID.String())
	}
	if s.Name != "test-op" {
		t.Errorf("name mismatch: got %q", s.Name)
	}
	if s.SpanKind != 1 {
		t.Errorf("expected spanKind 1, got %d", s.SpanKind)
	}
	if s.StartTime != now.UnixNano() {
		t.Errorf("startTime mismatch")
	}
	if s.EndTime != now.Add(42*time.Millisecond).UnixNano() {
		t.Errorf("endTime mismatch")
	}
	if s.StatusCode != "ERROR" {
		t.Errorf("expected statusCode 'ERROR', got %q", s.StatusCode)
	}
	if s.StatusMsg != "something broke" {
		t.Errorf("expected statusMessage 'something broke', got %q", s.StatusMsg)
	}
	if s.Attributes["http.method"] != "GET" {
		t.Errorf("expected http.method=GET, got %v", s.Attributes["http.method"])
	}
	// JSON numbers unmarshal as float64
	if v, ok := s.Attributes["http.status"].(float64); !ok || v != 200 {
		t.Errorf("expected http.status=200, got %v", s.Attributes["http.status"])
	}
	if len(s.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(s.Events))
	}
	if s.Events[0].Name != "cache-miss" {
		t.Errorf("expected event name 'cache-miss', got %q", s.Events[0].Name)
	}
	if s.Events[0].Attributes["key"] != "users" {
		t.Errorf("expected event attr key=users, got %v", s.Events[0].Attributes["key"])
	}
}

func TestHTTPExporter_BatchFlushAtBatchSize(t *testing.T) {
	srv, getReqs, waitForSpans := collectServer(t)

	batchSize := 5
	e := NewHTTPExporter(srv.URL, WithBatchSize(batchSize), WithFlushInterval(time.Hour))
	defer e.Shutdown(context.Background())

	for i := range batchSize {
		e.ExportSpan(context.Background(), makeTestSpan("span-"+string(rune('a'+i))))
	}

	waitForSpans(batchSize)

	reqs := getReqs()
	if len(reqs) != 1 {
		t.Fatalf("expected exactly 1 batch request, got %d", len(reqs))
	}
	if len(reqs[0].Spans) != batchSize {
		t.Errorf("expected %d spans in batch, got %d", batchSize, len(reqs[0].Spans))
	}
}

func TestHTTPExporter_TimerFlushSendsPartialBatch(t *testing.T) {
	srv, getReqs, waitForSpans := collectServer(t)

	e := NewHTTPExporter(srv.URL, WithBatchSize(100), WithFlushInterval(50*time.Millisecond))
	defer e.Shutdown(context.Background())

	e.ExportSpan(context.Background(), makeTestSpan("timer-span"))

	waitForSpans(1)

	reqs := getReqs()
	if len(reqs) == 0 {
		t.Fatal("expected flush from timer")
	}
	total := 0
	for _, r := range reqs {
		total += len(r.Spans)
	}
	if total != 1 {
		t.Errorf("expected 1 span total, got %d", total)
	}
}

func TestHTTPExporter_ShutdownFlushesRemaining(t *testing.T) {
	srv, getReqs, _ := collectServer(t)

	// Large batch size and long interval so nothing flushes automatically.
	e := NewHTTPExporter(srv.URL, WithBatchSize(1000), WithFlushInterval(time.Hour))

	for range 3 {
		e.ExportSpan(context.Background(), makeTestSpan("shutdown-span"))
	}

	// Small sleep to let spans land in the channel.
	time.Sleep(10 * time.Millisecond)

	err := e.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	reqs := getReqs()
	total := 0
	for _, r := range reqs {
		total += len(r.Spans)
	}
	if total != 3 {
		t.Errorf("expected 3 spans flushed on shutdown, got %d", total)
	}
}

func TestHTTPExporter_DropsSpansWhenBufferFull(t *testing.T) {
	// Create exporter with a tiny buffer (channel size is hardcoded to defaultBufferSize,
	// but we can observe the drop behavior by blocking the consumer).
	// Instead, we directly test with a server that never reads, making the channel fill up.

	// Use a server that hangs to block flushes.
	var blocking atomic.Bool
	blocking.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body to avoid connection resets.
		io.ReadAll(r.Body)
		for blocking.Load() {
			time.Sleep(10 * time.Millisecond)
		}
		w.WriteHeader(202)
	}))
	t.Cleanup(srv.Close)

	// Use batch size 1 so every span triggers a flush (which blocks).
	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))

	// Fill the buffer. The first span will trigger a flush that blocks.
	// Subsequent spans fill the channel (4096 buffer). After that, drops.
	for range defaultBufferSize + 100 {
		e.ExportSpan(context.Background(), makeTestSpan("flood"))
	}

	// Unblock and shut down -- just verify no panic.
	blocking.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e.Shutdown(ctx)
}

func TestHTTPExporter_HandlesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	t.Cleanup(srv.Close)

	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))

	e.ExportSpan(context.Background(), makeTestSpan("error-span"))

	// Give it time to flush and handle the error.
	time.Sleep(50 * time.Millisecond)

	// Should not panic. Shutdown should work cleanly.
	err := e.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestHTTPExporter_HandlesConnectionError(t *testing.T) {
	// Point to an endpoint that doesn't exist.
	e := NewHTTPExporter("http://127.0.0.1:1", WithBatchSize(1), WithFlushInterval(time.Hour))

	e.ExportSpan(context.Background(), makeTestSpan("unreachable-span"))

	// Give it time to attempt and fail.
	time.Sleep(50 * time.Millisecond)

	// Should not panic.
	err := e.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

func TestHTTPExporter_StatusConversion(t *testing.T) {
	srv, getReqs, waitForSpans := collectServer(t)

	e := NewHTTPExporter(srv.URL, WithBatchSize(3), WithFlushInterval(time.Hour))
	defer e.Shutdown(context.Background())

	statuses := []SpanStatus{StatusUnset, StatusOK, StatusError}
	expected := []string{"UNSET", "OK", "ERROR"}

	for _, s := range statuses {
		span := makeTestSpan("status-test")
		span.Status = s
		e.ExportSpan(context.Background(), span)
	}

	waitForSpans(3)

	reqs := getReqs()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	for i, s := range reqs[0].Spans {
		if s.StatusCode != expected[i] {
			t.Errorf("span %d: expected statusCode %q, got %q", i, expected[i], s.StatusCode)
		}
	}
}

func TestHTTPExporter_SpanKindHardcodedInternal(t *testing.T) {
	srv, getReqs, waitForSpans := collectServer(t)

	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))
	defer e.Shutdown(context.Background())

	e.ExportSpan(context.Background(), makeTestSpan("kind-test"))
	waitForSpans(1)

	reqs := getReqs()
	if reqs[0].Spans[0].SpanKind != 1 {
		t.Errorf("expected spanKind 1, got %d", reqs[0].Spans[0].SpanKind)
	}
}

func TestHTTPExporter_ZeroParentIDOmitted(t *testing.T) {
	var mu sync.Mutex
	var rawBodies [][]byte
	got := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		rawBodies = append(rawBodies, body)
		mu.Unlock()
		select {
		case got <- struct{}{}:
		default:
		}
		w.WriteHeader(202)
	}))
	t.Cleanup(srv.Close)

	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))
	defer e.Shutdown(context.Background())

	// Root span: zero ParentID.
	span := makeTestSpan("root-span")
	span.ParentID = SpanID{} // explicitly zero
	e.ExportSpan(context.Background(), span)

	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for request")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(rawBodies) == 0 {
		t.Fatal("no requests received")
	}

	// The raw JSON should NOT contain "parentSpanId" due to omitempty.
	raw := string(rawBodies[0])
	if containsField(raw, "parentSpanId") {
		t.Errorf("expected parentSpanId to be omitted for zero ParentID, got: %s", raw)
	}
}

func TestHTTPExporter_NonZeroParentIDIncluded(t *testing.T) {
	var mu sync.Mutex
	var rawBodies [][]byte
	got := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		rawBodies = append(rawBodies, body)
		mu.Unlock()
		select {
		case got <- struct{}{}:
		default:
		}
		w.WriteHeader(202)
	}))
	t.Cleanup(srv.Close)

	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))
	defer e.Shutdown(context.Background())

	span := makeTestSpan("child-span")
	span.ParentID = newSpanID() // non-zero
	e.ExportSpan(context.Background(), span)

	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for request")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(rawBodies) == 0 {
		t.Fatal("no requests received")
	}

	raw := string(rawBodies[0])
	if !containsField(raw, "parentSpanId") {
		t.Errorf("expected parentSpanId to be present for non-zero ParentID, got: %s", raw)
	}
}

func TestHTTPExporter_ShutdownRespectsContext(t *testing.T) {
	// Server that blocks until unblock channel is closed.
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		<-unblock
		w.WriteHeader(202)
	}))
	t.Cleanup(func() {
		close(unblock) // unblock any pending handler so srv.Close() can complete
		srv.Close()
	})

	e := NewHTTPExporter(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))

	e.ExportSpan(context.Background(), makeTestSpan("blocked"))

	// Give it time to start the blocking flush.
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := e.Shutdown(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

// containsField checks if a JSON string contains a given field name.
func containsField(jsonStr, field string) bool {
	// Simple check: look for "field": in the JSON.
	return len(jsonStr) > 0 && (contains(jsonStr, `"`+field+`":`) || contains(jsonStr, `"`+field+`" :`))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
