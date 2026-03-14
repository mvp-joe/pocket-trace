import { useState, useCallback, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useTrace } from '@/api/hooks.ts'
import type { SpanNode } from '@/api/types.ts'
import { PageHeader } from '@/components/PageHeader.tsx'
import { CopyButton } from '@/components/CopyButton.tsx'
import { ErrorBadge } from '@/components/ErrorBadge.tsx'
import { Button } from '@/components/ui/button.tsx'
import { ArrowLeft } from 'lucide-react'
import { TimeRuler } from './trace/TimeRuler.tsx'
import { SpanTree } from './trace/SpanTree.tsx'
import { SpanDetail } from './trace/SpanDetail.tsx'

/** Recursively find a SpanNode by spanId in the tree */
function findSpan(nodes: SpanNode[], spanId: string): SpanNode | undefined {
  for (const node of nodes) {
    if (node.spanId === spanId) return node
    const found = findSpan(node.children, spanId)
    if (found) return found
  }
  return undefined
}

export function TracePage() {
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()
  const { data: trace, isLoading, error } = useTrace(traceId ?? '')

  const [collapsedSpans, setCollapsedSpans] = useState<Set<string>>(new Set())
  const [selectedSpanId, setSelectedSpanId] = useState<string | null>(null)

  const handleToggle = useCallback((spanId: string) => {
    setCollapsedSpans((prev) => {
      const next = new Set(prev)
      if (next.has(spanId)) {
        next.delete(spanId)
      } else {
        next.add(spanId)
      }
      return next
    })
  }, [])

  const handleSelect = useCallback((spanId: string) => {
    setSelectedSpanId((prev) => (prev === spanId ? null : spanId))
  }, [])

  const handleCloseDetail = useCallback(() => {
    setSelectedSpanId(null)
  }, [])

  // Compute the trace start time from the earliest root span
  const traceStartTime = useMemo(() => {
    if (!trace || trace.roots.length === 0) return 0
    return Math.min(...trace.roots.map((r) => r.startTime))
  }, [trace])

  // Find the selected span node in the tree
  const selectedSpan = useMemo(() => {
    if (!selectedSpanId || !trace) return null
    return findSpan(trace.roots, selectedSpanId) ?? null
  }, [selectedSpanId, trace])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center p-6 py-24 text-muted-foreground">
        Loading trace...
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6">
        <div
          className="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
          role="alert"
        >
          Failed to load trace: {error.message}
        </div>
      </div>
    )
  }

  if (!trace) {
    return (
      <div className="flex items-center justify-center p-6 py-24 text-muted-foreground">
        Trace not found
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="shrink-0 p-6 pb-4">
        <PageHeader title="Trace">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigate(-1)}
            aria-label="Go back"
          >
            <ArrowLeft className="size-4" />
            Back
          </Button>
        </PageHeader>

        {/* Trace metadata row */}
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1">
          <div className="flex items-center gap-1">
            <span className="font-mono text-sm text-muted-foreground" title={trace.traceId}>
              {trace.traceId}
            </span>
            <CopyButton text={trace.traceId} />
          </div>
          <span className="text-sm tabular-nums text-muted-foreground">
            {trace.durationMs.toFixed(1)}ms
          </span>
          <span className="text-sm text-muted-foreground">
            {trace.spanCount} {trace.spanCount === 1 ? 'span' : 'spans'}
          </span>
          <span className="text-sm text-muted-foreground">
            {trace.serviceCount} {trace.serviceCount === 1 ? 'service' : 'services'}
          </span>
          {trace.errorCount > 0 && <ErrorBadge count={trace.errorCount} />}
        </div>
      </div>

      {/* Main content: timeline + optional detail panel */}
      <div className="flex min-h-0 flex-1">
        {/* Timeline / waterfall */}
        <div className="min-w-0 flex-1 overflow-auto border-t border-border">
          {/* Time ruler header - aligned with duration bar column */}
          <div
            className="sticky top-0 z-10 grid bg-background"
            style={{ gridTemplateColumns: 'minmax(200px, 40%) 1fr' }}
          >
            <div className="border-b border-border" />
            <div className="pr-[88px]">
              <TimeRuler durationMs={trace.durationMs} />
            </div>
          </div>

          {/* Span tree */}
          <SpanTree
            roots={trace.roots}
            traceStartTime={traceStartTime}
            traceDurationMs={trace.durationMs}
            collapsedSpans={collapsedSpans}
            selectedSpanId={selectedSpanId}
            onToggle={handleToggle}
            onSelect={handleSelect}
          />
        </div>

        {/* Detail panel - shown when a span is selected */}
        {selectedSpan && (
          <div className="w-[380px] shrink-0 overflow-hidden">
            <SpanDetail span={selectedSpan} onClose={handleCloseDetail} />
          </div>
        )}
      </div>
    </div>
  )
}
