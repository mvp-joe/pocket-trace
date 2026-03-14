package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"pocket-trace/internal/daemon"
	"pocket-trace/internal/server"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon and service status",
	Long:  "Query the running daemon for health info and show systemd service status.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Query systemd service status.
		mgr, err := daemon.NewDaemonManager()
		if err != nil {
			return err
		}

		svcStatus, err := mgr.Status()
		if err != nil {
			return fmt.Errorf("querying service status: %w", err)
		}

		fmt.Println("Service Status:")
		if svcStatus.Running {
			fmt.Println("  State:   running")
		} else {
			fmt.Println("  State:   stopped")
		}
		if svcStatus.Enabled {
			fmt.Println("  Enabled: yes")
		} else {
			fmt.Println("  Enabled: no")
		}
		if svcStatus.PID > 0 {
			fmt.Printf("  PID:     %d\n", svcStatus.PID)
		}
		if svcStatus.Uptime != "" {
			fmt.Printf("  Since:   %s\n", svcStatus.Uptime)
		}

		// If running, also query the daemon's HTTP status endpoint.
		if !svcStatus.Running {
			fmt.Println("\nDaemon is not running. Start it with: sudo systemctl start pocket-trace")
			return nil
		}

		fmt.Println()

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://localhost:7070/api/status")
		if err != nil {
			fmt.Printf("Daemon API unreachable: %v\n", err)
			return nil
		}
		defer resp.Body.Close()

		var apiResp server.APIResponse[server.StatusResponse]
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			fmt.Printf("Failed to parse daemon response: %v\n", err)
			return nil
		}

		if apiResp.Error != "" {
			fmt.Printf("Daemon error: %s\n", apiResp.Error)
			return nil
		}

		d := apiResp.Data
		fmt.Println("Daemon Status:")
		fmt.Printf("  Version: %s\n", d.Version)
		fmt.Printf("  Uptime:  %s\n", d.Uptime)
		fmt.Printf("  Spans:   %d\n", d.DB.SpanCount)
		fmt.Printf("  Traces:  %d\n", d.DB.TraceCount)
		fmt.Printf("  DB Size: %s\n", formatBytes(d.DB.DBSizeBytes))

		return nil
	},
}

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
