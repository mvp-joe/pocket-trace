# Pocket Trace

Lightweight tracing for Go services and desktop apps. Gives you visibility into what's happening without the overhead of a full OpenTelemetry setup.

Spans log to `slog` for local visibility and optionally export to [Quickwit](https://quickwit.io) via OTLP for search and trace visualization.

## Install

```bash
go get pocket-trace
```

## Usage

```go
package main

import (
	"context"
	"fmt"

	trace "pocket-trace"
	"pocket-trace/otlp"
)

func main() {
	// Configure service name and OTLP export to Quickwit.
	trace.SetServiceName("my-app")
	trace.SetExporter(otlp.NewExporter("http://localhost:7281"))
	defer trace.Shutdown(context.Background())

	ctx := context.Background()

	// Create a span — logs entry to slog and exports to Quickwit on End().
	span, ctx := trace.Start(ctx, "handle-request", "path", "/api/foo")
	defer span.End()

	// Record events within the span.
	span.Event("cache-miss", "key", "user:123")

	// Nested spans share the same trace ID.
	loadUser(ctx)
}

func loadUser(ctx context.Context) error {
	span, ctx := trace.Start(ctx, "load-user", "user_id", 123)
	var err error
	defer span.EndErr(&err)

	// ... do work ...
	if err != nil {
		return fmt.Errorf("load user: %w", err)
	}
	return nil
}
```

### slog output

Spans log to slog with markers for entry (`→`), events (`•`), and exit (`←`):

```
level=INFO msg="→ handle-request" trace_id=a1b2c3... span_id=d4e5f6... path=/api/foo
level=INFO msg="• cache-miss" trace_id=a1b2c3... span_id=d4e5f6... elapsed_ms=2 key=user:123
level=INFO msg="→ load-user" trace_id=a1b2c3... span_id=f7a8b9... parent_id=d4e5f6... user_id=123
level=INFO msg="← load-user" trace_id=a1b2c3... span_id=f7a8b9... parent_id=d4e5f6... duration_ms=5
level=INFO msg="← handle-request" trace_id=a1b2c3... span_id=d4e5f6... duration_ms=8
```

### Without Quickwit

If you don't need search/visualization, skip the exporter. Spans still log to slog:

```go
span, ctx := trace.Start(ctx, "my-operation")
defer span.End()
```

### Exporter options

```go
otlp.NewExporter("http://localhost:7281",
	otlp.WithBatchSize(256),          // flush after N spans (default 256)
	otlp.WithFlushInterval(2*time.Second), // flush interval (default 2s)
)
```

## Setting up Quickwit

Quickwit indexes your traces and makes them searchable. It runs locally with a single command — no cluster, no cloud, no config.

### Install Quickwit

```bash
curl -L https://install.quickwit.io | sh
```

Or with Docker:

```bash
docker pull quickwit/quickwit
```

### Run Quickwit

```bash
./quickwit run
```

Or with Docker:

```bash
docker run --rm -v $(pwd)/qwdata:/quickwit/qwdata \
  -p 7280:7280 -p 7281:7281 \
  quickwit/quickwit run
```

That's it. Quickwit auto-creates the `otel-traces-v0_7` index when it receives the first OTLP trace — no schema setup needed.

| Port | Purpose |
|------|---------|
| 7280 | REST API + Web UI |
| 7281 | OTLP ingestion + Jaeger gRPC |

## Searching traces

### Quickwit UI

Open **http://localhost:7280/ui/search**, select the `otel-traces-v0_7` index, and search.

### REST API

```bash
# Find traces from a specific service
curl "http://localhost:7280/api/v1/otel-traces-v0_7/search?query=service_name:my-app"

# Find error spans
curl "http://localhost:7280/api/v1/otel-traces-v0_7/search?query=span_status:ERROR"

# Search by span name
curl "http://localhost:7280/api/v1/otel-traces-v0_7/search?query=span_name:load-user"

# Combine filters
curl "http://localhost:7280/api/v1/otel-traces-v0_7/search?query=service_name:my-app AND span_status:ERROR&max_hits=50"
```

### CLI

```bash
quickwit index search --index otel-traces-v0_7 --query "service_name:my-app"
```

### Query syntax

Quickwit uses a Lucene-like query language:

```
service_name:my-app                              # field match
span_name:"handle-request"                       # exact phrase
service_name:my-app AND span_status:ERROR        # boolean AND
service_name:app1 OR service_name:app2           # boolean OR
NOT span_status:ok                               # negation
resource_attributes.service.name:my-app          # nested fields
service_name: IN [app1 app2 app3]                # set membership
```

## Viewing traces with Jaeger UI

Quickwit is an official Jaeger storage backend. Run Jaeger pointed at Quickwit to get trace visualization with waterfall views, service graphs, and trace comparison.

> **Note:** Quickwit currently implements the Jaeger v1 gRPC SpanReader API. Jaeger v2 uses a new storage API that Quickwit doesn't support yet, so use the v1 image.

**Linux:**

```bash
docker run --rm --name jaeger \
  --network=host \
  -e SPAN_STORAGE_TYPE=grpc \
  -e GRPC_STORAGE_SERVER=127.0.0.1:7281 \
  jaegertracing/jaeger-query:1.76.0
```

**macOS:**

```bash
docker run --rm --name jaeger \
  -e SPAN_STORAGE_TYPE=grpc \
  -e GRPC_STORAGE_SERVER=host.docker.internal:7281 \
  -p 16686:16686 \
  jaegertracing/jaeger-query:1.76.0
```

Open **http://localhost:16686** to browse traces.

## Architecture

```
your apps → pocket-trace → slog (console)
                         → OTLP protobuf POST → Quickwit :7281
                                                     ↓
                                              Jaeger UI :16686
                                              Quickwit UI :7280
```

No OTEL collector. No OTEL SDK. Just your app, a protobuf POST, and Quickwit.

## Dependencies

The core `trace` package has zero external dependencies (just stdlib).

The `otlp` exporter adds:

- `go.opentelemetry.io/proto/otlp` — pre-generated OTLP protobuf types
- `google.golang.org/protobuf` — protobuf runtime
- `golang.org/x/net` — HTTP/2 cleartext (h2c) transport
