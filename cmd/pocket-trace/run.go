package main

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pocket-trace/internal/config"
	"pocket-trace/internal/server"
	"pocket-trace/internal/store"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the daemon in the foreground",
	Long:  "Start the pocket-trace daemon in the foreground. Useful for testing and seeing output directly.",
	RunE:  runDaemon,
}

func runDaemon(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Ensure the database directory exists.
	dbDir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return err
	}

	slog.Info("opening store", "path", cfg.DBPath)
	s, err := store.New(cfg.DBPath)
	if err != nil {
		return err
	}

	buf := server.NewSpanBuffer(s, cfg.BufferSize, 64, cfg.FlushInterval)

	h := &server.Handlers{
		Store:     s,
		Buffer:    buf,
		StartTime: time.Now(),
		Version:   version,
	}

	// Strip the "ui/dist" prefix from the embedded FS so the server sees
	// files at the root (e.g., "index.html" instead of "ui/dist/index.html").
	// fs.Sub returns an error only if the path is invalid, which it won't be
	// for a compile-time constant. For the dev build, uiFS is empty and Sub
	// will return a valid but empty FS.
	uiAssets, _ := fs.Sub(uiFS, "ui/dist")

	srv := server.New(s, buf, h, uiAssets, cfg.Retention, cfg.PurgeInterval)

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Listen)
		errCh <- srv.Start(cfg.Listen)
	}()

	select {
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig)
	case err := <-errCh:
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
		return err
	}

	slog.Info("shutdown complete")
	return nil
}
