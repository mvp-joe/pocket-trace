package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"pocket-trace/internal/server"

	"github.com/spf13/cobra"
)

var olderThan string

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Delete old trace data",
	Long:  "Send a purge request to the running daemon to delete spans older than the specified duration.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate the duration string before sending.
		if _, err := time.ParseDuration(olderThan); err != nil {
			return fmt.Errorf("invalid --older-than duration %q: %w", olderThan, err)
		}

		endpoint := fmt.Sprintf("http://localhost:7070/api/purge?olderThan=%s", url.QueryEscape(olderThan))

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Post(endpoint, "application/json", nil)
		if err != nil {
			return fmt.Errorf("daemon unreachable (is it running?): %w", err)
		}
		defer resp.Body.Close()

		var apiResp server.APIResponse[server.PurgeResult]
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if apiResp.Error != "" {
			return fmt.Errorf("purge failed: %s", apiResp.Error)
		}

		fmt.Printf("Purged %d spans older than %s.\n", apiResp.Data.Deleted, olderThan)
		return nil
	},
}

func init() {
	purgeCmd.Flags().StringVar(&olderThan, "older-than", "24h", "delete spans older than this duration (e.g. 24h, 168h)")
}
