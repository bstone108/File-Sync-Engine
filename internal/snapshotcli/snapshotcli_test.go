package snapshotcli

import (
	"errors"
	"strings"
	"testing"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/state"
)

func TestRunSnapshotCommandRendersListRestoreAndRetentionOutput(t *testing.T) {
	listOutput, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionList, ID: "docs"}, Runners{
		List: func(opts cli.Options) ([]state.SnapshotMarker, error) {
			if opts.ID != "docs" {
				t.Fatalf("list runner did not receive options: %+v", opts)
			}
			return []state.SnapshotMarker{{ID: "snap-1", FolderID: "docs", Cursor: 7, StateHash: "abc"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run list returned error: %v", err)
	}
	if !strings.Contains(listOutput, "snapshot: id=snap-1 folder=docs cursor=7") || !strings.Contains(listOutput, "snapshot summary: count=1") {
		t.Fatalf("unexpected list output: %q", listOutput)
	}

	planOutput, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionRestorePlan, ID: "snap-1"}, Runners{
		RestorePlan: func(opts cli.Options) (backup.RestorePlan, error) {
			return backup.RestorePlan{SnapshotID: opts.ID, FolderID: "docs", Destination: "/restore", TotalFiles: 2, TotalBytes: 9}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run restore-plan returned error: %v", err)
	}
	if !strings.Contains(planOutput, "restore-plan summary: snapshot=snap-1 folder=docs destination=/restore dryRun=false files=2 bytes=9") {
		t.Fatalf("unexpected restore-plan output: %q", planOutput)
	}

	restoreOutput, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionRestore, ID: "snap-1"}, Runners{
		Restore: func(opts cli.Options) (backup.RestoreResult, error) {
			return backup.RestoreResult{SnapshotID: opts.ID, FolderID: "docs", Destination: "/restore", TotalFiles: 2, RestoredFiles: 1, RemainingFiles: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run restore returned error: %v", err)
	}
	if !strings.Contains(restoreOutput, "restore summary: snapshot=snap-1 folder=docs destination=/restore files=2 restored=1") {
		t.Fatalf("unexpected restore output: %q", restoreOutput)
	}

	retentionOutput, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionRetention, KeepLast: 3}, Runners{
		Retention: func(opts cli.Options) (backup.SnapshotRetentionPlan, error) {
			if opts.KeepLast != 3 {
				t.Fatalf("retention runner did not receive options: %+v", opts)
			}
			return backup.SnapshotRetentionPlan{JobID: "ret-1", KeepLast: opts.KeepLast, DeprecateSnapshots: []string{"old"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run retention returned error: %v", err)
	}
	if !strings.Contains(retentionOutput, "snapshot retention summary: jobId=ret-1 keepLast=3 deprecated=1") {
		t.Fatalf("unexpected retention output: %q", retentionOutput)
	}
}

func TestRunSnapshotCommandRendersMarkerActionsAndDeleteOutput(t *testing.T) {
	showOutput, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionShow, ID: "snap-1"}, Runners{
		Marker: func(opts cli.Options) (state.SnapshotMarker, error) {
			if opts.Action != cli.ActionShow {
				t.Fatalf("marker runner did not receive action: %+v", opts)
			}
			return state.SnapshotMarker{ID: opts.ID, FolderID: "docs"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run show returned error: %v", err)
	}
	if !strings.Contains(showOutput, "snapshot: id=snap-1 folder=docs") {
		t.Fatalf("unexpected show output: %q", showOutput)
	}

	deleteOutput, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionDelete, ID: "snap-1"}, Runners{
		Marker: func(opts cli.Options) (state.SnapshotMarker, error) {
			return state.SnapshotMarker{ID: opts.ID, FolderID: "docs"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run delete returned error: %v", err)
	}
	if deleteOutput != "snapshot deleted: id=snap-1\n" {
		t.Fatalf("unexpected delete output: %q", deleteOutput)
	}
}

func TestRunSnapshotCommandPropagatesRunnerErrors(t *testing.T) {
	expected := errors.New("restore failed")
	_, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionRestore}, Runners{
		Restore: func(opts cli.Options) (backup.RestoreResult, error) {
			return backup.RestoreResult{}, expected
		},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected propagated runner error, got %v", err)
	}
}

func TestRunSnapshotCommandRejectsUnsupportedAction(t *testing.T) {
	_, err := Run(cli.Options{Command: cli.CommandSnapshot, Action: "unknown"}, Runners{})
	if err == nil || !strings.Contains(err.Error(), "snapshot action unknown not implemented") {
		t.Fatalf("expected unsupported action error, got %v", err)
	}
}
