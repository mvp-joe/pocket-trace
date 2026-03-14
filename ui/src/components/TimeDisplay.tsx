import { formatDistanceToNow, format } from "date-fns"

interface TimeDisplayProps {
  nanos: number
}

function nanosToDate(nanos: number): Date {
  return new Date(nanos / 1_000_000)
}

export function TimeDisplay({ nanos }: TimeDisplayProps) {
  const date = nanosToDate(nanos)
  const absolute = format(date, "yyyy-MM-dd HH:mm:ss.SSS")
  const relative = formatDistanceToNow(date, { addSuffix: true })

  return (
    <time dateTime={date.toISOString()} title={absolute}>
      {relative}
    </time>
  )
}
