interface TimeRulerProps {
  durationMs: number
}

function formatTickLabel(ms: number): string {
  if (ms === 0) return '0ms'
  if (ms < 1) return `${(ms * 1000).toFixed(0)}us`
  if (ms < 1000) return `${ms.toFixed(ms < 10 ? 1 : 0)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function TimeRuler({ durationMs }: TimeRulerProps) {
  const tickCount = 5
  const ticks = Array.from({ length: tickCount }, (_, i) => ({
    percent: (i / (tickCount - 1)) * 100,
    label: formatTickLabel((i / (tickCount - 1)) * durationMs),
  }))

  return (
    <div
      className="relative h-6 w-full border-b border-border text-[10px] text-muted-foreground"
      aria-hidden="true"
    >
      {ticks.map((tick) => (
        <span
          key={tick.percent}
          className="absolute -translate-x-1/2 tabular-nums"
          style={{ left: `${tick.percent}%` }}
        >
          {tick.label}
        </span>
      ))}
    </div>
  )
}
