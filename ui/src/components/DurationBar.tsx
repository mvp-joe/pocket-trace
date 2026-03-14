import { cn } from "@/lib/utils"

interface DurationBarProps {
  duration: number
  totalDuration: number
  offset?: number
}

export function DurationBar({ duration, totalDuration, offset }: DurationBarProps) {
  if (totalDuration <= 0) return null

  const widthPercent = Math.max(0.5, (duration / totalDuration) * 100)
  const offsetPercent = offset != null ? (offset / totalDuration) * 100 : 0

  return (
    <div
      className={cn(
        "relative h-5 w-full rounded-sm bg-muted",
        offset == null && "bg-transparent"
      )}
      role="img"
      aria-label={`Duration: ${duration.toFixed(1)}ms`}
    >
      <div
        className="absolute top-0 h-full rounded-sm bg-blue-500/70 dark:bg-blue-400/60"
        style={{
          left: `${offsetPercent}%`,
          width: `${widthPercent}%`,
        }}
      />
    </div>
  )
}
