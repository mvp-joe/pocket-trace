package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	configPath string
	version    = "dev"
)

var rootCmd = &cobra.Command{
	Use:          "pocket-trace",
	Short:        "Self-contained tracing daemon",
	Long:         "pocket-trace — accepts trace data via JSON HTTP POST, stores spans in SQLite, and serves a web UI.",
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config file")
	rootCmd.AddCommand(runCmd)
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
