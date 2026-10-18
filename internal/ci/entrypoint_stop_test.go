package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
	cmd := startEntrypoint(t, `#!/bin/sh
case "$1" in
start)
  trap 'exit 2' TERM
  sleep 30
  ;;
*)
  exit 0
  ;;
esac
`)
	time.Sleep(400 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	waitErr := waitFor(t, cmd, 5*time.Second)
	if waitErr != nil {
		t.Fatalf("SIGTERM entrypoint exit = %v", waitErr)
	}
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
