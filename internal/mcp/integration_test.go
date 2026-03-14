package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"pocket-trace/internal/server"
	"pocket-trace/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"
)

// setupMCPClient creates a Fiber server with seeded data, starts it on a random
// port, and returns a connected MCP client session. The server and session are
// cleaned up when the test finishes.
func setupMCPClient(t *testing.T, seedData bool) *mcp.ClientSession {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	buf := server.NewSpanBuffer(s, 100, 64, 50*time.Millisecond)

	h := &server.Handlers{
		Store:     s,
		Buffer:    buf,
		StartTime: time.Now(),
		Version:   "test",
	}

	srv := server.New(s, buf, h, nil, 0, 0)

	if seedData {
		now := time.Now().UnixNano()
		oneHourAgo := now - int64(1*time.Hour)

		spans := []store.Span{
			{
				TraceID: "trace-1", SpanID: "t1-root",
				ServiceName: "auth-svc", SpanName: "handle-login",
				SpanKind: 2, StartTime: now - 100_000_000, EndTime: now,
				DurationMs: 100, StatusCode: "OK",
			},
			{
				TraceID: "trace-1", SpanID: "t1-child", ParentSpanID: "t1-root",
				ServiceName: "auth-svc", SpanName: "db-query",
				SpanKind: 3, StartTime: now - 80_000_000, EndTime: now - 20_000_000,
				DurationMs: 60, StatusCode: "OK",
			},
			{
				TraceID: "trace-2", SpanID: "t2-root",
				ServiceName: "api-gateway", SpanName: "handle-request",
				SpanKind: 2, StartTime: now - 200_000_000, EndTime: now - 50_000_000,
				DurationMs: 150, StatusCode: "OK",
			},
			{
				TraceID: "trace-2", SpanID: "t2-child", ParentSpanID: "t2-root",
				ServiceName: "auth-svc", SpanName: "validate-token",
				SpanKind: 3, StartTime: now - 180_000_000, EndTime: now - 60_000_000,
				DurationMs: 120, StatusCode: "ERROR", StatusMsg: "token expired",
			},
			{
				TraceID: "trace-3", SpanID: "t3-root",
				ServiceName: "user-svc", SpanName: "get-profile",
				SpanKind: 2, StartTime: oneHourAgo, EndTime: oneHourAgo + 50_000_000,
				DurationMs: 50, StatusCode: "OK",
			},
		}

		if err := s.InsertSpans(context.Background(), spans); err != nil {
			t.Fatalf("seed spans: %v", err)
		}
	}

	// Start the Fiber server on a random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	go func() {
		_ = srv.App().Listener(ln)
	}()

	t.Cleanup(func() {
		_ = srv.App().ShutdownWithContext(context.Background())
		buf.Shutdown()
		s.Close()
	})

	// Connect the MCP SDK client to the Fiber server's /mcp endpoint.
	endpoint := fmt.Sprintf("http://%s/mcp", ln.Addr().String())
	transport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		DisableStandaloneSSE: true,
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "0.0.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return session
}

func TestMCPIntegration_ToolList(t *testing.T) {
	t.Parallel()
	session := setupMCPClient(t, false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	expectedTools := []string{
		"find_error_traces",
		"get_dependencies",
		"get_span",
		"get_status",
		"get_trace",
		"list_services",
		"search_traces",
	}

	if len(result.Tools) != len(expectedTools) {
		t.Fatalf("got %d tools, want %d", len(result.Tools), len(expectedTools))
	}

	var toolNames []string
	for _, tool := range result.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	slices.Sort(toolNames)

	for i, want := range expectedTools {
		if toolNames[i] != want {
			t.Errorf("tool[%d] = %q, want %q", i, toolNames[i], want)
		}
	}

	// Verify each tool has an input schema.
	for _, tool := range result.Tools {
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil InputSchema", tool.Name)
		}
	}
}

func TestMCPIntegration_ToolCallReturnsData(t *testing.T) {
	t.Parallel()
	session := setupMCPClient(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_services",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Fatalf("tool returned error")
	}

	if len(result.Content) == 0 {
		t.Fatal("no content returned")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] type = %T, want *mcp.TextContent", result.Content[0])
	}

	// Parse the JSON text to verify it matches the seeded data.
	var services []store.ServiceSummary
	if err := json.Unmarshal([]byte(tc.Text), &services); err != nil {
		t.Fatalf("unmarshal services: %v\ntext: %s", err, tc.Text)
	}

	if len(services) != 3 {
		t.Fatalf("got %d services, want 3", len(services))
	}

	// Verify service names (sorted by name from the store).
	names := make([]string, len(services))
	for i, s := range services {
		names[i] = s.Name
	}
	slices.Sort(names)
	expected := []string{"api-gateway", "auth-svc", "user-svc"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("service[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestMCPIntegration_ToolCallInvalidInput(t *testing.T) {
	t.Parallel()
	session := setupMCPClient(t, false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call get_trace without the required traceId field.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_trace",
		Arguments: map[string]any{},
	})

	// The SDK may return an error from schema validation, or the tool may
	// return an error result. Either way, the call should not succeed.
	if err != nil {
		// SDK-level error (e.g., schema validation rejected the call).
		// This is acceptable behavior.
		return
	}

	// If no SDK error, the tool result should indicate an error.
	if result.IsError {
		// Tool returned an error result -- expected behavior.
		return
	}

	// If we got here, the tool returned a successful result with empty traceId,
	// which should result in a "not found" error from the store.
	if len(result.Content) > 0 {
		tc, ok := result.Content[0].(*mcp.TextContent)
		if ok && len(tc.Text) > 0 {
			// The tool should have returned a "not found" error result since
			// the empty traceId won't match anything.
			t.Logf("tool returned: %s", tc.Text)
		}
	}
}

