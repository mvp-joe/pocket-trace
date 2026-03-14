# Test Specifications

## Unit Tests

### Store (`internal/store/`)

- **InsertSpans inserts and retrieves a span:** Insert a single span, query by trace ID, verify all fields match.
- **InsertSpans handles batch of 100 spans:** Insert 100 spans across 10 traces, verify correct counts per trace.
- **InsertSpans is idempotent (INSERT OR REPLACE):** Insert a span, insert again with same trace_id+span_id but different attributes, verify the updated values are stored.
- **ListServices returns distinct services with counts:** Insert spans for 3 services with varying counts, verify returned list has correct names, counts, and last-seen times.
- **ListServices returns empty list on empty DB:** No spans inserted, verify empty slice returned (not nil).
- **SearchTraces filters by service name:** Insert spans for services A, B, C. Search with service=A, verify only A's traces returned.
- **SearchTraces filters by span name substring:** Insert spans with names "handle-request", "db-query", "handle-response". Search with spanName="handle", verify 2 traces returned.
- **SearchTraces filters by duration range:** Insert spans with durations 10ms, 50ms, 200ms. Search with minDuration=20, maxDuration=100, verify only 50ms trace returned.
- **SearchTraces filters by time range:** Insert spans across different times. Search with start/end range, verify only spans within range returned.
- **SearchTraces respects limit:** Insert 50 traces, search with limit=10, verify 10 results returned.
- **SearchTraces orders by start_time descending:** Insert traces at t=1, t=2, t=3. Verify results come back in order t=3, t=2, t=1.
- **SearchTraces identifies root span correctly:** Insert a trace with root (parent_span_id IS NULL) and children. Verify `TraceSummary.RootSpan` matches the root span's name.
- **SearchTraces picks earliest root when multiple exist:** Insert a trace with two root spans (parent_span_id IS NULL) at t=2 and t=1. Verify `TraceSummary.RootSpan` is the name of the span at t=1 (earliest).
- **GetTrace returns a tree structure:** Insert 5 spans forming a tree (root -> A, root -> B, A -> C, A -> D). Verify returned `TraceDetail` has `roots` with 1 entry (the root span) containing 2 children (A, B), A has 2 children (C, D), B has 0 children.
- **GetTrace sorts children by startTime:** Insert child spans with startTime t=3, t=1, t=2 under same parent. Verify children array is ordered t=1, t=2, t=3.
- **GetTrace computes aggregate stats:** Insert trace with 5 spans, 2 services, 1 error. Verify `spanCount=5`, `serviceCount=2`, `errorCount=1`, `durationMs` matches root span duration.
- **GetTrace promotes orphan spans to roots:** Insert spans A (root), B (parent=A), C (parent=X where X does not exist). Verify `roots` has 2 entries: A (with child B) and C. No spans silently dropped.
- **GetTrace returns nil for unknown trace ID:** Query a nonexistent trace ID, verify nil returned.
- **GetSpan returns a single span:** Insert a span, retrieve by trace_id+span_id, verify all fields.
- **GetSpan returns nil for unknown span:** Query nonexistent IDs, verify nil returned.
- **GetDependencies finds cross-service calls:** Insert parent span (service=A) and child span (service=B, parent_span_id=parent's span_id, same trace). Verify dependency A->B with callCount=1.
- **GetDependencies excludes same-service calls:** Insert parent and child spans both in service A. Verify no dependencies returned.
- **GetDependencies respects time range:** Insert old and new cross-service spans. Query with recent time, verify only new dependencies included.
- **PurgeOlderThan deletes old spans:** Insert spans at t=1h ago and t=now. Purge older than 30m. Verify old spans deleted, new spans remain.
- **PurgeOlderThan returns count of deleted spans:** Insert 10 old spans, purge, verify returned count is 10.
- **Stats returns correct counts:** Insert known data, verify span count, trace count, oldest/newest times, and DB size > 0.

### Config (`internal/config/`)

- **Load returns defaults when no file exists:** Call Load with nonexistent path, verify all default values populated.
- **Load reads YAML file correctly:** Write a temp YAML file with custom values, load it, verify all fields override defaults.
- **Load merges partial config with defaults:** Write YAML with only `listen` set, verify listen is overridden but all other fields have defaults.
- **Default returns valid config:** Call Default(), verify listen=":7070", retention=168h, etc.

### SpanBuffer (`internal/server/`)

- **Add sends spans to channel:** Create buffer, add 5 spans, verify they appear in the store after flush interval.
- **Flush triggers at batch size:** Create buffer with batchSize=10, add 10 spans, verify store receives them before flush interval.
- **Shutdown drains remaining spans:** Add spans without triggering batch, call Shutdown, verify all spans written to store.
- **Add drops spans when channel full:** Create buffer with small channel, flood it, verify no panic and warning logged.

### HTTPExporter (`exporter.go`)

- **ExportSpan queues span for export:** Create exporter with test HTTP server, export a span, wait for flush, verify server received JSON POST.
- **Flush sends correct JSON payload:** Export a span with attributes and events, verify the JSON body matches IngestRequest schema with correct field values.
- **Batch flush at batch size:** Export batchSize spans, verify server receives a single POST with all spans.
- **Timer flush sends partial batch:** Export fewer than batchSize spans, wait for flush interval, verify server receives them.
- **Shutdown flushes remaining spans:** Export spans without triggering batch, call Shutdown, verify all spans sent to server.
- **Drops spans when buffer full:** Create exporter with small buffer, flood it, verify no panic and spans are dropped.
- **Handles server errors gracefully:** Export a span to a server that returns 500, verify no panic, warning logged.
- **Handles connection errors gracefully:** Export a span to an unreachable endpoint, verify no panic, error logged.
- **Converts SpanStatus int to string correctly:** Export spans with StatusUnset(0), StatusOK(1), StatusError(2), verify JSON payload contains "UNSET", "OK", "ERROR" respectively.
- **Hardcodes SpanKind to 1 (internal):** Export a span, verify JSON payload has `spanKind: 1`.
- **Converts zero ParentID to empty string:** Export a root span (zero ParentID), verify `parentSpanId` is omitted from JSON payload.

### DaemonManager (`internal/daemon/`)

- **NewDaemonManager returns SystemdManager on Linux:** Call NewDaemonManager, verify returned type (build-tag dependent test).
- **SystemdManager.Install writes correct unit file:** Mock exec.Command, verify the unit file content includes correct binary path and config path.
- **SystemdManager.Uninstall removes unit file:** Mock exec.Command, verify stop, disable, remove, daemon-reload sequence.

## Integration Tests

### Ingest to Query Flow

**Given** a running daemon with an empty database
**When** a POST to `/api/ingest` sends 3 spans forming a trace tree (root -> child1, root -> child2)
**Then** GET `/api/traces/:traceID` returns a `TraceDetail` with `roots` containing 1 entry with 2 `children`, each with the correct span data

### Service Aggregation

**Given** spans ingested from services "auth-svc", "api-gateway", and "user-svc"
**When** GET `/api/services` is called
**Then** all 3 services are listed with correct span counts and last-seen times

### Search Filters

**Given** traces with varying services, durations, and timestamps
**When** GET `/api/traces?service=auth-svc&minDuration=50` is called
**Then** only traces from auth-svc with duration >= 50ms are returned

### Dependency Graph

**Given** spans where api-gateway calls auth-svc (3 times) and user-svc (5 times)
**When** GET `/api/dependencies?lookback=1h` is called
**Then** two dependencies are returned: api-gateway->auth-svc (count=3) and api-gateway->user-svc (count=5)

### Status Endpoint

**Given** a running daemon with ingested spans
**When** GET `/api/status` is called
**Then** the response includes a non-empty `version`, non-zero `uptime`, and `db` stats with correct `spanCount` and `traceCount`

### Retention Purge

**Given** spans with start_time 48 hours ago and spans with start_time 1 hour ago
**When** purge is called with --older-than=24h
**Then** old spans are deleted and recent spans remain, verified via GET `/api/traces`

### End-to-End Library to UI

**Given** a running daemon
**When** an app uses `trace.SetExporter(trace.NewHTTPExporter("http://localhost:7070"))`, creates spans with `trace.Start`, and ends them
**Then** the spans appear in GET `/api/traces` with correct service name, trace structure, attributes, and events

## E2E Tests

### Full Trace Lifecycle

1. Start the daemon with a temp SQLite database
2. Configure an app with `NewHTTPExporter` pointing at the daemon
3. Create a trace with 3 nested spans, each with attributes and events
4. Wait for exporter flush
5. Query `GET /api/services` -- verify the service appears
6. Query `GET /api/traces?service=test-svc` -- verify the trace summary
7. Query `GET /api/traces/:traceID` -- verify `TraceDetail` with `roots` containing nested children tree
8. Query `GET /api/traces/:traceID/spans/:spanID` -- verify a specific span's attributes and events
9. Call purge with a future time -- verify all spans removed
10. Query `GET /api/traces` -- verify empty results

### Static UI Serving

1. Start the daemon
2. Fetch `GET /` -- verify 200 and HTML content (index.html)
3. Fetch `GET /services` -- verify 200 and same HTML (SPA fallback)
4. Fetch `GET /nonexistent.js` -- verify 404 or fallback to index.html

## Error Scenarios

- **Malformed ingest JSON:** POST invalid JSON to `/api/ingest`, expect 400 with error message.
- **Empty ingest body:** POST empty body to `/api/ingest`, expect 400.
- **Missing required fields in span:** POST span without traceId, expect 400.
- **Unknown trace ID on GET:** GET `/api/traces/nonexistent`, expect 404.
- **Unknown span ID on GET:** GET `/api/traces/valid-trace/spans/nonexistent`, expect 404.
- **Invalid lookback duration:** GET `/api/dependencies?lookback=banana`, expect 400.
- **Invalid limit value:** GET `/api/traces?limit=-1`, expect 400 or clamp to default.
- **Database file permissions:** Start daemon with unwritable DB path, expect clear error message on startup.
- **Concurrent ingest:** Send 100 concurrent POST requests to `/api/ingest`, verify all spans eventually stored without data corruption.
- **Root span parent_span_id stored as NULL:** POST a span with empty/missing `parentSpanId`, verify it is stored as SQL NULL (not empty string). Verify `GetTrace` returns it correctly with empty `parentSpanId` in JSON.
- **Purge API:** POST `/api/purge?olderThan=1h`, verify old spans deleted, recent spans remain.
- **Purge invalid duration:** POST `/api/purge?olderThan=banana`, expect 400.
