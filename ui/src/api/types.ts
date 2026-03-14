// API response envelope - all endpoints return this shape
export interface APIResponse<T> {
  data: T
  error?: string
}

// GET /api/services
export interface ServiceSummary {
  name: string
  spanCount: number
  lastSeen: number // unix nanos
}

// GET /api/traces (search results)
export interface TraceSummary {
  traceId: string
  rootSpan: string
  serviceName: string
  startTime: number // unix nanos
  durationMs: number
  spanCount: number
  errorCount: number
}

// GET /api/traces/:traceID
export interface TraceDetail {
  traceId: string
  roots: SpanNode[]
  spanCount: number
  serviceCount: number
  durationMs: number
  errorCount: number
}

// Tree node returned by trace detail endpoint
export interface SpanNode {
  traceId: string
  spanId: string
  parentSpanId?: string
  serviceName: string
  spanName: string
  spanKind: number
  startTime: number // unix nanos
  endTime: number // unix nanos
  durationMs: number
  statusCode: string // "OK" | "ERROR" | "UNSET"
  statusMessage?: string
  attributes?: Record<string, unknown>
  events?: SpanEvent[]
  children: SpanNode[]
}

// Flat span (single span endpoint)
export interface Span {
  traceId: string
  spanId: string
  parentSpanId?: string
  serviceName: string
  spanName: string
  spanKind: number
  startTime: number // unix nanos
  endTime: number // unix nanos
  durationMs: number
  statusCode: string
  statusMessage?: string
  attributes?: Record<string, unknown>
  events?: SpanEvent[]
}

export interface SpanEvent {
  name: string
  time: number // unix nanos
  attributes?: Record<string, unknown>
}

// GET /api/dependencies
export interface Dependency {
  parent: string
  child: string
  callCount: number
}

// GET /api/status
export interface StatusResponse {
  version: string
  uptime: string
  db: DBStats
}

export interface DBStats {
  spanCount: number
  traceCount: number
  dbSizeBytes: number
  oldestSpan: number // unix nanos, 0 if empty
  newestSpan: number // unix nanos, 0 if empty
}

// Query params for GET /api/traces
export interface TraceQuery {
  service?: string
  spanName?: string
  minDuration?: number // ms
  maxDuration?: number // ms
  start?: number // unix nanos
  end?: number // unix nanos
  limit?: number
}
