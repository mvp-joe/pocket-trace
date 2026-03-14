package main

import (
	"fmt"

	"pocket-trace/internal/daemon"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove pocket-trace system service",
	Long:  "Stop and remove the pocket-trace systemd service. Requires root privileges.",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := daemon.NewDaemonManager()
		if err != nil {
			return err
		}

		fmt.Println("Uninstalling pocket-trace service...")

		if err := mgr.Uninstall(); err != nil {
			return fmt.Errorf("uninstall failed: %w", err)
		}

		fmt.Println("pocket-trace service removed successfully.")
		return nil
	},
}
