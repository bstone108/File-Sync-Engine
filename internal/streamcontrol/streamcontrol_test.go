package streamcontrol

import (
	"strings"
	"testing"

	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/streamsync"
)

func TestRunServeReportsMissingConfiguredFolder(t *testing.T) {
	cfg := config.Config{NodeName: "node-a", Folders: []config.FolderConfig{{ID: "present", Path: t.TempDir(), BlockSize: 1024}}}

	_, err := Run(RunOptions{Config: cfg, CLI: cli.Options{Action: cli.ActionServe, ID: "missing"}, In: strings.NewReader(""), Out: &strings.Builder{}})

	if err == nil || !strings.Contains(err.Error(), `folder "missing" not found`) {
		t.Fatalf("expected missing-folder error, got %v", err)
	}
}

func TestRunConfiguredLoadsConfigAndPreservesStreamInputs(t *testing.T) {
	var observed RunOptions
	in := strings.NewReader("incoming")
	out := &strings.Builder{}
	result, err := RunConfigured(ConfiguredOptions{
		ConfigPath: "config.jsonc",
		CLI:        cli.Options{Action: cli.ActionPull, ID: "folder-a", Path: "/target"},
		In:         in,
		Out:        out,
		LoadConfig: func(path string) (config.Config, error) {
			if path != "config.jsonc" {
				t.Fatalf("unexpected config path %q", path)
			}
			return config.Config{NodeName: "node-a", Folders: []config.FolderConfig{{ID: "folder-a", Path: "/source", BlockSize: 1024}}}, nil
		},
		Run: func(opts RunOptions) (RunResult, error) {
			observed = opts
			pull := streamsync.PullResult{FilesWritten: 2, BlocksFetched: 3}
			return RunResult{Pull: &pull}, nil
		},
	})

	if err != nil {
		t.Fatalf("RunConfigured returned error: %v", err)
	}
	if observed.Config.NodeName != "node-a" || observed.CLI.ID != "folder-a" || observed.CLI.Path != "/target" {
		t.Fatalf("configured stream options not preserved: %#v", observed)
	}
	if observed.In != in || observed.Out != out {
		t.Fatalf("stream input/output handles were not preserved")
	}
	if result.Pull == nil || result.Pull.FilesWritten != 2 || result.Pull.BlocksFetched != 3 {
		t.Fatalf("unexpected configured stream result: %#v", result.Pull)
	}
}
