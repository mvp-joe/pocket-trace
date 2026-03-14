import { ChevronRight } from 'lucide-react'

interface CollapseToggleProps {
  hasChildren: boolean
  isCollapsed: boolean
  onToggle: () => void
}

export function CollapseToggle({
  hasChildren,
  isCollapsed,
  onToggle,
}: CollapseToggleProps) {
  if (!hasChildren) {
    // Reserve space for alignment
    return <span className="inline-block w-4" aria-hidden="true" />
  }

  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onToggle()
      }}
      className="inline-flex size-4 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      aria-label={isCollapsed ? 'Expand children' : 'Collapse children'}
      aria-expanded={!isCollapsed}
    >
      <ChevronRight
        className={`size-3.5 transition-transform ${isCollapsed ? '' : 'rotate-90'}`}
      />
    </button>
  )
}
