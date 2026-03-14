# Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/modelcontextprotocol/go-sdk` | v1.4.1 | Official Go MCP SDK. Provides `mcp.NewServer`, `mcp.AddTool`, `mcp.NewStreamableHTTPHandler`, and all MCP protocol types. |

## Rationale

The official Go MCP SDK (`modelcontextprotocol/go-sdk`) is the canonical implementation maintained in collaboration with Google. It provides:

- Generic `AddTool[In, Out]` that auto-generates JSON Schema from Go struct tags, eliminating manual schema definition
- `StreamableHTTPHandler` that implements `http.Handler`, making it trivial to mount on the existing Fiber v3 server
- Built-in stateless mode (`Stateless: true`) that avoids session tracking overhead
- Input validation against the generated schema before the handler is called

The alternative (`mark3labs/mcp-go`) is a community SDK that predates the official one. The official SDK is preferred for long-term maintenance and spec compliance.

Version 1.4.1 is pinned because versions before 1.3.1 have a known JSON parsing vulnerability (CVE-2026-27896).
