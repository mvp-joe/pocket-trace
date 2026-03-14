package server

import (
	"encoding/json"
	"time"

	"pocket-trace/internal/store"

	"github.com/gofiber/fiber/v3"
)

// APIResponse wraps all API responses.
type APIResponse[T any] struct {
	Data  T      `json:"data"`
	Error string `json:"error,omitempty"`
}

// PurgeResult is returned by POST /api/purge.
type PurgeResult struct {
	Deleted int64 `json:"deleted"`
}

// StatusResponse reports daemon health.
type StatusResponse struct {
	Version string        `json:"version"`
	Uptime  string        `json:"uptime"`
	DB      store.DBStats `json:"db"`
}

// Handlers holds dependencies for HTTP handler methods.
type Handlers struct {
	Store     *store.Store
	Buffer    *SpanBuffer
	StartTime time.Time
	Version   string
}

// Ingest handles POST /api/ingest.
// It parses the JSON body, converts IngestSpan entries to store.Span,
// pushes them into the SpanBuffer, and returns 202 Accepted.
func (h *Handlers) Ingest(c fiber.Ctx) error {
	var req IngestRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}

	if len(req.Spans) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "no spans provided")
	}

	spans := make([]store.Span, len(req.Spans))
	for i, is := range req.Spans {
		if is.TraceID == "" || is.SpanID == "" {
			return fiber.NewError(fiber.StatusBadRequest, "span missing traceId or spanId")
		}

		span := store.Span{
			TraceID:      is.TraceID,
			SpanID:       is.SpanID,
			ParentSpanID: is.ParentSpanID, // empty string -> store converts to NULL
			ServiceName:  req.ServiceName,
			SpanName:     is.Name,
			SpanKind:     is.SpanKind,
			StartTime:    is.StartTime,
			EndTime:      is.EndTime,
			DurationMs:   (is.EndTime - is.StartTime) / 1e6,
			StatusCode:   is.StatusCode,
			StatusMsg:    is.StatusMsg,
		}

		// Convert attributes map to json.RawMessage.
		if len(is.Attributes) > 0 {
			b, err := json.Marshal(is.Attributes)
			if err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "invalid attributes: "+err.Error())
			}
			span.Attributes = b
		}

		// Convert events to json.RawMessage.
		if len(is.Events) > 0 {
			b, err := json.Marshal(is.Events)
			if err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "invalid events: "+err.Error())
			}
			span.Events = b
		}

		spans[i] = span
	}

	h.Buffer.Add(spans)

	return c.Status(fiber.StatusAccepted).JSON(APIResponse[IngestResult]{
		Data: IngestResult{Accepted: len(spans)},
	})
}
