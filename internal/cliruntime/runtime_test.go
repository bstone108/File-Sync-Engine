package cliruntime

import (
	"path/filepath"
	"testing"

	"filesyncengine/internal/cli"
)

func TestRunParsesResolvesConfigAndDispatchesSelectedCommand(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	executablePath := filepath.Join(dir, "fse")

	var called string
	var receivedConfigPath string
	err := Run([]string{"status", configPath}, executablePath, Runners{
		Status: func(path string) {
			called = "status"
			receivedConfigPath = path
		},
	})

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if called != "status" {
		t.Fatalf("expected status runner, got %q", called)
	}
	if receivedConfigPath != configPath {
		t.Fatalf("expected resolved config path %q, got %q", configPath, receivedConfigPath)
	}
}

func TestRunReturnsParseErrorsBeforeDispatch(t *testing.T) {
	var called bool
	err := Run([]string{}, filepath.Join(t.TempDir(), "fse"), Runners{
		Start: func(path string) { called = true },
	})

	if err == nil {
		t.Fatalf("expected parse error")
	}
	if called {
		t.Fatalf("runner should not be called after parse error")
	}
}

func TestRunPassesOriginalOptionsToOptionAwareRunners(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	executablePath := filepath.Join(dir, "fse")

	var received cli.Options
	err := Run([]string{"scan", "--folder", "photos", configPath}, executablePath, Runners{
		Scan: func(opts cli.Options, path string) {
			received = opts
		},
	})

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if received.Command != cli.CommandScan || received.ID != "photos" || received.ConfigPath != configPath {
		t.Fatalf("scan options not preserved: %#v", received)
	}
}

func TestRunDispatchesWebGUIAndIdentityCommands(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	executablePath := filepath.Join(dir, "fse")

	var calls []string
	if err := Run([]string{"web-gui", "start", configPath}, executablePath, Runners{
		WebGUI: func(opts cli.Options, path string) {
			calls = append(calls, string(opts.Command)+":"+string(opts.Action)+":"+path)
		},
	}); err != nil {
		t.Fatalf("Run web-gui returned error: %v", err)
	}
	if err := Run([]string{"identity", "export", "--group", "family-sync", configPath}, executablePath, Runners{
		Identity: func(opts cli.Options, path string) {
			calls = append(calls, string(opts.Command)+":"+opts.ID+":"+path)
		},
	}); err != nil {
		t.Fatalf("Run identity returned error: %v", err)
	}

	want := []string{"web-gui:start:" + configPath, "identity:family-sync:" + configPath}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunDispatchesContainerBootstrapCommand(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	executablePath := filepath.Join(dir, "fse")

	var receivedConfigPath string
	if err := Run([]string{"container-bootstrap", configPath}, executablePath, Runners{
		ContainerBootstrap: func(path string) {
			receivedConfigPath = path
		},
	}); err != nil {
		t.Fatalf("Run container-bootstrap returned error: %v", err)
	}
	if receivedConfigPath != configPath {
		t.Fatalf("container-bootstrap config path = %q, want %q", receivedConfigPath, configPath)
	}
}
