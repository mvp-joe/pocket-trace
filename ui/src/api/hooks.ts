import { useQuery } from '@tanstack/react-query'
import {
  getDependencies,
  getServices,
  getStatus,
  getTrace,
  getTraces,
} from './client.ts'
import type { TraceQuery } from './types.ts'

export function useServices() {
  return useQuery({
    queryKey: ['services'],
    queryFn: getServices,
  })
}

export function useTraces(filters: TraceQuery) {
  return useQuery({
    queryKey: ['traces', filters],
    queryFn: () => getTraces(filters),
  })
}

export function useTrace(traceId: string) {
  return useQuery({
    queryKey: ['trace', traceId],
    queryFn: () => getTrace(traceId),
    enabled: !!traceId,
  })
}

export function useDependencies(lookback?: string) {
  return useQuery({
    queryKey: ['dependencies', lookback],
    queryFn: () => getDependencies(lookback),
  })
}

export function useStatus() {
  return useQuery({
    queryKey: ['status'],
    queryFn: getStatus,
    refetchInterval: 5000,
  })
}
