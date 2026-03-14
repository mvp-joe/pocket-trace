package server

import "github.com/gofiber/fiber/v3"

// RegisterRoutes wires all API endpoints to their handler methods.
func RegisterRoutes(app *fiber.App, h *Handlers) {
	api := app.Group("/api")

	api.Post("/ingest", h.Ingest)

	// Query endpoints (Phase 4 -- stubs for now):
	// api.Get("/services", h.ListServices)
	// api.Get("/traces", h.SearchTraces)
	// api.Get("/traces/:traceID", h.GetTrace)
	// api.Get("/traces/:traceID/spans/:spanID", h.GetSpan)
	// api.Get("/dependencies", h.GetDependencies)
	// api.Get("/status", h.Status)
	// api.Post("/purge", h.Purge)
}
