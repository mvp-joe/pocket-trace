package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"pocket-trace/internal/store"
)

// tools holds the store dependency and provides MCP tool handler methods.
type tools struct {
	store *store.Store
}

// --- Input types ---

// SearchTracesInput is the input for the search_traces tool.
type SearchTracesInput struct {
	Service       string `json:"service,omitempty"       jsonschema:"filter by service name"`
	SpanName      string `json:"spanName,omitempty"      jsonschema:"filter by span name (substring match)"`
	MinDurationMs int64  `json:"minDurationMs,omitempty" jsonschema:"minimum span duration in milliseconds"`
	MaxDurationMs int64  `json:"maxDurationMs,omitempty" jsonschema:"maximum span duration in milliseconds"`
	Start         int64  `json:"start,omitempty"         jsonschema:"start time as unix nanoseconds"`
	End           int64  `json:"end,omitempty"            jsonschema:"end time as unix nanoseconds"`
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

// New creates an MCP server with all tools registered against the given store.
// Returns an http.Handler suitable for mounting on the Fiber app via adaptor.
func New(s *store.Store, version string) http.Handler {
	t := &tools{store: s}

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "pocket-trace",
		Version: version,
	}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_services",
		Description: "List all instrumented services with span counts and last-seen timestamps. Use this to discover what services are being traced.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.listServices)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_traces",
		Description: "Search for traces matching filter criteria. Supports filtering by service name, span name (substring), duration range, and time range. Returns trace summaries sorted by most recent first.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.searchTraces)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_trace",
		Description: "Get the full span tree for a specific trace by its trace ID. Returns hierarchical span data including timing, attributes, and parent-child relationships.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.getTrace)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_span",
		Description: "Get a single span by trace ID and span ID. Returns full span details including attributes, events, timing, and status.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.getSpan)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_status",
		Description: "Get daemon health and database statistics including span count, trace count, database size, and time range of stored data.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.getStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_error_traces",
		Description: "Find traces that contain errors and return their full span trees. This is the best starting point for debugging errors — it searches for recent traces with failures and returns complete span details so you can immediately see what went wrong without needing multiple calls. Optionally filter to a specific service.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.findErrorTraces)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_dependencies",
		Description: "Get the service dependency graph showing which services call which other services, with call counts. Useful for understanding system topology and communication patterns.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.getDependencies)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	return handler
}

// --- Tool handlers ---

func (t *tools) listServices(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	services, err := t.store.ListServices(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list services: %w", err)
	}
	return jsonResult(services)
}

func (t *tools) searchTraces(ctx context.Context, _ *mcp.CallToolRequest, input SearchTracesInput) (*mcp.CallToolResult, any, error) {
	q := store.TraceQuery{
		ServiceName: input.Service,
		SpanName:    input.SpanName,
		MinDuration: input.MinDurationMs,
		MaxDuration: input.MaxDurationMs,
		Limit:       input.Limit,
	}

	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}

	if input.Start != 0 {
		q.Start = time.Unix(0, input.Start)
	}
	if input.End != 0 {
		q.End = time.Unix(0, input.End)
	}

	traces, err := t.store.SearchTraces(ctx, q)
	if err != nil {
		return nil, nil, fmt.Errorf("search traces: %w", err)
	}
	return jsonResult(traces)
}

func (t *tools) getTrace(ctx context.Context, _ *mcp.CallToolRequest, input GetTraceInput) (*mcp.CallToolResult, any, error) {
	trace, err := t.store.GetTrace(ctx, input.TraceID)
	if err != nil {
		return nil, nil, fmt.Errorf("get trace: %w", err)
	}
	if trace == nil {
		return errResult("trace %s not found", input.TraceID), nil, nil
	}
	return jsonResult(trace)
}

func (t *tools) getSpan(ctx context.Context, _ *mcp.CallToolRequest, input GetSpanInput) (*mcp.CallToolResult, any, error) {
	span, err := t.store.GetSpan(ctx, input.TraceID, input.SpanID)
	if err != nil {
		return nil, nil, fmt.Errorf("get span: %w", err)
	}
	if span == nil {
		return errResult("span %s/%s not found", input.TraceID, input.SpanID), nil, nil
	}
	return jsonResult(span)
}

func (t *tools) getStatus(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	stats, err := t.store.Stats(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get status: %w", err)
	}
	return jsonResult(stats)
}

func (t *tools) findErrorTraces(ctx context.Context, _ *mcp.CallToolRequest, input FindErrorTracesInput) (*mcp.CallToolResult, any, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	// Search with a larger limit to increase chances of finding enough error traces.
	q := store.TraceQuery{
		ServiceName: input.Service,
		Limit:       100,
	}
	summaries, err := t.store.SearchTraces(ctx, q)
	if err != nil {
		return nil, nil, fmt.Errorf("find error traces: %w", err)
	}

	// Collect full trace details for traces with errors, tolerating individual failures.
	var results []*store.TraceDetail
	for _, s := range summaries {
		if s.ErrorCount <= 0 {
			continue
		}
		detail, err := t.store.GetTrace(ctx, s.TraceID)
		if err != nil {
			continue // partial failure: skip this trace
		}
		if detail == nil {
			continue
		}
		results = append(results, detail)
		if len(results) >= limit {
			break
		}
	}

	if results == nil {
		results = []*store.TraceDetail{}
	}
	return jsonResult(results)
}

func (t *tools) getDependencies(ctx context.Context, _ *mcp.CallToolRequest, input GetDependenciesInput) (*mcp.CallToolResult, any, error) {
	hours := input.SinceHours
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	deps, err := t.store.GetDependencies(ctx, since)
	if err != nil {
		return nil, nil, fmt.Errorf("get dependencies: %w", err)
	}
	return jsonResult(deps)
}

// --- Helpers ---

// jsonResult serializes v as JSON and wraps it in a CallToolResult with TextContent.
func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

// errResult returns a CallToolResult with IsError set and a formatted message.
func errResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}
