package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mockExecCommand returns a function that replaces exec.Command in tests.
// It records each invocation in calls, serves responses keyed by the full
// command string, and causes any command containing failOn to exit 1.
func mockExecCommand(t *testing.T, calls *[]string, responses map[string]string, failOn string) func(string, ...string) *exec.Cmd {
	t.Helper()
	return func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		*calls = append(*calls, full)

		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)

		key := strings.TrimSpace(full)
		env := os.Environ()
		if resp, ok := responses[key]; ok {
			env = append(env, "GO_TEST_HELPER_RESPONSE="+resp)
		}
		if failOn != "" && strings.Contains(full, failOn) {
			env = append(env, "GO_TEST_HELPER_EXIT=1")
		} else {
			env = append(env, "GO_TEST_HELPER_EXIT=0")
		}
		env = append(env, "GO_TEST_HELPER_PROCESS=1")
		cmd.Env = env

		return cmd
	}
}

// TestHelperProcess is the subprocess spawned by mockExecCommand.
// It is not a real test.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER_PROCESS") != "1" {
		return
	}
	if resp := os.Getenv("GO_TEST_HELPER_RESPONSE"); resp != "" {
		fmt.Fprint(os.Stdout, resp)
	}
	if os.Getenv("GO_TEST_HELPER_EXIT") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func withMockExec(t *testing.T, calls *[]string, responses map[string]string, failOn string) {
	t.Helper()
	orig := execCommand
	execCommand = mockExecCommand(t, calls, responses, failOn)
	t.Cleanup(func() { execCommand = orig })
}

func TestSystemdManager_Uninstall_ExecSequence(t *testing.T) {
	if os.Getuid() != 0 {
		// The real unit file path requires root to remove.
		// Create a temp file and override unitPath for this test.
		tmp, err := os.CreateTemp("", "pocket-trace-unit-*")
		if err != nil {
			t.Fatal(err)
		}
		tmp.Close()
		origPath := unitPath
		unitPath = tmp.Name()
		t.Cleanup(func() { unitPath = origPath })
	}

	var calls []string
	withMockExec(t, &calls, nil, "")

	mgr := &SystemdManager{}
	err := mgr.Uninstall()
	if err != nil {
		t.Fatalf("Uninstall() unexpected error: %v", err)
	}

	want := []string{
		"systemctl stop pocket-trace",
		"systemctl disable pocket-trace",
		"systemctl daemon-reload",
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %v", len(calls), len(want), calls)
	}
	for i, w := range want {
		if calls[i] != w {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], w)
		}
	}
}

func TestSystemdManager_Uninstall_FailsOnStop(t *testing.T) {
	var calls []string
	withMockExec(t, &calls, nil, "stop")

	mgr := &SystemdManager{}
	err := mgr.Uninstall()
	if err == nil {
		t.Fatal("expected error from Uninstall when stop fails")
	}
	if !strings.Contains(err.Error(), "systemctl stop") {
		t.Errorf("error should mention systemctl stop, got: %v", err)
	}
	if len(calls) != 1 {
		t.Errorf("should stop after first failure, got %d calls", len(calls))
	}
}

func TestSystemdManager_Status_Running(t *testing.T) {
	showOutput := "ActiveState=active\nSubState=running\nMainPID=12345\nActiveEnterTimestamp=Thu 2026-03-12 10:00:00 UTC\nUnitFileState=enabled\n"

	var calls []string
	withMockExec(t, &calls, map[string]string{
		"systemctl show pocket-trace --property=ActiveState,SubState,MainPID,ActiveEnterTimestamp,UnitFileState": showOutput,
	}, "")

	mgr := &SystemdManager{}
	status, err := mgr.Status()
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}

	if !status.Running {
		t.Error("expected Running=true")
	}
	if !status.Enabled {
		t.Error("expected Enabled=true")
	}
	if status.PID != 12345 {
		t.Errorf("PID = %d, want 12345", status.PID)
	}
	if status.Uptime != "Thu 2026-03-12 10:00:00 UTC" {
		t.Errorf("Uptime = %q, want timestamp string", status.Uptime)
	}
}

func TestSystemdManager_Status_Stopped(t *testing.T) {
	showOutput := "ActiveState=inactive\nSubState=dead\nMainPID=0\nActiveEnterTimestamp=n/a\nUnitFileState=disabled\n"

	var calls []string
	withMockExec(t, &calls, map[string]string{
		"systemctl show pocket-trace --property=ActiveState,SubState,MainPID,ActiveEnterTimestamp,UnitFileState": showOutput,
	}, "")

	mgr := &SystemdManager{}
	status, err := mgr.Status()
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}

	if status.Running {
		t.Error("expected Running=false")
	}
	if status.Enabled {
		t.Error("expected Enabled=false")
	}
	if status.PID != 0 {
		t.Errorf("PID = %d, want 0", status.PID)
	}
	if status.Uptime != "" {
		t.Errorf("Uptime = %q, want empty", status.Uptime)
	}
}

func TestSystemdManager_Status_FailsOnExec(t *testing.T) {
	var calls []string
	withMockExec(t, &calls, nil, "show")

	mgr := &SystemdManager{}
	_, err := mgr.Status()
	if err == nil {
		t.Fatal("expected error from Status when systemctl fails")
	}
	if !strings.Contains(err.Error(), "systemctl show") {
		t.Errorf("error should mention systemctl show, got: %v", err)
	}
}

func TestParseProperties(t *testing.T) {
	t.Parallel()

	input := "ActiveState=active\nSubState=running\nMainPID=42\nSomeEmpty=\n"
	props := parseProperties(input)

	cases := map[string]string{
		"ActiveState": "active",
		"SubState":    "running",
		"MainPID":     "42",
		"SomeEmpty":   "",
	}
	for k, want := range cases {
		if got := props[k]; got != want {
			t.Errorf("props[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestUnitTemplateContent(t *testing.T) {
	t.Parallel()

	content := fmt.Sprintf(unitTemplate, "/usr/bin/pocket-trace", "/etc/pocket-trace/config.yaml")

	mustContain := []string{
		"[Unit]",
		"Description=pocket-trace daemon",
		"After=network.target",
		"[Service]",
		"Type=simple",
		"ExecStart=/usr/bin/pocket-trace run --config /etc/pocket-trace/config.yaml",
		"Restart=on-failure",
		"[Install]",
		"WantedBy=multi-user.target",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("unit template missing %q", s)
		}
	}
}

func TestSystemdManager_Install_WritesCorrectExecSequence(t *testing.T) {
	var calls []string
	withMockExec(t, &calls, nil, "")

	mgr := &SystemdManager{}
	// Install will fail on os.WriteFile (can't write to /etc/systemd/system in tests),
	// which is expected. We verify the error message is about the unit file write.
	err := mgr.Install("/usr/local/bin/pocket-trace", "/etc/pocket-trace/config.yaml")
	if err == nil {
		t.Fatal("expected error from Install (can't write to /etc)")
	}
	if !strings.Contains(err.Error(), "writing unit file") {
		t.Errorf("error should mention writing unit file, got: %v", err)
	}
	// No systemctl calls should have been made since WriteFile fails first.
	if len(calls) != 0 {
		t.Errorf("expected no exec calls before WriteFile, got %d: %v", len(calls), calls)
	}
}
