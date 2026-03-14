package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var olderThan string

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Delete old trace data",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("purge: not implemented yet (older-than=%s)\n", olderThan)
		return nil
	},
}

func init() {
	purgeCmd.Flags().StringVar(&olderThan, "older-than", "24h", "delete spans older than this duration (e.g. 24h, 7d)")
}
