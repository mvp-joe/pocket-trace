# Implementation

## Phase 1: MCP Server Skeleton and Basic Tools

- [x] Add `github.com/modelcontextprotocol/go-sdk` dependency (`go get github.com/modelcontextprotocol/go-sdk@v1.4.1`)
- [x] Create `internal/mcp/` package with `mcp.go` file
- [x] Define `tools` struct holding `*store.Store`
- [x] Define input types: `SearchTracesInput`, `GetTraceInput`, `GetSpanInput`, `FindErrorTracesInput`, `GetDependenciesInput`
- [x] Implement `New(s *store.Store, version string) http.Handler` factory function
- [x] Implement `listServices` tool handler
- [x] Implement `searchTraces` tool handler (convert input to `store.TraceQuery`)
- [x] Implement `getTrace` tool handler (return error text for not-found)
- [x] Implement `getSpan` tool handler (return error text for not-found)
- [x] Implement `getStatus` tool handler
- [x] Implement `getDependencies` tool handler (convert `sinceHours` to `time.Time` via `time.Now().Add(-duration)`)

## Phase 2: Compound Tool and Server Integration

- [ ] Implement `findErrorTraces` compound tool handler (search, filter errors, fetch full traces, return partial results on individual GetTrace failure)
- [ ] Mount MCP handler in `internal/server/server.go` via `adaptor.HTTPHandler()` between `RegisterRoutes` and SPA fallback
- [ ] Verify adaptor passes requests correctly (path, headers, SSE streaming)
- [ ] Verify `go build` succeeds with new dependency
- [ ] Manual smoke test: start daemon, call `/mcp` with MCP initialize + tool list

## Phase 3: Tests

- [ ] Write unit tests for each tool handler in `internal/mcp/mcp_test.go`
- [ ] Write integration tests in `internal/mcp/integration_test.go`: MCP tool call through the full Fiber server with adaptor
- [ ] Verify all existing tests still pass (`go test ./...`)

## Notes

- All tool handlers serialize results as JSON text via `mcp.TextContent`. The SDK handles JSON-RPC framing.
- The `find_error_traces` tool searches with a higher limit (e.g., 100) then filters to `ErrorCount > 0` client-side, since there is no store-level "errors only" filter.
- Tool descriptions should be clear and useful to LLMs -- they appear in the MCP tool listing.
