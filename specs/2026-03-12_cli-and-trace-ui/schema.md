# SQLite Schema

## Entity Relationship

```
┌─────────────────────────────────────────────────┐
│                     spans                        │
├─────────────────────────────────────────────────┤
│ PK: (trace_id, span_id)                         │
│                                                  │
│ trace_id ──────────┐                             │
│ span_id            │  self-referential           │
│ parent_span_id ────┘  (parent-child tree)        │
│ service_name                                     │
│ span_name                                        │
│ span_kind                                        │
│ start_time (unix nanos)                          │
│ end_time (unix nanos)                            │
│ duration_ms                                      │
│ status_code                                      │
│ status_message                                   │
│ attributes (JSON object)                         │
│ events (JSON array)                              │
└─────────────────────────────────────────────────┘
```

## Table: spans

| Field          | Type    | References         | Notes                                      |
|----------------|---------|--------------------|--------------------------------------------|
| trace_id       | TEXT    |                    | NOT NULL. Hex-encoded 16-byte trace ID.    |
| span_id        | TEXT    |                    | NOT NULL. Hex-encoded 8-byte span ID.      |
| parent_span_id | TEXT    | spans.span_id      | NULL for root spans. Self-referential FK.  |
| service_name   | TEXT    |                    | NOT NULL. Service that produced the span.  |
| span_name      | TEXT    |                    | NOT NULL. Operation name.                  |
| span_kind      | INTEGER |                    | 0=unspecified, 1=internal, 2=server, etc.  |
| start_time     | INTEGER |                    | NOT NULL. Unix nanoseconds.                |
| end_time       | INTEGER |                    | NOT NULL. Unix nanoseconds.                |
| duration_ms    | INTEGER |                    | NOT NULL. Precomputed for query efficiency. |
| status_code    | TEXT    |                    | "OK", "ERROR", or "UNSET".                 |
| status_message | TEXT    |                    | Error message when status is ERROR.        |
| attributes     | TEXT    |                    | JSON object of key-value pairs.            |
| events         | TEXT    |                    | JSON array of span events.                 |

**Primary Key:** `(trace_id, span_id)`

## Indexes

| Index Name              | Columns                   | Purpose                                    |
|-------------------------|---------------------------|--------------------------------------------|
| idx_spans_trace_id      | trace_id                  | Fast trace lookup (all spans in a trace).  |
| idx_spans_service_time  | service_name, start_time  | Service-scoped time-range queries.         |
| idx_spans_start_time    | start_time                | Retention purge by age.                    |

## Schema Migration

The store applies schema on startup using `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS`. No migration framework -- the schema is simple enough for idempotent DDL. Future schema changes will use a `schema_version` table with sequential migration functions.

## SQLite Configuration

Applied via PRAGMA on connection open:

- `PRAGMA journal_mode=WAL` -- concurrent reads during writes
- `PRAGMA synchronous=NORMAL` -- safe with WAL, better write throughput
- `PRAGMA foreign_keys=OFF` -- no FK constraints needed (parent_span_id is informational)
- `PRAGMA busy_timeout=5000` -- wait up to 5s on lock contention
