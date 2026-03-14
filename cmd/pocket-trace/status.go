package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon and service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("status: not implemented yet")
		return nil
	},
}
