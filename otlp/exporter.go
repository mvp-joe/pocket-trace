package otlp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	trace "pocket-trace"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/proto"
)

// Exporter sends finished spans to an OTLP-compatible HTTP endpoint (e.g. Quickwit).
type Exporter struct {
	endpoint string
	client   *http.Client
	spans    chan *trace.FinishedSpan
	stop     chan struct{}
	wg       sync.WaitGroup

	batchSize    int
	flushInterval time.Duration
}

// Option configures the exporter.
type Option func(*Exporter)

// WithBatchSize sets the max batch size before flushing (default 256).
func WithBatchSize(n int) Option {
	return func(e *Exporter) { e.batchSize = n }
}

// WithFlushInterval sets the flush interval (default 2s).
func WithFlushInterval(d time.Duration) Option {
	return func(e *Exporter) { e.flushInterval = d }
}

// WithHTTPClient sets a custom HTTP client for the exporter.
func WithHTTPClient(c *http.Client) Option {
	return func(e *Exporter) { e.client = c }
}

// NewExporter creates an OTLP HTTP exporter that batches and sends spans.
// Endpoint should be the OTLP base URL, e.g. "http://localhost:7281".
// For plain HTTP endpoints (like Quickwit's gRPC/OTLP port), the exporter
// automatically uses HTTP/2 cleartext (h2c).
func NewExporter(endpoint string, opts ...Option) *Exporter {
	e := &Exporter{
		endpoint:      endpoint,
		client:        newHTTPClient(endpoint),
		spans:         make(chan *trace.FinishedSpan, 4096),
		stop:          make(chan struct{}),
		batchSize:     256,
		flushInterval: 2 * time.Second,
	}
	for _, o := range opts {
		o(e)
	}
	e.wg.Add(1)
	go e.run()
	return e
}

// ExportSpan queues a span for export. Non-blocking; drops spans if the buffer is full.
func (e *Exporter) ExportSpan(_ context.Context, span *trace.FinishedSpan) {
	select {
	case e.spans <- span:
	default:
		slog.Warn("pocket-trace: export buffer full, dropping span", "span", span.Name)
	}
}

// Shutdown flushes remaining spans and stops the exporter.
func (e *Exporter) Shutdown(_ context.Context) error {
	close(e.stop)
	e.wg.Wait()
	return nil
}

func (e *Exporter) run() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.flushInterval)
	defer ticker.Stop()

	var batch []*trace.FinishedSpan

	for {
		select {
		case span := <-e.spans:
			batch = append(batch, span)
			if len(batch) >= e.batchSize {
				e.flush(batch)
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				e.flush(batch)
				batch = nil
			}
		case <-e.stop:
			// Drain remaining spans from channel.
			for {
				select {
				case span := <-e.spans:
					batch = append(batch, span)
				default:
					goto done
				}
			}
		done:
			if len(batch) > 0 {
				e.flush(batch)
			}
			return
		}
	}
}

func (e *Exporter) flush(spans []*trace.FinishedSpan) {
	service := trace.ServiceName()

	otlpSpans := make([]*tracepb.Span, len(spans))
	for i, s := range spans {
		otlpSpans[i] = convertSpan(s)
	}

	// TracesData is wire-compatible with ExportTraceServiceRequest
	// (same field at position 1) — avoids pulling in the gRPC collector dependency.
	req := &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{{
					Key:   "service.name",
					Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: service}},
				}},
			},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: &commonpb.InstrumentationScope{
					Name:    "pocket-trace",
					Version: "0.1.0",
				},
				Spans: otlpSpans,
			}},
		}},
	}

	data, err := proto.Marshal(req)
	if err != nil {
		slog.Error("pocket-trace: failed to marshal OTLP request", "error", err)
		return
	}

	// gRPC framing: 1 byte compressed flag (0) + 4 byte big-endian message length + message.
	var frame bytes.Buffer
	frame.WriteByte(0) // not compressed
	binary.Write(&frame, binary.BigEndian, uint32(len(data)))
	frame.Write(data)

	grpcPath := "/opentelemetry.proto.collector.trace.v1.TraceService/Export"
	httpReq, err := http.NewRequest("POST", e.endpoint+grpcPath, &frame)
	if err != nil {
		slog.Error("pocket-trace: failed to create request", "error", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/grpc")
	httpReq.Header.Set("TE", "trailers")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		slog.Error("pocket-trace: failed to send traces", "error", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	grpcStatus := resp.Header.Get("Grpc-Status")
	if resp.StatusCode != http.StatusOK {
		slog.Error("pocket-trace: OTLP export failed", "status", resp.StatusCode)
	} else if grpcStatus != "" && grpcStatus != "0" {
		grpcMsg := resp.Header.Get("Grpc-Message")
		slog.Error("pocket-trace: OTLP gRPC error", "grpc_status", grpcStatus, "message", grpcMsg)
	}
}

func convertSpan(s *trace.FinishedSpan) *tracepb.Span {
	span := &tracepb.Span{
		TraceId:           s.TraceID[:],
		SpanId:            s.SpanID[:],
		Name:              s.Name,
		Kind:              tracepb.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(s.Start.UnixNano()),
		EndTimeUnixNano:   uint64(s.End.UnixNano()),
		Attributes:        convertAttrs(s.Attrs),
		Status:            convertStatus(s.Status, s.StatusMsg),
	}

	if !s.ParentID.IsZero() {
		span.ParentSpanId = s.ParentID[:]
	}

	for _, ev := range s.Events {
		span.Events = append(span.Events, &tracepb.Span_Event{
			TimeUnixNano: uint64(ev.Time.UnixNano()),
			Name:         ev.Name,
			Attributes:   convertAttrs(ev.Attrs),
		})
	}

	return span
}

func convertStatus(status trace.SpanStatus, msg string) *tracepb.Status {
	s := &tracepb.Status{Message: msg}
	switch status {
	case trace.StatusOK:
		s.Code = tracepb.Status_STATUS_CODE_OK
	case trace.StatusError:
		s.Code = tracepb.Status_STATUS_CODE_ERROR
	default:
		s.Code = tracepb.Status_STATUS_CODE_UNSET
	}
	return s
}

func convertAttrs(attrs []trace.Attr) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	kvs := make([]*commonpb.KeyValue, len(attrs))
	for i, a := range attrs {
		kvs[i] = &commonpb.KeyValue{
			Key:   a.Key,
			Value: convertValue(a.Value),
		}
	}
	return kvs
}

// newHTTPClient creates an HTTP client appropriate for the endpoint scheme.
// Plain HTTP endpoints use h2c (HTTP/2 cleartext) since OTLP endpoints like
// Quickwit's port 7281 speak HTTP/2 (gRPC). HTTPS endpoints use the default
// transport which negotiates HTTP/2 via TLS ALPN.
func newHTTPClient(endpoint string) *http.Client {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "https" {
		return &http.Client{Timeout: 10 * time.Second}
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

func convertValue(v any) *commonpb.AnyValue {
	switch val := v.(type) {
	case string:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: val}}
	case int:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: int64(val)}}
	case int64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: val}}
	case float64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: val}}
	case bool:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: val}}
	default:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: fmt.Sprint(val)}}
	}
}
