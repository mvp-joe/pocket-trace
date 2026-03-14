import type { SpanNode } from '@/api/types.ts'
import { SpanRow } from './SpanRow.tsx'

interface SpanTreeProps {
  roots: SpanNode[]
  traceStartTime: number // nanos
  traceDurationMs: number
  collapsedSpans: Set<string>
  selectedSpanId: string | null
  onToggle: (spanId: string) => void
  onSelect: (spanId: string) => void
}

export function SpanTree({
  roots,
  traceStartTime,
  traceDurationMs,
  collapsedSpans,
  selectedSpanId,
  onToggle,
  onSelect,
}: SpanTreeProps) {
  return (
    <div role="treegrid" aria-label="Span tree">
      {roots.map((root) => (
        <SpanRow
          key={root.spanId}
          node={root}
          depth={0}
          traceStartTime={traceStartTime}
          traceDurationMs={traceDurationMs}
          isCollapsed={collapsedSpans.has(root.spanId)}
          isSelected={selectedSpanId === root.spanId}
          onToggle={onToggle}
          onSelect={onSelect}
          collapsedSpans={collapsedSpans}
          selectedSpanId={selectedSpanId}
        />
      ))}
    </div>
  )
}
