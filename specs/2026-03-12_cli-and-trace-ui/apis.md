# REST API Endpoints

All endpoints are served by the pocket-trace daemon on the configured listen address (default `:7070`).

## Ingest API

### POST /api/ingest

Accepts a batch of spans from the library exporter.

- **Request Body:** `IngestRequest` (see interface.md)
- **Response:** `APIResponse[IngestResult]` (see interface.md)
- **Success:** 202 Accepted
- **Errors:**
  - 400 -- malformed JSON or missing required fields
  - 503 -- buffer full (span channel at capacity)

The handler converts `IngestSpan` entries to `store.Span` (serializing attributes/events to JSON, converting empty `ParentSpanID` to SQL NULL for root spans), then pushes them into the `SpanBuffer`. Response is returned immediately -- writes happen asynchronously.

---

## Query API

### GET /api/services

Lists all services that have sent spans, with aggregated stats.

- **Query Params:** none
- **Response:** `APIResponse[[]ServiceSummary]`
- **Success:** 200

Services are derived from `SELECT DISTINCT service_name` with count and max start_time aggregation.

---

### GET /api/traces

Searches for traces matching filter criteria. Returns summary-level data (not full span trees).

- **Query Params:**
  - `service` (string, optional) -- filter by service name
  - `spanName` (string, optional) -- filter by span name (substring match)
  - `minDuration` (int, optional) -- minimum duration in ms
  - `maxDuration` (int, optional) -- maximum duration in ms
  - `start` (int64, optional) -- start of time range, unix nanos
  - `end` (int64, optional) -- end of time range, unix nanos
  - `limit` (int, optional) -- max results, default 20, max 100
- **Response:** `APIResponse[[]TraceSummary]`
- **Success:** 200

The query groups spans by trace_id, computing root span name, total duration, span count, and error count. Results are ordered by start_time descending (newest first).

---

### GET /api/traces/:traceID

Returns a full trace as a pre-built tree structure. The tree is built server-side so consumers (UI and LLMs) receive spans with children already nested.

- **Path Params:**
  - `traceID` (string) -- hex-encoded trace ID
- **Response:** `APIResponse[TraceDetail]` (see interface.md for `TraceDetail` and `SpanNode` types)
- **Success:** 200
- **Errors:**
  - 404 -- no spans found for trace ID

The response contains `roots` — an array of `SpanNode` trees (typically one root span, but may contain orphan spans promoted to roots when their parent is missing). Each `SpanNode` has the same fields as `Span` plus a `children: []SpanNode` array. Children and roots are sorted by `startTime`.

---

### GET /api/traces/:traceID/spans/:spanID

Returns a single span with full detail.

- **Path Params:**
  - `traceID` (string) -- hex-encoded trace ID
  - `spanID` (string) -- hex-encoded span ID
- **Response:** `APIResponse[Span]`
- **Success:** 200
- **Errors:**
  - 404 -- span not found

---

### GET /api/dependencies

Returns service-to-service call relationships for the dependency graph.

- **Query Params:**
  - `lookback` (string, optional) -- duration string (e.g. "1h", "24h"), default "1h"
- **Response:** `APIResponse[[]Dependency]`
- **Success:** 200

Dependencies are computed by joining spans: find spans where both the parent and child exist but have different service_name values. Group by (parent_service, child_service) with a call count.

---

### GET /api/status

Returns daemon health and database statistics.

- **Query Params:** none
- **Response:** `APIResponse[StatusResponse]`
- **Success:** 200

---

### POST /api/purge

Purges spans older than the specified duration. Used by the `purge` CLI command to avoid dual-writer SQLite issues.

- **Query Params:**
  - `olderThan` (string, required) -- duration string (e.g. "24h", "7d", "168h")
- **Response:** `APIResponse[PurgeResult]` (see interface.md)
- **Success:** 200
- **Errors:**
  - 400 -- invalid duration format

---

## Static UI

### GET /\*

All non-`/api` routes serve the embedded React application via `//go:embed ui/dist`. The Fiber static middleware serves `index.html` for any path that does not match a static asset, enabling client-side routing.
