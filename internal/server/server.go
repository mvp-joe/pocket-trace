package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"time"

	"pocket-trace/internal/store"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

// Server wraps the Fiber HTTP server, store, and span buffer.
type Server struct {
	app        *fiber.App
	store      *store.Store
	buffer     *SpanBuffer
	stopPurger chan struct{}
}

// New creates a Server with the Fiber app, routes, and middleware configured.
// The uiFS parameter provides embedded UI assets (may be nil if not yet available).
// retention and purgeInterval control the background retention purger. Pass zero
// values to disable the purger (useful in tests).
func New(s *store.Store, buf *SpanBuffer, h *Handlers, uiFS fs.FS, retention, purgeInterval time.Duration) *Server {
	app := fiber.New(fiber.Config{
		ErrorHandler: jsonErrorHandler,
	})

	app.Use(recover.New())

	RegisterRoutes(app, h)

	// TODO: Serve embedded UI assets from uiFS when available.
	_ = uiFS

	srv := &Server{
		app:    app,
		store:  s,
		buffer: buf,
	}

	// Start retention purger if both retention and purge interval are configured.
	if retention > 0 && purgeInterval > 0 {
		srv.stopPurger = make(chan struct{})
		go srv.runRetentionPurger(retention, purgeInterval)
	}

	return srv
}

// runRetentionPurger periodically purges spans older than the retention period.
func (s *Server) runRetentionPurger(retention, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			before := time.Now().Add(-retention)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			deleted, err := s.store.PurgeOlderThan(ctx, before)
			cancel()
			if err != nil {
				slog.Error("retention purge failed", "error", err)
			} else if deleted > 0 {
				slog.Info("retention purge", "deleted", deleted)
			}
		case <-s.stopPurger:
			return
		}
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

	// Stop the retention purger.
	if s.stopPurger != nil {
		close(s.stopPurger)
	}

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
