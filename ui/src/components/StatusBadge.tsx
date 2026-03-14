import { Badge } from "@/components/ui/badge"

const STATUS_STYLES: Record<string, string> = {
  OK: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300",
  ERROR:
    "bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300",
  UNSET:
    "bg-gray-100 text-gray-600 dark:bg-gray-800/30 dark:text-gray-400",
}

interface StatusBadgeProps {
  status: string
}

export function StatusBadge({ status }: StatusBadgeProps) {
  const normalized = status.toUpperCase()
  const styles = STATUS_STYLES[normalized] ?? STATUS_STYLES.UNSET

  return (
    <Badge variant="outline" className={`border-transparent ${styles}`}>
      {normalized}
    </Badge>
  )
}
