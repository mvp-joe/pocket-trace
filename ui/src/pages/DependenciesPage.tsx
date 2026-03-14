import { useState } from 'react'
import { useDependencies } from '@/api/hooks.ts'
import type { Dependency } from '@/api/types.ts'
import { PageHeader } from '@/components/PageHeader.tsx'
import { ServiceBadge } from '@/components/ServiceBadge.tsx'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select.tsx'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table.tsx'

// --- Lookback presets ---

const LOOKBACK_OPTIONS = [
  { label: 'Last 1 hour', value: '1h' },
  { label: 'Last 6 hours', value: '6h' },
  { label: 'Last 24 hours', value: '24h' },
  { label: 'Last 7 days', value: '7d' },
] as const

// --- LookbackSelect ---

interface LookbackSelectProps {
  value: string
  onValueChange: (value: string) => void
}

function LookbackSelect({ value, onValueChange }: LookbackSelectProps) {
  return (
    <div className="flex items-center gap-2">
      <label
        htmlFor="lookback-select"
        className="text-sm font-medium text-muted-foreground"
      >
        Lookback
      </label>
      <Select value={value} onValueChange={(val) => onValueChange(val as string)}>
        <SelectTrigger id="lookback-select" aria-label="Select lookback period">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {LOOKBACK_OPTIONS.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

// --- DependencyGraph (table view for MVP) ---

interface DependencyGraphProps {
  dependencies: Dependency[]
}

function DependencyGraph({ dependencies }: DependencyGraphProps) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Parent</TableHead>
          <TableHead className="w-12 text-center" aria-hidden="true" />
          <TableHead>Child</TableHead>
          <TableHead className="text-right">Call Count</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {dependencies.map((dep) => (
          <TableRow key={`${dep.parent}-${dep.child}`}>
            <TableCell>
              <ServiceBadge name={dep.parent} />
            </TableCell>
            <TableCell className="text-center text-muted-foreground" aria-label="calls">
              <span aria-hidden="true">{'\u2192'}</span>
            </TableCell>
            <TableCell>
              <ServiceBadge name={dep.child} />
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {dep.callCount.toLocaleString()}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

// --- DependenciesPage ---

export function DependenciesPage() {
  const [lookback, setLookback] = useState('1h')
  const { data: dependencies, isLoading, error } = useDependencies(lookback)

  return (
    <div className="space-y-6 p-6">
      <PageHeader title="Dependencies">
        <LookbackSelect value={lookback} onValueChange={setLookback} />
      </PageHeader>

      {isLoading && (
        <div className="flex items-center justify-center py-12 text-muted-foreground">
          Loading dependencies...
        </div>
      )}

      {error && (
        <div
          className="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
          role="alert"
        >
          Failed to load dependencies: {error.message}
        </div>
      )}

      {dependencies && dependencies.length === 0 && !isLoading && (
        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <p className="text-lg font-medium">No dependencies found</p>
          <p className="text-sm">
            Send traces with multiple services to see dependencies here.
          </p>
        </div>
      )}

      {dependencies && dependencies.length > 0 && (
        <DependencyGraph dependencies={dependencies} />
      )}
    </div>
  )
}
