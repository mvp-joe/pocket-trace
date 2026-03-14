import type {
  APIResponse,
  Dependency,
  ServiceSummary,
  StatusResponse,
  TraceDetail,
  TraceQuery,
  TraceSummary,
} from './types.ts'

class APIError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'APIError'
    this.status = status
  }
}

async function fetchAPI<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)

  if (!res.ok) {
    // Try to extract error message from response body
    try {
      const body = (await res.json()) as APIResponse<unknown>
      if (body.error) {
        throw new APIError(body.error, res.status)
      }
    } catch (e) {
      if (e instanceof APIError) throw e
    }
    throw new APIError(`Request failed: ${res.status} ${res.statusText}`, res.status)
  }

  const body = (await res.json()) as APIResponse<T>
  if (body.error) {
    throw new APIError(body.error, res.status)
  }
  return body.data
}

export function getServices(): Promise<ServiceSummary[]> {
  return fetchAPI<ServiceSummary[]>('/api/services')
}

export function getTraces(query: TraceQuery): Promise<TraceSummary[]> {
  const params = new URLSearchParams()
  if (query.service) params.set('service', query.service)
  if (query.spanName) params.set('spanName', query.spanName)
  if (query.minDuration != null) params.set('minDuration', String(query.minDuration))
  if (query.maxDuration != null) params.set('maxDuration', String(query.maxDuration))
  if (query.start != null) params.set('start', String(query.start))
  if (query.end != null) params.set('end', String(query.end))
  if (query.limit != null) params.set('limit', String(query.limit))

  const qs = params.toString()
  return fetchAPI<TraceSummary[]>(`/api/traces${qs ? `?${qs}` : ''}`)
}

export function getTrace(traceId: string): Promise<TraceDetail> {
  return fetchAPI<TraceDetail>(`/api/traces/${encodeURIComponent(traceId)}`)
}

export function getDependencies(lookback?: string): Promise<Dependency[]> {
  const params = lookback ? `?lookback=${encodeURIComponent(lookback)}` : ''
  return fetchAPI<Dependency[]>(`/api/dependencies${params}`)
}

export function getStatus(): Promise<StatusResponse> {
  return fetchAPI<StatusResponse>('/api/status')
}
