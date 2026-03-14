package daemon

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// execCommand is the function used to create exec.Cmd instances.
// It is a package-level variable so tests can replace it with a mock.
var execCommand = exec.Command

const unitTemplate = `[Unit]
Description=pocket-trace daemon
After=network.target

[Service]
Type=simple
ExecStart=%s --config %s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// SystemdManager implements DaemonManager for Linux systems using systemd.
type SystemdManager struct{}

// Install writes the systemd unit file, reloads the daemon, enables and starts the service.
func (m *SystemdManager) Install(binaryPath string, configPath string) error {
	content := fmt.Sprintf(unitTemplate, binaryPath, configPath)

	if err := os.WriteFile(unitPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}

	steps := []struct {
		name string
		args []string
	}{
		{"daemon-reload", []string{"systemctl", "daemon-reload"}},
		{"enable", []string{"systemctl", "enable", serviceName}},
		{"start", []string{"systemctl", "start", serviceName}},
	}

	for _, step := range steps {
		out, err := execCommand(step.args[0], step.args[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl %s: %w (output: %s)", step.name, err, out)
		}
	}

	return nil
}

// Uninstall stops the service, disables it, removes the unit file, and reloads the daemon.
func (m *SystemdManager) Uninstall() error {
	steps := []struct {
		name string
		args []string
	}{
		{"stop", []string{"systemctl", "stop", serviceName}},
		{"disable", []string{"systemctl", "disable", serviceName}},
	}

	for _, step := range steps {
		out, err := execCommand(step.args[0], step.args[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl %s: %w (output: %s)", step.name, err, out)
		}
	}

	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}

	out, err := execCommand("systemctl", "daemon-reload").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w (output: %s)", err, out)
	}

	return nil
}

// Status queries systemctl for the current state of the service.
func (m *SystemdManager) Status() (*ServiceStatus, error) {
	out, err := execCommand("systemctl", "show", serviceName,
		"--property=ActiveState,SubState,MainPID,ActiveEnterTimestamp,UnitFileState").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("systemctl show: %w (output: %s)", err, out)
	}

	props := parseProperties(string(out))

	status := &ServiceStatus{
		Running: props["ActiveState"] == "active" && props["SubState"] == "running",
		Enabled: props["UnitFileState"] == "enabled",
	}

	if pid, err := strconv.Atoi(props["MainPID"]); err == nil && pid > 0 {
		status.PID = pid
	}

	if ts := props["ActiveEnterTimestamp"]; ts != "" && ts != "n/a" {
		status.Uptime = ts
	}

	return status, nil
}

// parseProperties parses "Key=Value" lines from systemctl show output.
func parseProperties(output string) map[string]string {
	props := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = v
		}
	}
	return props
}
