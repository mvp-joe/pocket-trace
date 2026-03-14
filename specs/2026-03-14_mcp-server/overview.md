# MCP Server

## Summary

Add a Model Context Protocol (MCP) server to pocket-trace, enabling AI tools (Claude, Cursor, etc.) to query trace data directly via the standardized MCP tool-calling interface. The MCP server exposes read-only tools that map to the existing `store.Store` query methods, mounted as a Streamable HTTP endpoint at `/mcp` on the existing Fiber v3 server. One compound tool (`find_error_traces`) provides value beyond the raw API by fetching full trace trees for error traces in a single call.

## Goals

- Expose trace data to AI agents via MCP tools (list services, search traces, get trace details, find errors, view dependencies, check status)
- Run on the existing Fiber v3 server at `/mcp` -- no separate process or port
- Stateless operation -- no session tracking, no MCP resources or prompts
- Read-only access -- no purge or ingest via MCP
- Provide a compound `find_error_traces` tool that fetches full span trees for error traces in one call

## Non-Goals

- MCP resources or prompts (tools only)
- Write operations (ingest, purge) via MCP
- Authentication or authorization for MCP (matches existing API which has none)
- Separate MCP transport (stdio, SSE) -- Streamable HTTP only
- A standalone MCP binary -- this is embedded in the existing daemon

## Current Status

Planning

## Key Files

- [interface.md](interface.md) -- Go type signatures for MCP tool inputs and the server setup function
- [apis.md](apis.md) -- MCP tool definitions (name, description, input/output schemas)
- [system-components.md](system-components.md) -- `internal/mcp` package components
- [implementation.md](implementation.md) -- phased implementation plan
- [tests.md](tests.md) -- test specifications
- [dependencies.md](dependencies.md) -- new `github.com/modelcontextprotocol/go-sdk` dependency
