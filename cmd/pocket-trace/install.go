package main

import (
	"fmt"
	"os"
	"path/filepath"

	"pocket-trace/internal/daemon"

	"github.com/spf13/cobra"
)

var installConfigPath string

const defaultConfigYAML = `# pocket-trace daemon configuration
listen: ":7070"
db_path: "/var/lib/pocket-trace/pocket-trace.db"
retention: "168h"
purge_interval: "1h"
flush_interval: "2s"
buffer_size: 4096
log_level: "info"
`

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install pocket-trace as a system service",
	Long:  "Install pocket-trace as a systemd service. Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve absolute path to the running binary.
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving binary path: %w", err)
		}
		binaryPath, err := filepath.EvalSymlinks(exe)
		if err != nil {
			return fmt.Errorf("resolving binary symlinks: %w", err)
		}

		// Create config directory and default config if they don't exist.
		configDir := filepath.Dir(installConfigPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("creating config directory %s: %w", configDir, err)
		}
		if _, err := os.Stat(installConfigPath); os.IsNotExist(err) {
			if err := os.WriteFile(installConfigPath, []byte(defaultConfigYAML), 0644); err != nil {
				return fmt.Errorf("writing default config: %w", err)
			}
			fmt.Printf("Created default config: %s\n", installConfigPath)
		}

		// Create data directory for the SQLite database.
		if err := os.MkdirAll("/var/lib/pocket-trace", 0755); err != nil {
			return fmt.Errorf("creating data directory: %w", err)
		}

		mgr, err := daemon.NewDaemonManager()
		if err != nil {
			return err
		}

		fmt.Printf("Installing pocket-trace service...\n")
		fmt.Printf("  Binary: %s\n", binaryPath)
		fmt.Printf("  Config: %s\n", installConfigPath)

		if err := mgr.Install(binaryPath, installConfigPath); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}

		fmt.Println("pocket-trace service installed and started successfully.")
		return nil
	},
}

func init() {
	installCmd.Flags().StringVar(&installConfigPath, "config-path", "/etc/pocket-trace/config.yaml", "path to config file for the service")
}
