package server

import (
	"context"
	"errors"
	"io/fs"

	"pocket-trace/internal/store"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

// Server wraps the Fiber HTTP server, store, and span buffer.
type Server struct {
	app    *fiber.App
	store  *store.Store
	buffer *SpanBuffer
}

// New creates a Server with the Fiber app, routes, and middleware configured.
// The uiFS parameter provides embedded UI assets (may be nil if not yet available).
func New(s *store.Store, buf *SpanBuffer, h *Handlers, uiFS fs.FS) *Server {
	app := fiber.New(fiber.Config{
		ErrorHandler: jsonErrorHandler,
	})

	app.Use(recover.New())

	RegisterRoutes(app, h)

	// TODO: Serve embedded UI assets from uiFS when available.
	_ = uiFS

	return &Server{
		app:    app,
		store:  s,
		buffer: buf,
	}
}

// Start begins listening on the given address. Blocks until the server stops.
func (s *Server) Start(listenAddr string) error {
	return s.app.Listen(listenAddr)
}

// Shutdown gracefully shuts down the server, flushes the span buffer, and
// closes the store.
func (s *Server) Shutdown(ctx context.Context) error {
	var errs []error

	if err := s.app.ShutdownWithContext(ctx); err != nil {
		errs = append(errs, err)
	}

	s.buffer.Shutdown()

	if err := s.store.Close(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// App returns the underlying Fiber app, useful for testing.
func (s *Server) App() *fiber.App {
	return s.app
}

// jsonErrorHandler returns errors as JSON using the APIResponse envelope.
func jsonErrorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError

	var e *fiber.Error
	if errors.As(err, &e) {
		code = e.Code
	}

	return c.Status(code).JSON(APIResponse[any]{
		Error: err.Error(),
	})
}
