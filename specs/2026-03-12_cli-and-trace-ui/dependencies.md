# Dependencies

## New Go Dependencies

| Package                       | Version  | Purpose                                          |
|-------------------------------|----------|--------------------------------------------------|
| `github.com/spf13/cobra`     | v1.10.x  | CLI command framework (root, install, uninstall, status, purge) |
| `github.com/gofiber/fiber/v3` | v3.0.x   | HTTP server for ingest API, query API, and static UI serving    |
| `modernc.org/sqlite`          | v1.37.x  | Pure-Go SQLite driver (no CGO, cross-compilable)               |
| `gopkg.in/yaml.v3`            | v3.0.x   | YAML config file parsing                                       |

## Removed Go Dependencies

| Package                                  | Reason                       |
|------------------------------------------|------------------------------|
| `go.opentelemetry.io/proto/otlp`         | OTLP exporter dropped        |
| `golang.org/x/net`                       | Removed as direct dep (was used for h2c/OTLP gRPC). Remains as transitive dep of Fiber v3. |
| `google.golang.org/protobuf`             | Protobuf no longer needed    |

## New TypeScript Dependencies

| Package                  | Version | Purpose                                         |
|--------------------------|---------|--------------------------------------------------|
| `vite`                   | ^6      | Build tool and dev server                       |
| `react`                  | ^19     | UI framework                                    |
| `react-dom`              | ^19     | React DOM renderer                              |
| `react-router-dom`       | ^7      | Client-side routing                             |
| `@tanstack/react-query`  | ^5      | Data fetching, caching, and synchronization     |
| `tailwindcss`            | ^4      | Utility-first CSS                               |
| `shadcn/ui`              | latest  | Pre-built accessible UI components (copied into project) |
| `lucide-react`           | latest  | Icon library                                    |
| `date-fns`               | ^4      | Date formatting utilities                       |
| `typescript`             | ^5      | Type safety                                     |

## Rationale

**modernc.org/sqlite over github.com/mattn/go-sqlite3:** The mattn driver requires CGO and a C compiler, complicating cross-compilation and CI. modernc.org/sqlite is a pure Go translation of SQLite's C source, requiring zero external toolchain. Performance is comparable for the workload (single-writer, moderate read volume). The driver registers as a standard `database/sql` driver, so switching later is trivial.

**Fiber v3 over net/http:** Fiber provides routing, JSON binding, static file serving, and error handling out of the box with minimal boilerplate. The fasthttp engine underneath offers strong performance for the high-throughput ingest endpoint. The v3 API uses interfaces for the context, aligning with Go conventions.

**YAML over TOML for config:** YAML is more widely familiar and `gopkg.in/yaml.v3` is a single, stable dependency. The config structure is flat enough that either format works; YAML was chosen for readability of duration values and nested structures if needed later.
