package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove pocket-trace system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("uninstall: not implemented yet")
		return nil
	},
}
