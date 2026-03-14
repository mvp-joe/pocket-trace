package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install pocket-trace as a system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("install: not implemented yet")
		return nil
	},
}
