// Package daemon handles platform-specific service lifecycle management.
package daemon

import "fmt"

const (
	serviceName = "pocket-trace"
	unitPath    = "/etc/systemd/system/pocket-trace.service"
)

// DaemonManager handles platform-specific service lifecycle.
type DaemonManager interface {
	Install(binaryPath string, configPath string) error
	Uninstall() error
	Status() (*ServiceStatus, error)
}

// ServiceStatus reports the current state of the daemon service.
type ServiceStatus struct {
	Running bool   `json:"running"`
	Enabled bool   `json:"enabled"`
	PID     int    `json:"pid,omitempty"`
	Uptime  string `json:"uptime,omitempty"`
}

// NewDaemonManager returns the platform-appropriate DaemonManager.
func NewDaemonManager() (DaemonManager, error) {
	return newSystemdManager()
}

func newSystemdManager() (DaemonManager, error) {
	// Verify systemctl is available.
	out, err := execCommand("systemctl", "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("systemd not available: %w (output: %s)", err, out)
	}
	return &SystemdManager{}, nil
}
