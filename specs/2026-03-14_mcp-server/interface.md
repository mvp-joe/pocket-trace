# Interface

## MCP Tool Input Types

All input types live in `internal/mcp/`. The SDK's generic `mcp.AddTool[In, Out]` infers JSON Schema from these structs automatically via `json` and `jsonschema` struct tags.

```go
package mcpserver

// SearchTracesInput is the input for the search_traces tool.
type SearchTracesInput struct {
	Service       string `json:"service,omitempty"       jsonschema:"filter by service name"`
	SpanName      string `json:"spanName,omitempty"      jsonschema:"filter by span name (substring match)"`
	MinDurationMs int64  `json:"minDurationMs,omitempty" jsonschema:"minimum span duration in milliseconds"`
	MaxDurationMs int64  `json:"maxDurationMs,omitempty" jsonschema:"maximum span duration in milliseconds"`
	Start         int64  `json:"start,omitempty"         jsonschema:"start time as unix nanoseconds"`
	End           int64  `json:"end,omitempty"           jsonschema:"end time as unix nanoseconds"`
	Limit         int    `json:"limit,omitempty"         jsonschema:"max traces to return (default 20, max 100)"`
}

// GetTraceInput is the input for the get_trace tool.
type GetTraceInput struct {
	TraceID string `json:"traceId" jsonschema:"required,the trace ID to retrieve"`
}

// GetSpanInput is the input for the get_span tool.
type GetSpanInput struct {
	TraceID string `json:"traceId" jsonschema:"required,the trace ID"`
	SpanID  string `json:"spanId"  jsonschema:"required,the span ID"`
}

// FindErrorTracesInput is the input for the find_error_traces tool.
type FindErrorTracesInput struct {
	Service string `json:"service,omitempty" jsonschema:"filter errors to a specific service"`
	Limit   int    `json:"limit,omitempty"   jsonschema:"max error traces to return (default 5, max 20)"`
}

// GetDependenciesInput is the input for the get_dependencies tool.
type GetDependenciesInput struct {
	SinceHours int `json:"sinceHours,omitempty" jsonschema:"lookback window in hours (default 24)"`
}
```

`list_services` and `get_status` have no inputs -- they use `struct{}` as the input type.

## Tool Handler Signature

All tool handlers use `mcp.ToolHandlerFor[In, any]`. The SDK's generic handler type has three return values:

```go
// The SDK type (for reference):
// type ToolHandlerFor[In, Out any] func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)

// All handlers in this spec use Out=any and return nil for the Out value,
// constructing the *CallToolResult directly with JSON-serialized TextContent.
// Example handler signature:
func (t *tools) listServices(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error)
```

## Server Setup

```go
package mcpserver

import (
	"net/http"

	"pocket-trace/internal/store"
)

// New creates an MCP server with all tools registered against the given store.
// Returns an http.Handler suitable for mounting on the Fiber app via adaptor.
func New(s *store.Store, version string) http.Handler
```

Internally, `New` creates the MCP server, registers tools, then wraps it in a `StreamableHTTPHandler` using the SDK's callback pattern:

```go
srv := mcp.NewServer(...)
// ... register tools ...
handler := mcp.NewStreamableHTTPHandler(
	func(r *http.Request) *mcp.Server { return srv },
	&mcp.StreamableHTTPOptions{Stateless: true},
)
return handler
```

## Integration Point

In `internal/server/server.go`, the `New` function signature does not change. The MCP handler is mounted inside `New` before the SPA fallback catch-all, using `adaptor.HTTPHandler` to bridge `http.Handler` into Fiber's handler type (Fiber v3 runs on fasthttp, not net/http):

```go
import "github.com/gofiber/fiber/v3/middleware/adaptor"

// In server.New(), after RegisterRoutes and before the SPA handler:
mcpHandler := mcpserver.New(s, h.Version)
app.All("/mcp", adaptor.HTTPHandler(mcpHandler))
```
