# APIs

MCP tools exposed by the server. All tools are read-only. Transport is Streamable HTTP at `/mcp`.

## Tools

### list_services

List all services that have sent traces, with span counts and last-seen times.

- **Input:** none
- **Output:** JSON array of `store.ServiceSummary` (name, spanCount, lastSeen)
- **Maps to:** `store.ListServices(ctx)`

### search_traces

Search traces with optional filters. Returns summary information, not full span trees.

- **Input:** `SearchTracesInput` (all fields optional)
- **Output:** JSON array of `store.TraceSummary` (traceId, rootSpan, serviceName, startTime, durationMs, spanCount, errorCount)
- **Maps to:** `store.SearchTraces(ctx, TraceQuery)`
- **Notes:** Limit defaults to 20, max 100. Time filters are unix nanoseconds.

### get_trace

Get a full trace with its complete span tree.

- **Input:** `GetTraceInput` (traceId required)
- **Output:** `store.TraceDetail` (traceId, roots as span tree, spanCount, serviceCount, durationMs, errorCount)
- **Maps to:** `store.GetTrace(ctx, traceID)`
- **Errors:** Returns error text if trace not found

### get_span

Get a single span by trace ID and span ID.

- **Input:** `GetSpanInput` (traceId, spanId both required)
- **Output:** `store.Span` (all span fields including attributes and events)
- **Maps to:** `store.GetSpan(ctx, traceID, spanID)`
- **Errors:** Returns error text if span not found

### find_error_traces

Compound tool: find traces with errors and return their full span trees. This is the key value-add -- an LLM gets complete error context in one call instead of needing to search then fetch each trace individually.

- **Input:** `FindErrorTracesInput` (service optional, limit defaults to 5 max 20)
- **Output:** JSON array of `store.TraceDetail` -- full span trees for each error trace
- **Behavior:**
  1. Call `SearchTraces` with the service filter (if provided), limit set higher than requested
  2. Filter results to only traces where `ErrorCount > 0`
  3. Call `GetTrace` for each matching trace (up to the requested limit)
  4. Return the full trace details

### get_dependencies

Get the service dependency graph showing which services call which.

- **Input:** `GetDependenciesInput` (sinceHours optional, defaults to 24)
- **Output:** JSON array of `store.Dependency` (parent, child, callCount)
- **Maps to:** `store.GetDependencies(ctx, since)`

### get_status

Get daemon health and database statistics.

- **Input:** none
- **Output:** `store.DBStats` (spanCount, traceCount, dbSizeBytes, oldestSpan, newestSpan)
- **Maps to:** `store.Stats(ctx)`
