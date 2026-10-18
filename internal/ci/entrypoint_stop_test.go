package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEntrypointSIGTERMExitsZero(t *testing.T) {
	if _, err := exec.LookPath("busybox"); err != nil {
		t.Skip("busybox is not installed")
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		t.Skip("sudo is not installed")
	}
	script := `#!/bin/sh
case "$1" in
start)
  trap 'exit 2' TERM
  sleep 30
  ;;
*)
  exit 0
  ;;
esac
`
	var waitErr error
	for attempt := 1; attempt <= 3; attempt++ {
		cmd := startEntrypoint(t, script)
		time.Sleep(400 * time.Millisecond)
		// sudo dies from SIGTERM before the shell trap can exit 0. Signal the
		// entrypoint shell, which is the process Docker stops. A signal that
		// lands before that trap is installed kills sudo with the signal;
		// retry instead of treating that race as a failed stop.
		if err := signalEntrypoint(cmd.Process.Pid); err != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			waitErr = err
			continue
		}
		waitErr = waitFor(t, cmd, 5*time.Second)
		if waitErr == nil {
			return
		}
		t.Logf("SIGTERM attempt %d: %v", attempt, waitErr)
	}
	t.Fatalf("SIGTERM entrypoint exit = %v", waitErr)
}

func TestEntrypointCrashStillExitsNonZero(t *testing.T) {
	if _, err := exec.LookPath("busybox"); err != nil {
		t.Skip("busybox is not installed")
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		t.Skip("sudo is not installed")
	}
	cmd := startEntrypoint(t, `#!/bin/sh
case "$1" in
start)
  exit 2
  ;;
*)
  exit 0
  ;;
esac
`)
	waitErr := waitFor(t, cmd, 5*time.Second)
	if waitErr == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 2 {
		t.Fatalf("unrequested exit 2 = %v", waitErr)
	}
}

func startEntrypoint(t *testing.T, fake string) *exec.Cmd {
	t.Helper()
	busybox, err := exec.LookPath("busybox")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.jsonc")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "fse"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sudo", "-n", "env",
		"PATH="+bin+":/usr/bin:/bin",
		"FSE_CONFIG_PATH="+configPath,
		"FSE_RUN_AS_ROOT=true",
		busybox, "sh", filepath.Join(root, "scripts", "container-entrypoint.sh"),
	)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd
}

func signalEntrypoint(sudoPID int) error {
	deadline := time.Now().Add(2 * time.Second)
	var pid int
	var err error
	for time.Now().Before(deadline) {
		pid, err = firstChild(sudoPID)
		if err == nil && pid > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid <= 0 {
		if err == nil {
			err = fmt.Errorf("entrypoint shell did not start")
		}
		return err
	}
	// The shell runs as root under sudo. This process can signal sudo, and sudo
	// can signal the shell. Signaling the shell directly is EPERM.
	return exec.Command("sudo", "-n", "kill", "-TERM", strconv.Itoa(pid)).Run()
}

func firstChild(pid int) (int, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("no child of %d", pid)
	}
	return strconv.Atoi(fields[0])
}

func waitFor(t *testing.T, cmd *exec.Cmd, timeout time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		t.Fatal("entrypoint did not exit")
		return nil
	}
}
