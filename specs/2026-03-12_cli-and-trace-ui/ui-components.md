# UI Components

## Technology

- Vite + React 19 + TypeScript
- Tailwind CSS v4
- shadcn/ui component library
- TanStack React Query for data fetching and caching
- react-router-dom for client-side routing
- lucide-react for icons
- date-fns for date formatting

## Page Hierarchy

```
<App>
  <QueryClientProvider>
    <RouterProvider>
      <RootLayout>
        <Sidebar>
          <Logo />
          <Navigation>
            <NavItem to="/services" icon={Server} label="Services" />
            <NavItem to="/search" icon={Search} label="Search" />
            <NavItem to="/dependencies" icon={Network} label="Dependencies" />
          </Navigation>
          <DaemonStatus />   <!-- status indicator, spans/s -->
        </Sidebar>
        <MainContent>
          <Outlet />   <!-- page content renders here -->
        </MainContent>
      </RootLayout>
    </RouterProvider>
  </QueryClientProvider>
</App>
```

## Pages

### ServicesPage (`/services`)

Lists all services with span counts and last-seen timestamps.

```
<ServicesPage>
  <PageHeader title="Services" />
  <ServiceList>
    <ServiceCard>          <!-- one per service, clickable -->
      <ServiceName />
      <SpanCount />
      <LastSeen />         <!-- relative time, e.g. "2m ago" -->
    </ServiceCard>
  </ServiceList>
</ServicesPage>
```

- **Data:** `GET /api/services` via TanStack Query
- **Interaction:** Clicking a service navigates to `/search?service={name}`

### SearchPage (`/search`)

Search and filter traces across services.

```
<SearchPage>
  <PageHeader title="Search Traces" />
  <SearchFilters>
    <ServiceSelect />       <!-- dropdown from /api/services -->
    <SpanNameInput />       <!-- text input, substring match -->
    <DurationRange>
      <MinDurationInput />
      <MaxDurationInput />
    </DurationRange>
    <TimeRange>
      <StartTimePicker />
      <EndTimePicker />
    </TimeRange>
    <SearchButton />
  </SearchFilters>
  <TraceList>
    <TraceRow>              <!-- one per trace, clickable -->
      <TraceID />           <!-- truncated, monospace -->
      <RootSpanName />
      <ServiceBadge />
      <DurationBar />       <!-- visual bar relative to max -->
      <SpanCount />
      <ErrorBadge />        <!-- red badge if errors > 0 -->
      <Timestamp />         <!-- relative time -->
    </TraceRow>
  </TraceList>
</SearchPage>
```

- **Data:** `GET /api/traces` with query params, via TanStack Query
- **Interaction:** Clicking a trace navigates to `/traces/{traceID}`
- **URL State:** Filter values are synced to URL query params for shareability

### TracePage (`/traces/:traceID`)

Full trace detail with waterfall visualization.

```
<TracePage>
  <PageHeader>
    <BackButton />
    <TraceID />             <!-- full trace ID, copyable -->
    <TraceDuration />
    <SpanCount />
  </PageHeader>
  <TraceTimeline>
    <TimelineHeader>
      <TimeRuler />         <!-- time markers across the top -->
    </TimelineHeader>
    <SpanTree>              <!-- renders pre-built tree from API (roots[].children) -->
      <SpanRow depth={0}>
        <SpanIndent />      <!-- indent based on tree depth -->
        <CollapseToggle />  <!-- expand/collapse children -->
        <ServiceBadge />
        <SpanName />
        <DurationBar />     <!-- positioned relative to trace start -->
        <DurationLabel />
        <StatusIcon />      <!-- checkmark or error icon -->
      </SpanRow>
      <SpanRow depth={1}>
        <!-- child spans, recursively -->
      </SpanRow>
    </SpanTree>
  </TraceTimeline>
  <SpanDetail>              <!-- panel shown when a span row is selected -->
    <SpanDetailHeader>
      <SpanName />
      <ServiceName />
      <Duration />
      <StatusBadge />
    </SpanDetailHeader>
    <SpanAttributes>        <!-- key-value table from attributes JSON -->
      <AttributeRow key="" value="" />
    </SpanAttributes>
    <SpanEvents>            <!-- timeline of events within the span -->
      <EventRow>
        <EventName />
        <EventTime />       <!-- relative to span start -->
        <EventAttributes />
      </EventRow>
    </SpanEvents>
  </SpanDetail>
</TracePage>
```

- **Data:** `GET /api/traces/:traceID` via TanStack Query. Returns a `TraceDetail` with pre-built trees (`roots[]`, each with nested `children`). Typically one root; multiple only when orphan spans exist. Tree building is done server-side — the UI renders the tree directly, no client-side assembly needed.
- **Interaction:** Clicking a `SpanRow` opens the `SpanDetail` panel. Collapse toggles hide/show children.

### DependenciesPage (`/dependencies`)

Service dependency graph visualization.

```
<DependenciesPage>
  <PageHeader title="Dependencies" />
  <LookbackSelect />        <!-- 1h, 6h, 24h, 7d -->
  <DependencyGraph>         <!-- canvas or SVG graph -->
    <ServiceNode />         <!-- circle/box per service -->
    <DependencyEdge />      <!-- arrow with call count label -->
  </DependencyGraph>
</DependenciesPage>
```

- **Data:** `GET /api/dependencies?lookback=1h` via TanStack Query
- **Rendering:** Force-directed or hierarchical layout. For MVP, a simple grid/list showing parent->child relationships with call counts is acceptable; a proper graph visualization can follow.

## Shared Components

### Navigation Components

| Component      | Props                                          | Responsibility                        |
|----------------|------------------------------------------------|---------------------------------------|
| `RootLayout`   | none (uses Outlet)                             | Sidebar + main content shell          |
| `Sidebar`      | none                                           | Navigation container                  |
| `NavItem`      | `to: string, icon: LucideIcon, label: string` | Navigation link with active state     |
| `DaemonStatus` | none                                           | Fetches `/api/status`, shows health   |

### Display Components

| Component       | Props                                    | Responsibility                           |
|-----------------|------------------------------------------|------------------------------------------|
| `PageHeader`    | `title: string, children?: ReactNode`    | Page title bar with optional actions     |
| `ServiceBadge`  | `name: string`                           | Colored badge for service name           |
| `StatusBadge`   | `status: string`                         | OK (green) / ERROR (red) / UNSET (gray)  |
| `DurationBar`   | `duration: number, totalDuration: number, offset?: number` | Proportional width bar. In search results: offset omitted, bar is relative width. In trace waterfall: offset positions the bar from trace start, totalDuration is the full trace duration. |
| `ErrorBadge`    | `count: number`                          | Red error count indicator                |
| `TimeDisplay`   | `nanos: number`                          | Formats unix nanos to readable time      |
| `CopyButton`    | `text: string`                           | Click-to-copy with feedback              |

### Data Hooks

| Hook               | Query Key                    | Endpoint                          |
|---------------------|------------------------------|-----------------------------------|
| `useServices`       | `["services"]`               | `GET /api/services`               |
| `useTraces`         | `["traces", filters]`        | `GET /api/traces?...`             |
| `useTrace`          | `["trace", traceID]`         | `GET /api/traces/:traceID` (returns `TraceDetail` with tree) |
| `useDependencies`   | `["dependencies", lookback]` | `GET /api/dependencies?lookback=` |
| `useStatus`         | `["status"]`                 | `GET /api/status`                 |
