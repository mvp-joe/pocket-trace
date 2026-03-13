package otlp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	trace "pocket-trace"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestExporter_SendsOTLP(t *testing.T) {
	var mu sync.Mutex
	var gotBody []byte
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		// Strip gRPC frame header (5 bytes: 1 byte flag + 4 byte length).
		if len(body) >= 5 {
			gotBody = body[5:]
		}
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	trace.SetServiceName("test-svc")
	defer trace.SetServiceName("unknown")

	exp := NewExporter(srv.URL, WithFlushInterval(50*time.Millisecond), WithHTTPClient(srv.Client()))

	span := &trace.FinishedSpan{
		TraceID:   trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:    trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		Name:      "test-span",
		Start:     time.Now().Add(-100 * time.Millisecond),
		End:       time.Now(),
		Status:    trace.StatusOK,
		Attrs:     []trace.Attr{{Key: "k", Value: "v"}},
		Events:    []trace.SpanEvent{{Name: "ev1", Time: time.Now(), Attrs: []trace.Attr{{Key: "ek", Value: "ev"}}}},
	}

	exp.ExportSpan(context.Background(), span)

	// Wait for flush.
	time.Sleep(200 * time.Millisecond)
	exp.Shutdown(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if gotContentType != "application/grpc" {
		t.Errorf("expected content-type application/grpc, got %q", gotContentType)
	}

	if len(gotBody) == 0 {
		t.Fatal("expected non-empty body")
	}

	var td tracepb.TracesData
	if err := proto.Unmarshal(gotBody, &td); err != nil {
		t.Fatalf("failed to unmarshal OTLP body: %v", err)
	}

	if len(td.ResourceSpans) != 1 {
		t.Fatalf("expected 1 ResourceSpans, got %d", len(td.ResourceSpans))
	}

	rs := td.ResourceSpans[0]

	// Check service name.
	found := false
	for _, attr := range rs.Resource.Attributes {
		if attr.Key == "service.name" {
			if attr.Value.GetStringValue() != "test-svc" {
				t.Errorf("expected service.name 'test-svc', got %q", attr.Value.GetStringValue())
			}
			found = true
		}
	}
	if !found {
		t.Error("expected service.name attribute in resource")
	}

	if len(rs.ScopeSpans) != 1 {
		t.Fatalf("expected 1 ScopeSpans, got %d", len(rs.ScopeSpans))
	}

	ss := rs.ScopeSpans[0]
	if ss.Scope.Name != "pocket-trace" {
		t.Errorf("expected scope name 'pocket-trace', got %q", ss.Scope.Name)
	}

	if len(ss.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(ss.Spans))
	}

	s := ss.Spans[0]
	if s.Name != "test-span" {
		t.Errorf("expected span name 'test-span', got %q", s.Name)
	}
	if s.Status.Code != tracepb.Status_STATUS_CODE_OK {
		t.Errorf("expected OK status, got %v", s.Status.Code)
	}
	if len(s.Attributes) != 1 || s.Attributes[0].Key != "k" {
		t.Errorf("expected attrs [{k v}], got %v", s.Attributes)
	}
	if len(s.Events) != 1 || s.Events[0].Name != "ev1" {
		t.Errorf("expected 1 event 'ev1', got %v", s.Events)
	}
}

func TestExporter_ShutdownFlushesRemaining(t *testing.T) {
	var mu sync.Mutex
	var totalSpans int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Strip gRPC frame header (5 bytes).
		if len(body) >= 5 {
			body = body[5:]
		}
		var td tracepb.TracesData
		if err := proto.Unmarshal(body, &td); err == nil {
			for _, rs := range td.ResourceSpans {
				for _, ss := range rs.ScopeSpans {
					mu.Lock()
					totalSpans += len(ss.Spans)
					mu.Unlock()
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use a long flush interval so only shutdown triggers the flush.
	exp := NewExporter(srv.URL, WithFlushInterval(10*time.Minute), WithHTTPClient(srv.Client()))

	for i := 0; i < 5; i++ {
		exp.ExportSpan(context.Background(), &trace.FinishedSpan{
			TraceID: trace.TraceID{byte(i)},
			SpanID:  trace.SpanID{byte(i)},
			Name:    "span",
			Start:   time.Now(),
			End:     time.Now(),
		})
	}

	exp.Shutdown(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if totalSpans != 5 {
		t.Errorf("expected 5 spans flushed on shutdown, got %d", totalSpans)
	}
}

func TestConvertSpan_ParentID(t *testing.T) {
	parent := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}

	withParent := convertSpan(&trace.FinishedSpan{
		TraceID:  trace.TraceID{},
		SpanID:   trace.SpanID{9, 10, 11, 12, 13, 14, 15, 16},
		ParentID: parent,
		Name:     "child",
		Start:    time.Now(),
		End:      time.Now(),
	})
	if len(withParent.ParentSpanId) != 8 {
		t.Errorf("expected 8-byte parent span ID, got %d bytes", len(withParent.ParentSpanId))
	}

	withoutParent := convertSpan(&trace.FinishedSpan{
		Name:  "root",
		Start: time.Now(),
		End:   time.Now(),
	})
	if len(withoutParent.ParentSpanId) != 0 {
		t.Errorf("expected empty parent span ID for root span, got %d bytes", len(withoutParent.ParentSpanId))
	}
}

func TestConvertValue_Types(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{"hello", "string_value:\"hello\""},
		{42, "int_value:42"},
		{int64(99), "int_value:99"},
		{3.14, "double_value:3.14"},
		{true, "bool_value:true"},
		{[]int{1, 2}, "string_value:\"[1 2]\""},
	}

	for _, tt := range tests {
		v := convertValue(tt.in)
		got := v.GetValue()
		if got == nil {
			t.Errorf("convertValue(%v) returned nil value", tt.in)
		}
	}
}
