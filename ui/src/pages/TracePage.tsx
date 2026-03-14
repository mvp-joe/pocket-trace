import { useParams } from 'react-router-dom'

export function TracePage() {
  const { traceId } = useParams<{ traceId: string }>()

  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold">Trace</h1>
      <p className="mt-2 font-mono text-sm text-muted-foreground">{traceId}</p>
    </div>
  )
}
