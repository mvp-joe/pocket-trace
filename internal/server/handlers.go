package server

import (
	"encoding/json"
	"strconv"
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

// ListServices handles GET /api/services.
func (h *Handlers) ListServices(c fiber.Ctx) error {
	services, err := h.Store.ListServices(c.Context())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(APIResponse[[]store.ServiceSummary]{Data: services})
}

// SearchTraces handles GET /api/traces.
func (h *Handlers) SearchTraces(c fiber.Ctx) error {
	var q store.TraceQuery
	q.ServiceName = c.Query("service")
	q.SpanName = c.Query("spanName")

	if v := c.Query("minDuration"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid minDuration: "+err.Error())
		}
		q.MinDuration = n
	}
	if v := c.Query("maxDuration"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid maxDuration: "+err.Error())
		}
		q.MaxDuration = n
	}
	if v := c.Query("start"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid start: "+err.Error())
		}
		q.Start = time.Unix(0, n)
	}
	if v := c.Query("end"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid end: "+err.Error())
		}
		q.End = time.Unix(0, n)
	}

	limitStr := c.Query("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid limit: "+err.Error())
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q.Limit = limit

	traces, err := h.Store.SearchTraces(c.Context(), q)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(APIResponse[[]store.TraceSummary]{Data: traces})
}

// GetTrace handles GET /api/traces/:traceID.
func (h *Handlers) GetTrace(c fiber.Ctx) error {
	traceID := c.Params("traceID")

	detail, err := h.Store.GetTrace(c.Context(), traceID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if detail == nil {
		return fiber.NewError(fiber.StatusNotFound, "trace not found")
	}
	return c.JSON(APIResponse[*store.TraceDetail]{Data: detail})
}

// GetSpan handles GET /api/traces/:traceID/spans/:spanID.
func (h *Handlers) GetSpan(c fiber.Ctx) error {
	traceID := c.Params("traceID")
	spanID := c.Params("spanID")

	span, err := h.Store.GetSpan(c.Context(), traceID, spanID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if span == nil {
		return fiber.NewError(fiber.StatusNotFound, "span not found")
	}
	return c.JSON(APIResponse[*store.Span]{Data: span})
}

// GetDependencies handles GET /api/dependencies.
func (h *Handlers) GetDependencies(c fiber.Ctx) error {
	lookbackStr := c.Query("lookback", "1h")
	lookback, err := time.ParseDuration(lookbackStr)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid lookback duration: "+err.Error())
	}

	since := time.Now().Add(-lookback)
	deps, err := h.Store.GetDependencies(c.Context(), since)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(APIResponse[[]store.Dependency]{Data: deps})
}

// Status handles GET /api/status.
func (h *Handlers) Status(c fiber.Ctx) error {
	stats, err := h.Store.Stats(c.Context())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(APIResponse[StatusResponse]{
		Data: StatusResponse{
			Version: h.Version,
			Uptime:  time.Since(h.StartTime).Round(time.Second).String(),
			DB:      *stats,
		},
	})
}

// Purge handles POST /api/purge.
func (h *Handlers) Purge(c fiber.Ctx) error {
	olderThanStr := c.Query("olderThan")
	if olderThanStr == "" {
		return fiber.NewError(fiber.StatusBadRequest, "olderThan query parameter is required")
	}

	d, err := time.ParseDuration(olderThanStr)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid olderThan duration: "+err.Error())
	}

	before := time.Now().Add(-d)
	deleted, err := h.Store.PurgeOlderThan(c.Context(), before)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(APIResponse[PurgeResult]{Data: PurgeResult{Deleted: deleted}})
}
