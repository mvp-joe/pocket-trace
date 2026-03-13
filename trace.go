package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

// TraceID is a 16-byte unique identifier for a trace.
type TraceID [16]byte

// SpanID is an 8-byte unique identifier for a span.
type SpanID [8]byte

func (id TraceID) String() string { return hex.EncodeToString(id[:]) }
func (id SpanID) String() string  { return hex.EncodeToString(id[:]) }
func (id SpanID) IsZero() bool    { return id == SpanID{} }

// Attr is a key-value pair attached to a span or event.
type Attr struct {
	Key   string
	Value any
}

// SpanEvent is a point-in-time event recorded during a span.
type SpanEvent struct {
	Name  string
	Time  time.Time
	Attrs []Attr
}

// SpanStatus indicates the outcome of a span.
type SpanStatus int

const (
	StatusUnset SpanStatus = iota
	StatusOK
	StatusError
)

// FinishedSpan contains all data for a completed span, used by exporters.
type FinishedSpan struct {
	TraceID   TraceID
	SpanID    SpanID
	ParentID  SpanID
	Name      string
	Start     time.Time
	End       time.Time
	Attrs     []Attr
	Events    []SpanEvent
	Status    SpanStatus
	StatusMsg string
}

// Exporter receives completed spans for export to a backend.
type Exporter interface {
	ExportSpan(ctx context.Context, span *FinishedSpan)
	Shutdown(ctx context.Context) error
}

var (
	globalMu       sync.RWMutex
	globalExporter Exporter
	globalService  string = "unknown"
)

// SetExporter configures the global span exporter.
func SetExporter(e Exporter) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalExporter = e
}

// SetServiceName configures the service name used in exported traces.
func SetServiceName(name string) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalService = name
}

// ServiceName returns the configured service name.
func ServiceName() string {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalService
}

// Shutdown flushes the global exporter and releases resources.
func Shutdown(ctx context.Context) error {
	globalMu.RLock()
	exp := globalExporter
	globalMu.RUnlock()
	if exp != nil {
		return exp.Shutdown(ctx)
	}
	return nil
}

type ctxKey struct{}

// Span represents an in-progress traced operation.
type Span struct {
	traceID  TraceID
	spanID   SpanID
	parentID SpanID
	name     string
	start    time.Time
	attrs    []any
	events   []SpanEvent
	mu       sync.Mutex
	ctx      context.Context
}

// TraceID returns the span's trace ID.
func (s *Span) TraceID() TraceID { return s.traceID }

// SpanID returns the span's ID.
func (s *Span) SpanID() SpanID { return s.spanID }

// Start creates a new span, logs its entry, and returns the span with a child context.
func Start(ctx context.Context, name string, attrs ...any) (*Span, context.Context) {
	sid := newSpanID()
	var tid TraceID
	var pid SpanID

	if parent, ok := ctx.Value(ctxKey{}).(*Span); ok {
		tid = parent.traceID
		pid = parent.spanID
	} else {
		tid = newTraceID()
	}

	s := &Span{
		traceID:  tid,
		spanID:   sid,
		parentID: pid,
		name:     name,
		start:    time.Now(),
		attrs:    attrs,
		ctx:      ctx,
	}

	logAttrs := []any{"trace_id", tid, "span_id", sid}
	if !pid.IsZero() {
		logAttrs = append(logAttrs, "parent_id", pid)
	}
	logAttrs = append(logAttrs, attrs...)
	slog.InfoContext(ctx, "→ "+name, logAttrs...)

	return s, context.WithValue(ctx, ctxKey{}, s)
}

// Event logs a point-in-time event within the span.
func (s *Span) Event(name string, attrs ...any) {
	now := time.Now()

	s.mu.Lock()
	s.events = append(s.events, SpanEvent{
		Name:  name,
		Time:  now,
		Attrs: toAttrs(attrs),
	})
	s.mu.Unlock()

	logAttrs := []any{
		"trace_id", s.traceID,
		"span_id", s.spanID,
		"elapsed_ms", now.Sub(s.start).Milliseconds(),
	}
	if !s.parentID.IsZero() {
		logAttrs = append(logAttrs, "parent_id", s.parentID)
	}
	logAttrs = append(logAttrs, attrs...)
	slog.InfoContext(s.ctx, "• "+name, logAttrs...)
}

// End logs the span exit with duration.
func (s *Span) End() {
	s.end(nil)
}

// EndErr logs the span exit with duration and captures error state.
func (s *Span) EndErr(err *error) {
	s.end(err)
}

func (s *Span) end(errPtr *error) {
	endTime := time.Now()
	duration := endTime.Sub(s.start)

	logAttrs := []any{
		"trace_id", s.traceID,
		"span_id", s.spanID,
		"duration_ms", duration.Milliseconds(),
	}
	if !s.parentID.IsZero() {
		logAttrs = append(logAttrs, "parent_id", s.parentID)
	}

	lvl := slog.LevelInfo
	msg := "← " + s.name
	status := StatusOK
	var statusMsg string

	if errPtr != nil && *errPtr != nil {
		lvl = slog.LevelError
		status = StatusError
		statusMsg = (*errPtr).Error()
		logAttrs = append(logAttrs, "error", statusMsg)
	}

	slog.Log(s.ctx, lvl, msg, logAttrs...)

	globalMu.RLock()
	exp := globalExporter
	globalMu.RUnlock()

	if exp != nil {
		s.mu.Lock()
		events := make([]SpanEvent, len(s.events))
		copy(events, s.events)
		s.mu.Unlock()

		exp.ExportSpan(s.ctx, &FinishedSpan{
			TraceID:   s.traceID,
			SpanID:    s.spanID,
			ParentID:  s.parentID,
			Name:      s.name,
			Start:     s.start,
			End:       endTime,
			Attrs:     toAttrs(s.attrs),
			Events:    events,
			Status:    status,
			StatusMsg: statusMsg,
		})
	}
}

func newTraceID() TraceID {
	var id TraceID
	_, _ = rand.Read(id[:])
	return id
}

func newSpanID() SpanID {
	var id SpanID
	_, _ = rand.Read(id[:])
	return id
}

func toAttrs(kvs []any) []Attr {
	var attrs []Attr
	for i := 0; i+1 < len(kvs); i += 2 {
		key, ok := kvs[i].(string)
		if !ok {
			continue
		}
		attrs = append(attrs, Attr{Key: key, Value: kvs[i+1]})
	}
	return attrs
}
