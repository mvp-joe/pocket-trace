import { NavLink } from 'react-router-dom'
import { Network, Search, Server } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils.ts'
import { useStatus } from '@/api/hooks.ts'

function Logo() {
  return (
    <div className="flex items-center gap-2 px-3 py-4">
      <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
        <span className="text-sm font-bold">pt</span>
      </div>
      <span className="text-lg font-semibold tracking-tight">pocket-trace</span>
    </div>
  )
}

interface NavItemProps {
  to: string
  icon: LucideIcon
  label: string
}

function NavItem({ to, icon: Icon, label }: NavItemProps) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
          isActive
            ? 'bg-accent text-accent-foreground'
            : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
        )
      }
    >
      <Icon className="h-4 w-4 shrink-0" />
      {label}
    </NavLink>
  )
}

function DaemonStatus() {
  const { data, isError, isLoading } = useStatus()

  let statusColor = 'bg-muted-foreground' // unknown / loading
  let statusText = 'Checking...'

  if (isError) {
    statusColor = 'bg-destructive'
    statusText = 'Disconnected'
  } else if (data) {
    statusColor = 'bg-green-500'
    statusText = `${data.db.spanCount.toLocaleString()} spans`
  }

  return (
    <div className="border-t border-border px-3 py-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span
          className={cn('h-2 w-2 shrink-0 rounded-full', statusColor)}
          aria-hidden="true"
        />
        <span className={cn(isLoading && 'animate-pulse')}>
          {statusText}
        </span>
      </div>
    </div>
  )
}

export function Sidebar() {
  return (
    <aside className="flex h-screen w-60 shrink-0 flex-col border-r border-border bg-sidebar text-sidebar-foreground">
      <Logo />
      <nav className="flex flex-1 flex-col gap-1 px-2" aria-label="Main navigation">
        <NavItem to="/services" icon={Server} label="Services" />
        <NavItem to="/search" icon={Search} label="Search" />
        <NavItem to="/dependencies" icon={Network} label="Dependencies" />
      </nav>
      <DaemonStatus />
    </aside>
  )
}
