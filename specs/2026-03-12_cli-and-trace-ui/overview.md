# pocket-trace: Self-Contained Tracing System

## Summary

pocket-trace pivots from a library-only OTLP exporter into a complete, self-contained tracing system. A single Go binary acts as the daemon -- accepting trace data via JSON HTTP POST, storing spans in SQLite, and serving a React UI on port 7070. The existing library API (`trace.Start`, `span.End()`, etc.) remains unchanged; only the exporter is replaced. This eliminates the Quickwit dependency and all OTLP/protobuf packages, giving users a zero-infrastructure tracing solution suitable for local development, single-server deployments, and small VPS environments.

## Goals

- Replace the OTLP exporter with a JSON HTTP POST exporter targeting the pocket-trace daemon
- Build a daemon that ingests spans, stores them in SQLite, and serves a query API
- Provide CLI commands for service management (install/uninstall/status/purge) via Cobra
- Embed a React UI (Vite + shadcn/ui) for trace visualization (service list, search, waterfall, dependency graph)
- Remove all OTLP and protobuf dependencies
- Support systemd service management on Linux, with a DaemonManager interface for future macOS/launchd support
- Maintain the existing library API surface -- apps only change their exporter setup

## Non-Goals

- OpenTelemetry compatibility or OTLP ingestion
- Multi-node / distributed storage
- Authentication or multi-tenancy
- macOS launchd implementation (interface only, implemented later)
- Windows support
- Alerting or anomaly detection
- Modifying the existing `trace.Start` / `span.End()` library API (only the exporter changes)

## Current Status

Complete (implemented autonomously via /spec:implement-all, 2026-03-14)

## Key Files

- [implementation.md](implementation.md) -- phased build plan
- [interface.md](interface.md) -- Go types and interfaces
- [apis.md](apis.md) -- REST API endpoints
- [schema.md](schema.md) -- SQLite schema
- [system-components.md](system-components.md) -- backend architecture
- [ui-components.md](ui-components.md) -- React component hierarchy
- [dependencies.md](dependencies.md) -- new dependencies
- [tests.md](tests.md) -- test specifications
