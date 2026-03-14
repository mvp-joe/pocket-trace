import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { RootLayout } from '@/components/layout/RootLayout.tsx'
import { ServicesPage } from '@/pages/ServicesPage.tsx'
import { SearchPage } from '@/pages/SearchPage.tsx'
import { TracePage } from '@/pages/TracePage.tsx'
import { DependenciesPage } from '@/pages/DependenciesPage.tsx'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
})

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<RootLayout />}>
            <Route index element={<Navigate to="/services" replace />} />
            <Route path="services" element={<ServicesPage />} />
            <Route path="search" element={<SearchPage />} />
            <Route path="traces/:traceId" element={<TracePage />} />
            <Route path="dependencies" element={<DependenciesPage />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

export default App
