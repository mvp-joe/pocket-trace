package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pocket-trace",
	Short: "Self-contained tracing daemon",
	Long:  "pocket-trace daemon — accepts trace data via JSON HTTP POST, stores spans in SQLite, and serves a web UI.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("daemon starting")
		return nil
	},
}

func init() {
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
