package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	defaultBatchSize     = 256
	defaultFlushInterval = 2 * time.Second
	defaultBufferSize    = 4096
)

// HTTPExporter sends finished spans to the pocket-trace daemon via JSON HTTP POST.
// It implements the Exporter interface from trace.go.
type HTTPExporter struct {
	endpoint      string
	client        *http.Client
	spans         chan *FinishedSpan
	stop          chan struct{}
	wg            sync.WaitGroup
	batchSize     int
	flushInterval time.Duration
}

// ExporterOption configures the HTTPExporter.
type ExporterOption func(*HTTPExporter)

// WithBatchSize sets the number of spans that trigger an immediate flush.
func WithBatchSize(n int) ExporterOption {
	return func(e *HTTPExporter) {
		if n > 0 {
			e.batchSize = n
		}
	}
}

// WithFlushInterval sets the maximum time between flushes.
func WithFlushInterval(d time.Duration) ExporterOption {
	return func(e *HTTPExporter) {
		if d > 0 {
			e.flushInterval = d
		}
	}
}

// NewHTTPExporter creates a new HTTPExporter that sends spans to the given endpoint.
// The background goroutine starts immediately.
func NewHTTPExporter(endpoint string, opts ...ExporterOption) *HTTPExporter {
	e := &HTTPExporter{
		endpoint:      endpoint,
		client:        &http.Client{Timeout: 10 * time.Second},
		spans:         make(chan *FinishedSpan, defaultBufferSize),
		stop:          make(chan struct{}),
		batchSize:     defaultBatchSize,
		flushInterval: defaultFlushInterval,
	}
	for _, opt := range opts {
		opt(e)
	}
	e.wg.Add(1)
	go e.run()
	return e
}

// ExportSpan queues a span for export. Non-blocking; drops the span if the buffer is full.
func (e *HTTPExporter) ExportSpan(_ context.Context, span *FinishedSpan) {
	select {
	case e.spans <- span:
	default:
		slog.Warn("pocket-trace: export buffer full, dropping span",
			"span", span.Name,
			"trace_id", span.TraceID,
		)
	}
}

// Shutdown signals the background goroutine to drain remaining spans and flush.
// It blocks until all spans are flushed or the context is cancelled.
func (e *HTTPExporter) Shutdown(ctx context.Context) error {
	close(e.stop)

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run is the background goroutine that batches and flushes spans.
func (e *HTTPExporter) run() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.flushInterval)
	defer ticker.Stop()

	batch := make([]*FinishedSpan, 0, e.batchSize)

	for {
		select {
		case span := <-e.spans:
			batch = append(batch, span)
			if len(batch) >= e.batchSize {
				e.flush(batch)
				batch = make([]*FinishedSpan, 0, e.batchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				e.flush(batch)
				batch = make([]*FinishedSpan, 0, e.batchSize)
			}

		case <-e.stop:
			// Drain remaining spans from the channel.
			for {
				select {
				case span := <-e.spans:
					batch = append(batch, span)
				default:
					if len(batch) > 0 {
						e.flush(batch)
					}
					return
				}
			}
		}
	}
}

// flush converts a batch of FinishedSpans to an IngestRequest and POSTs it.
func (e *HTTPExporter) flush(batch []*FinishedSpan) {
	spans := make([]ingestSpan, len(batch))
	for i, fs := range batch {
		spans[i] = convertSpan(fs)
	}

	req := ingestRequest{
		ServiceName: ServiceName(),
		Spans:       spans,
	}

	body, err := json.Marshal(req)
	if err != nil {
		slog.Error("pocket-trace: failed to marshal spans", "error", err)
		return
	}

	url := e.endpoint + "/api/ingest"
	resp, err := e.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("pocket-trace: failed to send spans", "error", err, "endpoint", url)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("pocket-trace: ingest endpoint returned error",
			"status", resp.StatusCode,
			"endpoint", url,
		)
	}
}

// convertSpan converts a FinishedSpan to the JSON ingest format.
func convertSpan(fs *FinishedSpan) ingestSpan {
	s := ingestSpan{
		TraceID:    fs.TraceID.String(),
		SpanID:     fs.SpanID.String(),
		Name:       fs.Name,
		SpanKind:   1, // internal
		StartTime:  fs.Start.UnixNano(),
		EndTime:    fs.End.UnixNano(),
		StatusCode: statusString(fs.Status),
		StatusMsg:  fs.StatusMsg,
		Attributes: convertAttrs(fs.Attrs),
		Events:     convertEvents(fs.Events),
	}

	if !fs.ParentID.IsZero() {
		s.ParentSpanID = fs.ParentID.String()
	}

	return s
}

// statusString converts a SpanStatus enum to its string representation.
func statusString(s SpanStatus) string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusError:
		return "ERROR"
	default:
		return "UNSET"
	}
}

// convertAttrs converts []Attr to map[string]any for JSON.
func convertAttrs(attrs []Attr) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value
	}
	return m
}

// convertEvents converts []SpanEvent to []ingestEvent for JSON.
func convertEvents(events []SpanEvent) []ingestEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]ingestEvent, len(events))
	for i, ev := range events {
		out[i] = ingestEvent{
			Name:       ev.Name,
			Time:       ev.Time.UnixNano(),
			Attributes: convertAttrs(ev.Attrs),
		}
	}
	return out
}

// --- JSON types for the ingest API (unexported, only used by flush) ---

type ingestRequest struct {
	ServiceName string       `json:"serviceName"`
	Spans       []ingestSpan `json:"spans"`
}

type ingestSpan struct {
	TraceID      string         `json:"traceId"`
	SpanID       string         `json:"spanId"`
	ParentSpanID string         `json:"parentSpanId,omitempty"`
	Name         string         `json:"name"`
	SpanKind     int            `json:"spanKind"`
	StartTime    int64          `json:"startTime"`
	EndTime      int64          `json:"endTime"`
	StatusCode   string         `json:"statusCode"`
	StatusMsg    string         `json:"statusMessage,omitempty"`
	Attributes   map[string]any `json:"attributes,omitempty"`
	Events       []ingestEvent  `json:"events,omitempty"`
}

func (s ingestSpan) String() string {
	return fmt.Sprintf("%s(%s)", s.Name, s.SpanID)
}

type ingestEvent struct {
	Name       string         `json:"name"`
	Time       int64          `json:"time"`
	Attributes map[string]any `json:"attributes,omitempty"`
}
