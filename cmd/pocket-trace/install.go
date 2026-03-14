package main

import (
	"fmt"
	"os"
	"path/filepath"

	"pocket-trace/internal/daemon"

	"github.com/spf13/cobra"
)

var installConfigPath string

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
