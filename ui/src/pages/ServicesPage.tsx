import { useNavigate } from 'react-router-dom'
import { useServices } from '@/api/hooks.ts'
import { PageHeader } from '@/components/PageHeader.tsx'
import { TimeDisplay } from '@/components/TimeDisplay.tsx'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card.tsx'

export function ServicesPage() {
  const { data: services, isLoading, error } = useServices()
  const navigate = useNavigate()

  return (
    <div className="space-y-6 p-6">
      <PageHeader title="Services" />

      {isLoading && (
        <div className="flex items-center justify-center py-12 text-muted-foreground">
          Loading services...
        </div>
      )}

      {error && (
        <div
          className="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive"
          role="alert"
        >
          Failed to load services: {error.message}
        </div>
      )}

      {services && services.length === 0 && (
        <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <p className="text-lg font-medium">No services found</p>
          <p className="text-sm">
            Send some traces to see services appear here.
          </p>
        </div>
      )}

      {services && services.length > 0 && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {services.map((service) => (
            <Card
              key={service.name}
              className="cursor-pointer transition-shadow hover:ring-2 hover:ring-ring/50"
              role="link"
              tabIndex={0}
              onClick={() =>
                navigate(`/search?service=${encodeURIComponent(service.name)}`)
              }
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  navigate(
                    `/search?service=${encodeURIComponent(service.name)}`
                  )
                }
              }}
            >
              <CardHeader>
                <CardTitle>{service.name}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex items-center justify-between text-sm text-muted-foreground">
                  <span>
                    {service.spanCount.toLocaleString()}{' '}
                    {service.spanCount === 1 ? 'span' : 'spans'}
                  </span>
                  <TimeDisplay nanos={service.lastSeen} />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
