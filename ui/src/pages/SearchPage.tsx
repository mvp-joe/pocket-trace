import { useCallback, useMemo } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useServices, useTraces } from '@/api/hooks.ts'
import type { TraceQuery, TraceSummary } from '@/api/types.ts'
import { PageHeader } from '@/components/PageHeader.tsx'
import { ServiceBadge } from '@/components/ServiceBadge.tsx'
import { DurationBar } from '@/components/DurationBar.tsx'
import { ErrorBadge } from '@/components/ErrorBadge.tsx'
import { TimeDisplay } from '@/components/TimeDisplay.tsx'
import { Button } from '@/components/ui/button.tsx'
import { Input } from '@/components/ui/input.tsx'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select.tsx'

// Time preset options for the MVP time range filter
const TIME_PRESETS = [
  { label: 'Last 15 minutes', value: '15m', ms: 15 * 60 * 1000 },
  { label: 'Last 1 hour', value: '1h', ms: 60 * 60 * 1000 },
  { label: 'Last 6 hours', value: '6h', ms: 6 * 60 * 60 * 1000 },
  { label: 'Last 24 hours', value: '24h', ms: 24 * 60 * 60 * 1000 },
  { label: 'Last 7 days', value: '7d', ms: 7 * 24 * 60 * 60 * 1000 },
] as const

function timePresetToNanos(preset: string): { start: number; end: number } {
  const found = TIME_PRESETS.find((p) => p.value === preset)
  if (!found) return { start: 0, end: 0 }
  const now = Date.now()
  return {
    start: (now - found.ms) * 1_000_000,
    end: now * 1_000_000,
  }
}

export function SearchPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()

  // Read filter state from URL params
  const service = searchParams.get('service') ?? ''
  const spanName = searchParams.get('spanName') ?? ''
  const minDuration = searchParams.get('minDuration') ?? ''
  const maxDuration = searchParams.get('maxDuration') ?? ''
  const timeRange = searchParams.get('timeRange') ?? '1h'

  // Build query for the API
  const query = useMemo<TraceQuery>(() => {
    const q: TraceQuery = { limit: 50 }
    if (service) q.service = service
    if (spanName) q.spanName = spanName
    if (minDuration) q.minDuration = Number(minDuration)
    if (maxDuration) q.maxDuration = Number(maxDuration)
    if (timeRange) {
      const { start, end } = timePresetToNanos(timeRange)
      if (start > 0) {
        q.start = start
        q.end = end
      }
    }
    return q
  }, [service, spanName, minDuration, maxDuration, timeRange])

  const { data: services } = useServices()
  const { data: traces, isLoading, error } = useTraces(query)

  // Update a single filter param while preserving others
  const setFilter = useCallback(
    (key: string, value: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value) {
            next.set(key, value)
          } else {
            next.delete(key)
          }
          return next
        },
        { replace: true }
      )
    },
    [setSearchParams]
  )

  const clearFilters = useCallback(() => {
    setSearchParams({}, { replace: true })
  }, [setSearchParams])

  const hasActiveFilters = service || spanName || minDuration || maxDuration

  return (
    <div className="space-y-6 p-6">
      <PageHeader title="Search Traces" />

      {/* Search Filters */}
      <div className="space-y-4 rounded-lg border bg-card p-4">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {/* Service Select */}
          <div className="space-y-1.5">
            <label
              htmlFor="service-select"
              className="text-sm font-medium text-muted-foreground"
            >
              Service
            </label>
            <Select
              value={service}
              onValueChange={(val) => setFilter('service', val as string)}
            >
              <SelectTrigger
                id="service-select"
                className="w-full"
                aria-label="Filter by service"
              >
                <SelectValue placeholder="All services" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">All services</SelectItem>
                {services?.map((s) => (
                  <SelectItem key={s.name} value={s.name}>
                    {s.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Span Name Input */}
          <div className="space-y-1.5">
            <label
              htmlFor="span-name"
              className="text-sm font-medium text-muted-foreground"
            >
              Span Name
            </label>
            <Input
              id="span-name"
              placeholder="e.g. GET /api/users"
              value={spanName}
              onChange={(e) => setFilter('spanName', e.target.value)}
            />
          </div>

          {/* Duration Range */}
          <div className="space-y-1.5">
            <span className="text-sm font-medium text-muted-foreground">
              Duration (ms)
            </span>
            <div className="flex items-center gap-2">
              <Input
                type="number"
                placeholder="Min"
                min={0}
                value={minDuration}
                onChange={(e) => setFilter('minDuration', e.target.value)}
                aria-label="Minimum duration in milliseconds"
              />
              <span className="text-muted-foreground">-</span>
              <Input
                type="number"
                placeholder="Max"
                min={0}
                value={maxDuration}
                onChange={(e) => setFilter('maxDuration', e.target.value)}
                aria-label="Maximum duration in milliseconds"
              />
            </div>
          </div>

          {/* Time Range */}
          <div className="space-y-1.5">
            <label
              htmlFor="time-range"
              className="text-sm font-medium text-muted-foreground"
            >
              Time Range
            </label>
            <Select
              value={timeRange}
              onValueChange={(val) => setFilter('timeRange', val as string)}
            >
              <SelectTrigger
                id="time-range"
                className="w-full"
                aria-label="Filter by time range"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TIME_PRESETS.map((preset) => (
                  <SelectItem key={preset.value} value={preset.value}>
                    {preset.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        {hasActiveFilters && (
          <div className="flex justify-end">
            <Button variant="ghost" size="sm" onClick={clearFilters}>
              Clear filters
            </Button>
          </div>
        )}
      </div>

      {/* Results */}
      {isLoading && (
        <div className="flex items-center justify-center py-12 text-muted-foreground">
          Searching traces...
        </div>
      )}

      {error && (
        <div
          className="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
          role="alert"
        >
          Failed to load traces: {error.message}
        </div>
      )}

      {traces && traces.length === 0 && !isLoading && (
        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <p className="text-lg font-medium">No traces found</p>
          <p className="text-sm">
            Try adjusting your filters or sending some traces.
          </p>
        </div>
      )}

      {traces && traces.length > 0 && (
        <TraceList traces={traces} navigate={navigate} />
      )}
    </div>
  )
}

// --- TraceList ---

interface TraceListProps {
  traces: TraceSummary[]
  navigate: ReturnType<typeof useNavigate>
}

function TraceList({ traces, navigate }: TraceListProps) {
  const maxDuration = useMemo(
    () => Math.max(...traces.map((t) => t.durationMs)),
    [traces]
  )

  return (
    <div className="space-y-1">
      <div className="hidden items-center gap-4 px-4 py-2 text-xs font-medium text-muted-foreground sm:grid sm:grid-cols-[1fr_1fr_auto_minmax(120px,1fr)_auto_auto_auto]">
        <span>Trace ID</span>
        <span>Root Span</span>
        <span>Service</span>
        <span>Duration</span>
        <span>Spans</span>
        <span>Status</span>
        <span>Time</span>
      </div>
      {traces.map((trace) => (
        <TraceRow
          key={trace.traceId}
          trace={trace}
          maxDuration={maxDuration}
          onClick={() => navigate(`/traces/${trace.traceId}`)}
        />
      ))}
    </div>
  )
}

// --- TraceRow ---

interface TraceRowProps {
  trace: TraceSummary
  maxDuration: number
  onClick: () => void
}

function TraceRow({ trace, maxDuration, onClick }: TraceRowProps) {
  return (
    <div
      className="grid cursor-pointer items-center gap-4 rounded-lg border bg-card px-4 py-3 transition-colors hover:bg-muted/50 sm:grid-cols-[1fr_1fr_auto_minmax(120px,1fr)_auto_auto_auto]"
      role="link"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onClick()
        }
      }}
    >
      {/* Trace ID - truncated, monospace */}
      <span className="font-mono text-sm" title={trace.traceId}>
        {trace.traceId.slice(0, 8)}
      </span>

      {/* Root Span Name */}
      <span className="truncate text-sm font-medium" title={trace.rootSpan}>
        {trace.rootSpan}
      </span>

      {/* Service Badge */}
      <ServiceBadge name={trace.serviceName} />

      {/* Duration Bar + value */}
      <div className="flex items-center gap-2">
        <div className="min-w-0 flex-1">
          <DurationBar
            duration={trace.durationMs}
            totalDuration={maxDuration}
          />
        </div>
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
          {trace.durationMs.toFixed(1)}ms
        </span>
      </div>

      {/* Span Count */}
      <span className="text-xs tabular-nums text-muted-foreground">
        {trace.spanCount} {trace.spanCount === 1 ? 'span' : 'spans'}
      </span>

      {/* Error Badge */}
      <span className="min-w-[60px]">
        <ErrorBadge count={trace.errorCount} />
      </span>

      {/* Timestamp */}
      <span className="text-xs text-muted-foreground">
        <TimeDisplay nanos={trace.startTime} />
      </span>
    </div>
  )
}
