// Package config handles daemon configuration loading and defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultConfigPath = "/etc/pocket-trace/config.yaml"

// Config holds daemon configuration loaded from file or defaults.
type Config struct {
	Listen        string        `yaml:"listen"`
	DBPath        string        `yaml:"db_path"`
	Retention     time.Duration `yaml:"retention"`
	PurgeInterval time.Duration `yaml:"purge_interval"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	BufferSize    int           `yaml:"buffer_size"`
	LogLevel      string        `yaml:"log_level"`
}

// defaultDBPath returns the default database path.
// Uses /var/lib/pocket-trace/ when running as root (installed service),
// otherwise ~/.local/share/pocket-trace/ for local development.
func defaultDBPath() string {
	if os.Getuid() == 0 {
		return "/var/lib/pocket-trace/pocket-trace.db"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "pocket-trace.db"
	}
	return filepath.Join(home, ".local", "share", "pocket-trace", "pocket-trace.db")
}

// Default returns a Config populated with default values.
func Default() *Config {
	return &Config{
		Listen:        ":7070",
		DBPath:        defaultDBPath(),
		Retention:     168 * time.Hour,
		PurgeInterval: 1 * time.Hour,
		FlushInterval: 2 * time.Second,
		BufferSize:    4096,
		LogLevel:      "info",
	}
}

// Load reads configuration from a YAML file and merges it with defaults.
// If path is empty, it tries /etc/pocket-trace/config.yaml.
// If no file is found, it returns defaults without error.
func Load(path string) (*Config, error) {
	if path == "" {
		path = defaultConfigPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := Default() // start from defaults so unset fields keep defaults
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return cfg, nil
}

// UnmarshalYAML implements custom YAML unmarshaling for Config.
// This handles time.Duration fields which yaml.v3 cannot decode natively.
// Duration fields accept Go duration strings (e.g. "168h", "2s").
func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	// Use a raw struct so we can handle durations as strings.
	var raw struct {
		Listen        string `yaml:"listen"`
		DBPath        string `yaml:"db_path"`
		Retention     string `yaml:"retention"`
		PurgeInterval string `yaml:"purge_interval"`
		FlushInterval string `yaml:"flush_interval"`
		BufferSize    int    `yaml:"buffer_size"`
		LogLevel      string `yaml:"log_level"`
	}

	if err := value.Decode(&raw); err != nil {
		return err
	}

	if raw.Listen != "" {
		c.Listen = raw.Listen
	}
	if raw.DBPath != "" {
		c.DBPath = raw.DBPath
	}
	if raw.LogLevel != "" {
		c.LogLevel = raw.LogLevel
	}
	if raw.BufferSize != 0 {
		c.BufferSize = raw.BufferSize
	}

	if raw.Retention != "" {
		d, err := time.ParseDuration(raw.Retention)
		if err != nil {
			return fmt.Errorf("invalid retention duration %q: %w", raw.Retention, err)
		}
		c.Retention = d
	}
	if raw.PurgeInterval != "" {
		d, err := time.ParseDuration(raw.PurgeInterval)
		if err != nil {
			return fmt.Errorf("invalid purge_interval duration %q: %w", raw.PurgeInterval, err)
		}
		c.PurgeInterval = d
	}
	if raw.FlushInterval != "" {
		d, err := time.ParseDuration(raw.FlushInterval)
		if err != nil {
			return fmt.Errorf("invalid flush_interval duration %q: %w", raw.FlushInterval, err)
		}
		c.FlushInterval = d
	}

	return nil
}
