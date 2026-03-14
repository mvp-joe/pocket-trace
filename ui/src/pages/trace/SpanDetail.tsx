import type { SpanNode } from '@/api/types.ts'
import { StatusBadge } from '@/components/StatusBadge.tsx'
import { ServiceBadge } from '@/components/ServiceBadge.tsx'
import { Button } from '@/components/ui/button.tsx'
import { Separator } from '@/components/ui/separator.tsx'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table.tsx'
import { X } from 'lucide-react'

interface SpanDetailProps {
  span: SpanNode
  onClose: () => void
}

function formatValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return JSON.stringify(value)
}

function formatEventTime(eventTimeNanos: number, spanStartNanos: number): string {
  const diffMs = (eventTimeNanos - spanStartNanos) / 1_000_000
  if (diffMs < 0.01) return '0ms'
  if (diffMs < 1) return `${(diffMs * 1000).toFixed(0)}us`
  if (diffMs < 1000) return `+${diffMs.toFixed(1)}ms`
  return `+${(diffMs / 1000).toFixed(2)}s`
}

export function SpanDetail({ span, onClose }: SpanDetailProps) {
  const attributes = span.attributes ?? {}
  const attributeEntries = Object.entries(attributes)
  const events = span.events ?? []

  return (
    <div
      className="flex h-full flex-col border-l border-border bg-background"
      role="complementary"
      aria-label="Span details"
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-2 border-b border-border p-4">
        <div className="min-w-0 space-y-1">
          <h3 className="truncate text-sm font-semibold" title={span.spanName}>
            {span.spanName}
          </h3>
          <div className="flex flex-wrap items-center gap-2">
            <ServiceBadge name={span.serviceName} />
            <StatusBadge status={span.statusCode} />
            <span className="text-xs tabular-nums text-muted-foreground">
              {span.durationMs.toFixed(1)}ms
            </span>
          </div>
          {span.statusMessage && (
            <p className="text-xs text-red-500">{span.statusMessage}</p>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={onClose}
          aria-label="Close span details"
        >
          <X />
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {/* Identifiers */}
        <div className="space-y-1 p-4">
          <h4 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Identifiers
          </h4>
          <div className="space-y-0.5 text-xs">
            <div className="flex gap-2">
              <span className="shrink-0 text-muted-foreground">Span ID</span>
              <span className="font-mono">{span.spanId}</span>
            </div>
            {span.parentSpanId && (
              <div className="flex gap-2">
                <span className="shrink-0 text-muted-foreground">Parent</span>
                <span className="font-mono">{span.parentSpanId}</span>
              </div>
            )}
          </div>
        </div>

        <Separator />

        {/* Attributes */}
        <div className="p-4">
          <h4 className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Attributes
          </h4>
          {attributeEntries.length === 0 ? (
            <p className="text-xs text-muted-foreground">No attributes</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="h-7 text-xs">Key</TableHead>
                  <TableHead className="h-7 text-xs">Value</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {attributeEntries.map(([key, value]) => (
                  <TableRow key={key}>
                    <TableCell className="py-1 font-mono text-xs text-muted-foreground">
                      {key}
                    </TableCell>
                    <TableCell className="max-w-[200px] truncate py-1 font-mono text-xs">
                      {formatValue(value)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>

        {/* Events */}
        {events.length > 0 && (
          <>
            <Separator />
            <div className="p-4">
              <h4 className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Events ({events.length})
              </h4>
              <div className="space-y-3">
                {events.map((event, index) => (
                  <div key={index} className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-medium">{event.name}</span>
                      <span className="text-[10px] tabular-nums text-muted-foreground">
                        {formatEventTime(event.time, span.startTime)}
                      </span>
                    </div>
                    {event.attributes &&
                      Object.keys(event.attributes).length > 0 && (
                        <div className="ml-3 space-y-0.5 border-l border-border/50 pl-2">
                          {Object.entries(event.attributes).map(
                            ([key, value]) => (
                              <div
                                key={key}
                                className="flex gap-1.5 text-[11px]"
                              >
                                <span className="shrink-0 text-muted-foreground">
                                  {key}:
                                </span>
                                <span className="truncate font-mono">
                                  {formatValue(value)}
                                </span>
                              </div>
                            )
                          )}
                        </div>
                      )}
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
