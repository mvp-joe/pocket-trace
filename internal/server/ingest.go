package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"pocket-trace/internal/store"
)

// IngestRequest is the JSON body sent by the library exporter.
type IngestRequest struct {
	ServiceName string       `json:"serviceName"`
	Spans       []IngestSpan `json:"spans"`
}

// IngestSpan is a single span in the ingest payload.
type IngestSpan struct {
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
	Events       []IngestEvent  `json:"events,omitempty"`
}

// IngestEvent is a span event in the ingest payload.
type IngestEvent struct {
	Name       string         `json:"name"`
	Time       int64          `json:"time"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// IngestResult is returned by POST /api/ingest.
type IngestResult struct {
	Accepted int `json:"accepted"`
}

// SpanBuffer accumulates spans in memory and batch-writes them to the store.
type SpanBuffer struct {
	store         *store.Store
	spans         chan []store.Span
	batchSize     int
	flushInterval time.Duration
	stop          chan struct{}
	wg            sync.WaitGroup
}

// NewSpanBuffer creates a SpanBuffer and starts its background flush goroutine.
func NewSpanBuffer(s *store.Store, batchSize int, flushInterval time.Duration) *SpanBuffer {
	b := &SpanBuffer{
		store:         s,
		spans:         make(chan []store.Span, batchSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		stop:          make(chan struct{}),
	}
	b.wg.Add(1)
	go b.run()
	return b
}

// Add pushes a batch of spans into the buffer. Non-blocking; drops spans if
// the channel is full.
func (b *SpanBuffer) Add(spans []store.Span) {
	select {
	case b.spans <- spans:
	default:
		slog.Warn("span buffer full, dropping spans", "count", len(spans))
	}
}

// Shutdown signals the background goroutine to drain remaining spans and flush,
// then blocks until complete.
func (b *SpanBuffer) Shutdown() {
	close(b.stop)
	b.wg.Wait()
}

// run is the background goroutine that collects spans and flushes to the store.
func (b *SpanBuffer) run() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	batch := make([]store.Span, 0, b.batchSize)

	for {
		select {
		case spans := <-b.spans:
			batch = append(batch, spans...)
			if len(batch) >= b.batchSize {
				b.flush(batch)
				batch = make([]store.Span, 0, b.batchSize)
			}

		case <-ticker.C:
			if len(batch) > 0 {
				b.flush(batch)
				batch = make([]store.Span, 0, b.batchSize)
			}

		case <-b.stop:
			// Drain remaining spans from the channel.
			for {
				select {
				case spans := <-b.spans:
					batch = append(batch, spans...)
				default:
					if len(batch) > 0 {
						b.flush(batch)
					}
					return
				}
			}
		}
	}
}

// flush writes a batch of spans to the store.
func (b *SpanBuffer) flush(batch []store.Span) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.store.InsertSpans(ctx, batch); err != nil {
		slog.Error("failed to flush spans to store", "error", err, "count", len(batch))
	}
}
