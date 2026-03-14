import { Badge } from "@/components/ui/badge"

interface ErrorBadgeProps {
  count: number
}

export function ErrorBadge({ count }: ErrorBadgeProps) {
  if (count <= 0) return null

  return (
    <Badge variant="destructive">
      {count} {count === 1 ? "error" : "errors"}
    </Badge>
  )
}
