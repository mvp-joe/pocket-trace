# Tests

## Unit Tests

Test each tool handler method on the `tools` struct with a real SQLite store (matching the existing test pattern in `handlers_test.go`).

### list_services
- Empty database returns empty array
- Database with seeded spans returns correct service names, span counts, and last-seen times

### search_traces
- No filters returns all traces ordered by start time descending
- Filter by service name returns only matching traces
- Filter by min/max duration works correctly
- Limit defaults to 20 when not provided
- Limit is clamped to 100 when exceeding max

### get_trace
- Valid trace ID returns full trace detail with correct span tree structure
- Nonexistent trace ID returns an error result (not a Go error -- an MCP tool result with `IsError: true`)

### get_span
- Valid trace ID + span ID returns the span with all fields
- Nonexistent span returns an error result

### find_error_traces
- Returns only traces that have errors (ErrorCount > 0)
- Each returned trace includes the full span tree (is a `TraceDetail`, not a `TraceSummary`)
- Service filter restricts results to that service
- Limit defaults to 5 and is respected
- Database with no error traces returns empty array

### get_dependencies
- Returns correct parent-child service relationships
- sinceHours defaults to 24 when not provided
- Custom sinceHours value is respected (old dependencies excluded)

### get_status
- Returns correct span count, trace count, and DB size

## Integration Tests

### MCP server responds to tool list

**Given** a Fiber server with the MCP handler mounted at `/mcp`
**When** a valid MCP initialize request followed by a tools/list request is sent via HTTP POST
**Then** the response contains all 7 tool names with their input schemas

### MCP tool call returns data

**Given** a Fiber server with seeded trace data and the MCP handler mounted
**When** a valid MCP `tools/call` request for `list_services` is sent via HTTP POST
**Then** the response contains a `CallToolResult` with a `TextContent` whose text is valid JSON matching the seeded service data

### MCP tool call with invalid input

**Given** a Fiber server with the MCP handler mounted
**When** a `tools/call` request for `get_trace` is sent without the required `traceId` field
**Then** the response contains an error (SDK validates required fields from the schema)

## Error Scenarios

### Store errors propagate as tool errors
- If the store returns an error (e.g., database locked), the tool handler returns it as a Go error, which the SDK converts to an MCP error response

### find_error_traces with no matching traces
- When SearchTraces returns results but none have ErrorCount > 0, the tool returns an empty array (not an error)

### find_error_traces with GetTrace failure
- If SearchTraces succeeds but a subsequent GetTrace call fails for one trace, the tool returns the traces it successfully fetched (partial results), not an error. Partial data is more useful to the LLM than no data.
