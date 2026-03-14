# Pocket Trace

Lightweight tracing for Go services and desktop apps. Gives you visibility into what's happening without the overhead of a full OpenTelemetry setup.

Spans log to `slog` for local visibility and export to the built-in daemon for search and trace visualization. No OTEL collector, no external services — just your app and a single binary.

## Quick Start

### 1. Install the daemon

```bash
go install pocket-trace/cmd/pocket-trace@latest
```

### 2. Start it

```bash
pocket-trace run
```

The daemon listens on `:7070`, stores traces in SQLite (`~/.local/share/pocket-trace/`), and serves the web UI.

### 3. Add the library to your app

```bash
go get pocket-trace
```

```go
package main

import (
	"context"

	trace "pocket-trace"
)

func main() {
	trace.SetServiceName("my-app")
	trace.SetExporter(trace.NewHTTPExporter("http://localhost:7070"))
	defer trace.Shutdown(context.Background())

	ctx := context.Background()

	span, ctx := trace.Start(ctx, "handle-request", "path", "/api/foo")
	defer span.End()

	span.Event("cache-miss", "key", "user:123")

	loadUser(ctx)
}

func loadUser(ctx context.Context) {
	span, _ := trace.Start(ctx, "load-user", "user_id", 123)
	var err error
	defer span.EndErr(&err)

	// ... do work ...
}
```

### 4. View traces

Open **http://localhost:7070** to see traces in the web UI, or query the API directly:

```bash
curl http://localhost:7070/api/services
curl http://localhost:7070/api/traces?service=my-app
curl http://localhost:7070/api/traces/<traceID>
```

## slog Output

Spans always log to slog, with or without the daemon:

```
level=INFO msg="→ handle-request" trace_id=a1b2c3... span_id=d4e5f6... path=/api/foo
level=INFO msg="• cache-miss" trace_id=a1b2c3... span_id=d4e5f6... elapsed_ms=2 key=user:123
level=INFO msg="→ load-user" trace_id=a1b2c3... span_id=f7a8b9... parent_id=d4e5f6... user_id=123
level=INFO msg="← load-user" trace_id=a1b2c3... span_id=f7a8b9... parent_id=d4e5f6... duration_ms=5
level=INFO msg="← handle-request" trace_id=a1b2c3... span_id=d4e5f6... duration_ms=8
```

## Library-Only Mode

If you don't need search/visualization, skip the exporter. Spans still log to slog:

```go
span, ctx := trace.Start(ctx, "my-operation")
defer span.End()
```

## Exporter Options

```go
trace.NewHTTPExporter("http://localhost:7070",
	trace.WithBatchSize(256),               // flush after N spans (default 256)
	trace.WithFlushInterval(2*time.Second),  // flush interval (default 2s)
)
```

## Architecture

```
your apps → pocket-trace lib → slog (console)
                              → JSON HTTP POST → pocket-trace daemon :7070
                                                      │
                                                 SQLite (spans)
                                                      │
                                                 Web UI (React)
```

## Daemon Reference

### Running Locally

```bash
# No config needed
pocket-trace run

# With a config file
pocket-trace run --config my-config.yaml
```

### Installing as a System Service

```bash
# Creates config, data dir, and starts the systemd service
sudo pocket-trace install

# Check status
pocket-trace status

# Remove the service
sudo pocket-trace uninstall
```

### Configuration

Pass `--config` or let `install` create `/etc/pocket-trace/config.yaml`. All fields are optional:

```yaml
listen: ":7070"
db_path: "/var/lib/pocket-trace/pocket-trace.db"
retention: "168h"        # 7 days
purge_interval: "1h"
flush_interval: "2s"
buffer_size: 4096
log_level: "info"
```

### CLI Commands

| Command | Description |
|---------|-------------|
| `run` | Run daemon in the foreground |
| `install` | Install as a systemd service (requires root) |
| `uninstall` | Remove the systemd service (requires root) |
| `status` | Show daemon and service status |
| `purge --older-than 24h` | Delete old trace data |

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/ingest` | Accept spans from the exporter |
| `GET` | `/api/services` | List services with span counts |
| `GET` | `/api/traces` | Search traces (filter by service, span name, duration, time range) |
| `GET` | `/api/traces/:traceID` | Get full trace as a pre-built span tree |
| `GET` | `/api/traces/:traceID/spans/:spanID` | Get a single span |
| `GET` | `/api/dependencies` | Service-to-service dependency graph |
| `GET` | `/api/status` | Daemon health and DB stats |
| `POST` | `/api/purge?olderThan=24h` | Delete spans older than duration |

## Development

```bash
# Full build (Go binary with embedded React UI)
make build

# Development build (Go binary, no UI embedding)
make dev

# Clean build artifacts
make clean
```

## Dependencies

The `trace` library has zero external dependencies (just stdlib).
