package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefault_ReturnsValidConfig(t *testing.T) {
	t.Parallel()

	cfg := Default()

	if cfg.Listen != ":7070" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":7070")
	}
	if !strings.HasSuffix(cfg.DBPath, "pocket-trace.db") {
		t.Errorf("DBPath = %q, want suffix %q", cfg.DBPath, "pocket-trace.db")
	}
	if cfg.Retention != 168*time.Hour {
		t.Errorf("Retention = %v, want %v", cfg.Retention, 168*time.Hour)
	}
	if cfg.PurgeInterval != 1*time.Hour {
		t.Errorf("PurgeInterval = %v, want %v", cfg.PurgeInterval, 1*time.Hour)
	}
	if cfg.FlushInterval != 2*time.Second {
		t.Errorf("FlushInterval = %v, want %v", cfg.FlushInterval, 2*time.Second)
	}
	if cfg.BufferSize != 4096 {
		t.Errorf("BufferSize = %d, want %d", cfg.BufferSize, 4096)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoad_ReturnsDefaultsWhenNoFileExists(t *testing.T) {
	t.Parallel()

	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	expected := Default()
	assertConfigEqual(t, expected, cfg)
}

func TestLoad_ReturnsDefaultsWhenPathEmpty(t *testing.T) {
	t.Parallel()

	// Empty path falls back to /etc/pocket-trace/config.yaml which
	// likely doesn't exist in test, so we get defaults.
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	expected := Default()
	assertConfigEqual(t, expected, cfg)
}

func TestLoad_ReadsYAMLFileCorrectly(t *testing.T) {
	t.Parallel()

	content := `
listen: ":9090"
db_path: "/tmp/test.db"
retention: "72h"
purge_interval: "30m"
flush_interval: "5s"
buffer_size: 8192
log_level: "debug"
`

	path := writeTestConfig(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":9090")
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.Retention != 72*time.Hour {
		t.Errorf("Retention = %v, want %v", cfg.Retention, 72*time.Hour)
	}
	if cfg.PurgeInterval != 30*time.Minute {
		t.Errorf("PurgeInterval = %v, want %v", cfg.PurgeInterval, 30*time.Minute)
	}
	if cfg.FlushInterval != 5*time.Second {
		t.Errorf("FlushInterval = %v, want %v", cfg.FlushInterval, 5*time.Second)
	}
	if cfg.BufferSize != 8192 {
		t.Errorf("BufferSize = %d, want %d", cfg.BufferSize, 8192)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoad_MergesPartialConfigWithDefaults(t *testing.T) {
	t.Parallel()

	content := `listen: ":8080"`
	path := writeTestConfig(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	defaults := Default()

	// Overridden field.
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}

	// All other fields should retain defaults.
	if cfg.DBPath != defaults.DBPath {
		t.Errorf("DBPath = %q, want default %q", cfg.DBPath, defaults.DBPath)
	}
	if cfg.Retention != defaults.Retention {
		t.Errorf("Retention = %v, want default %v", cfg.Retention, defaults.Retention)
	}
	if cfg.PurgeInterval != defaults.PurgeInterval {
		t.Errorf("PurgeInterval = %v, want default %v", cfg.PurgeInterval, defaults.PurgeInterval)
	}
	if cfg.FlushInterval != defaults.FlushInterval {
		t.Errorf("FlushInterval = %v, want default %v", cfg.FlushInterval, defaults.FlushInterval)
	}
	if cfg.BufferSize != defaults.BufferSize {
		t.Errorf("BufferSize = %d, want default %d", cfg.BufferSize, defaults.BufferSize)
	}
	if cfg.LogLevel != defaults.LogLevel {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, defaults.LogLevel)
	}
}

func TestLoad_MergesPartialDurations(t *testing.T) {
	t.Parallel()

	content := `retention: "24h"`
	path := writeTestConfig(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Retention != 24*time.Hour {
		t.Errorf("Retention = %v, want %v", cfg.Retention, 24*time.Hour)
	}

	// Other durations keep defaults.
	defaults := Default()
	if cfg.PurgeInterval != defaults.PurgeInterval {
		t.Errorf("PurgeInterval = %v, want default %v", cfg.PurgeInterval, defaults.PurgeInterval)
	}
	if cfg.FlushInterval != defaults.FlushInterval {
		t.Errorf("FlushInterval = %v, want default %v", cfg.FlushInterval, defaults.FlushInterval)
	}
}

func TestLoad_ReturnsErrorForInvalidYAML(t *testing.T) {
	t.Parallel()

	path := writeTestConfig(t, `listen: [invalid`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "parsing config")
	}
}

func TestLoad_ReturnsErrorForInvalidDuration(t *testing.T) {
	t.Parallel()

	path := writeTestConfig(t, `retention: "banana"`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid duration")
	}
	if !strings.Contains(err.Error(), "invalid retention duration") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "invalid retention duration")
	}
}

func TestLoad_ReturnsErrorForUnreadableFile(t *testing.T) {
	t.Parallel()

	if os.Getuid() == 0 {
		t.Skip("chmod 0o000 has no effect when running as root")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: ':7070'"), 0o000); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error for unreadable file")
	}
	if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "reading config")
	}
}

func TestLoad_EmptyFileReturnsDefaults(t *testing.T) {
	t.Parallel()

	path := writeTestConfig(t, "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expected := Default()
	assertConfigEqual(t, expected, cfg)
}

// assertConfigEqual compares two Config values field by field with clear error messages.
func assertConfigEqual(t *testing.T, want, got *Config) {
	t.Helper()
	if got.Listen != want.Listen {
		t.Errorf("Listen = %q, want %q", got.Listen, want.Listen)
	}
	if got.DBPath != want.DBPath {
		t.Errorf("DBPath = %q, want %q", got.DBPath, want.DBPath)
	}
	if got.Retention != want.Retention {
		t.Errorf("Retention = %v, want %v", got.Retention, want.Retention)
	}
	if got.PurgeInterval != want.PurgeInterval {
		t.Errorf("PurgeInterval = %v, want %v", got.PurgeInterval, want.PurgeInterval)
	}
	if got.FlushInterval != want.FlushInterval {
		t.Errorf("FlushInterval = %v, want %v", got.FlushInterval, want.FlushInterval)
	}
	if got.BufferSize != want.BufferSize {
		t.Errorf("BufferSize = %d, want %d", got.BufferSize, want.BufferSize)
	}
	if got.LogLevel != want.LogLevel {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, want.LogLevel)
	}
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
