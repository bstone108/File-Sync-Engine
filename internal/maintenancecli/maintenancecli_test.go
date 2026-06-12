package maintenancecli

import (
	"errors"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/maintenancecontrol"
)

func TestRunMaintenanceCommandRendersScrubOutput(t *testing.T) {
	called := false
	output, err := Run(cli.Options{Command: cli.CommandMaintenance, Action: cli.ActionScrub, ID: "docs"}, Runners{
		Scrub: func(opts cli.Options) ([]maintenancecontrol.ScrubResult, error) {
			called = true
			if opts.ID != "docs" {
				t.Fatalf("scrub runner did not receive options: %+v", opts)
			}
			return []maintenancecontrol.ScrubResult{{FolderID: "docs", Mode: maintenance.FileScrubFullBlocks, FilesScanned: 2, BytesScanned: 64, Complete: true}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !called {
		t.Fatalf("scrub runner was not called")
	}
	if !strings.Contains(output, "maintenance scrub: folder=docs") || !strings.Contains(output, "maintenance scrub summary: folders=1") {
		t.Fatalf("unexpected scrub output: %q", output)
	}
}

func TestRunMaintenanceCommandRendersBackupScrubOutput(t *testing.T) {
	called := false
	output, err := Run(cli.Options{Command: cli.CommandMaintenance, Action: cli.ActionBackupScrub}, Runners{
		BackupScrub: func(opts cli.Options) (api.BackupScrubResponse, error) {
			called = true
			return api.BackupScrubResponse{Archive: api.BackupArchiveScrubState{CheckedJobs: 3, MissingBlocks: 1}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !called {
		t.Fatalf("backup scrub runner was not called")
	}
	if !strings.Contains(output, "maintenance backup-scrub: archiveCheckedJobs=3 archiveMissingBlocks=1") {
		t.Fatalf("unexpected backup scrub output: %q", output)
	}
}

func TestRunMaintenanceCommandPropagatesRunnerErrors(t *testing.T) {
	expected := errors.New("scrub failed")
	_, err := Run(cli.Options{Command: cli.CommandMaintenance, Action: cli.ActionScrub}, Runners{
		Scrub: func(opts cli.Options) ([]maintenancecontrol.ScrubResult, error) {
			return nil, expected
		},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected propagated runner error, got %v", err)
	}
}

func TestRunMaintenanceCommandRejectsUnsupportedAction(t *testing.T) {
	_, err := Run(cli.Options{Command: cli.CommandMaintenance, Action: "unknown"}, Runners{})
	if err == nil || !strings.Contains(err.Error(), "maintenance action unknown not implemented") {
		t.Fatalf("expected unsupported action error, got %v", err)
	}
}
