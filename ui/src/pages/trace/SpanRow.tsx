import type { SpanNode } from '@/api/types.ts'
import { ServiceBadge } from '@/components/ServiceBadge.tsx'
import { DurationBar } from '@/components/DurationBar.tsx'
import { CheckCircle2, AlertCircle } from 'lucide-react'
import { CollapseToggle } from './CollapseToggle.tsx'

interface SpanRowProps {
  node: SpanNode
  depth: number
  traceStartTime: number // nanos
  traceDurationMs: number
  isCollapsed: boolean
  isSelected: boolean
  onToggle: (spanId: string) => void
  onSelect: (spanId: string) => void
  collapsedSpans: Set<string>
  selectedSpanId: string | null
}

export function SpanRow({
  node,
  depth,
  traceStartTime,
  traceDurationMs,
  isCollapsed,
  isSelected,
  onToggle,
  onSelect,
  collapsedSpans,
  selectedSpanId,
}: SpanRowProps) {
  const hasChildren = node.children.length > 0
  const offsetMs = (node.startTime - traceStartTime) / 1_000_000
  const isError = node.statusCode === 'ERROR'

  return (
    <>
      <div
        className={`group grid cursor-pointer items-center gap-1 border-b border-border/50 px-2 py-1 transition-colors hover:bg-muted/50 ${
          isSelected ? 'bg-muted' : ''
        }`}
        style={{
          gridTemplateColumns: 'minmax(200px, 40%) 1fr',
        }}
        role="row"
        tabIndex={0}
        aria-selected={isSelected}
        onClick={() => onSelect(node.spanId)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onSelect(node.spanId)
          }
        }}
      >
        {/* Left side: indent + toggle + service badge + span name */}
        <div className="flex min-w-0 items-center gap-1.5">
          {/* Indent based on depth */}
          {depth > 0 && (
            <span
              className="shrink-0"
              style={{ width: `${depth * 16}px` }}
              aria-hidden="true"
            />
          )}
          <CollapseToggle
            hasChildren={hasChildren}
            isCollapsed={isCollapsed}
            onToggle={() => onToggle(node.spanId)}
          />
          <ServiceBadge name={node.serviceName} />
          <span
            className="truncate text-sm font-medium"
            title={node.spanName}
          >
            {node.spanName}
          </span>
        </div>

        {/* Right side: duration bar + duration label + status icon */}
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <DurationBar
              duration={node.durationMs}
              totalDuration={traceDurationMs}
              offset={offsetMs}
            />
          </div>
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
            {node.durationMs.toFixed(1)}ms
          </span>
          {isError ? (
            <AlertCircle
              className="size-3.5 shrink-0 text-red-500"
              aria-label="Error"
            />
          ) : (
            <CheckCircle2
              className="size-3.5 shrink-0 text-green-500"
              aria-label="OK"
            />
          )}
        </div>
      </div>

      {/* Render children recursively when not collapsed */}
      {hasChildren &&
        !isCollapsed &&
        node.children.map((child) => (
          <SpanRow
            key={child.spanId}
            node={child}
            depth={depth + 1}
            traceStartTime={traceStartTime}
            traceDurationMs={traceDurationMs}
            isCollapsed={collapsedSpans.has(child.spanId)}
            isSelected={selectedSpanId === child.spanId}
            onToggle={onToggle}
            onSelect={onSelect}
            collapsedSpans={collapsedSpans}
            selectedSpanId={selectedSpanId}
          />
        ))}
    </>
  )
}
