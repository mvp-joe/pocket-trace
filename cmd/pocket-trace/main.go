package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pocket-trace/internal/config"
	"pocket-trace/internal/server"
	"pocket-trace/internal/store"

	"github.com/spf13/cobra"
)

var (
	configPath string
	version    = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "pocket-trace",
	Short: "Self-contained tracing daemon",
	Long:  "pocket-trace daemon — accepts trace data via JSON HTTP POST, stores spans in SQLite, and serves a web UI.",
	RunE:  runDaemon,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config file")
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(purgeCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runDaemon(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
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

	srv := server.New(s, buf, h, nil)

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
