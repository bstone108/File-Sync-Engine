package metadatacli

import (
	"errors"
	"strings"
	"testing"

	"filesyncengine/internal/cli"
	"filesyncengine/internal/metadataops"
	"filesyncengine/internal/state"
)

func TestRunMetadataCommandRendersCompactOutput(t *testing.T) {
	called := false
	output, err := Run(cli.Options{Command: cli.CommandMetadata, Action: cli.ActionCompact, ID: "docs"}, Runners{
		CompactionStatePath: func() string { return "/tmp/state.json" },
		Compact: func(opts cli.Options) ([]state.MetadataCompactionResult, error) {
			called = true
			if opts.ID != "docs" {
				t.Fatalf("compact runner did not receive options: %+v", opts)
			}
			return []state.MetadataCompactionResult{{Plan: state.MetadataCompactionPlan{FolderID: "docs", SafeCursor: 7}, CompactedTombstones: 2}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !called {
		t.Fatalf("compact runner was not called")
	}
	if !strings.Contains(output, "metadata compacted: folder=docs") || !strings.Contains(output, "state=/tmp/state.json") {
		t.Fatalf("unexpected compact output: %q", output)
	}
}

func TestRunMetadataCommandRendersImportAndSplitOutput(t *testing.T) {
	importOutput, err := Run(cli.Options{Command: cli.CommandMetadata, Action: cli.ActionImportJSON, Path: "/tmp/source.json"}, Runners{
		ImportJSON: func(opts cli.Options) (metadataops.Result, error) {
			if opts.Path != "/tmp/source.json" {
				t.Fatalf("import runner did not receive options: %+v", opts)
			}
			return metadataops.Result{SourcePath: opts.Path, TargetPath: "/tmp/target", Folders: 1, ImportedManifests: 3}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run import returned error: %v", err)
	}
	if !strings.Contains(importOutput, "metadata import-json summary: source=/tmp/source.json target=/tmp/target folders=1 manifests=3") {
		t.Fatalf("unexpected import output: %q", importOutput)
	}

	splitOutput, err := Run(cli.Options{Command: cli.CommandMetadata, Action: cli.ActionSplitBadger, Path: "/tmp/source.badger"}, Runners{
		SplitBadger: func(opts cli.Options) (metadataops.Result, error) {
			return metadataops.Result{SourcePath: opts.Path, TargetPath: "/tmp/per-folder", Folders: 2, ImportedManifests: 4}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run split returned error: %v", err)
	}
	if !strings.Contains(splitOutput, "metadata split-badger summary: source=/tmp/source.badger target=/tmp/per-folder folders=2 manifests=4") {
		t.Fatalf("unexpected split output: %q", splitOutput)
	}
}

func TestRunMetadataCommandPropagatesRunnerErrors(t *testing.T) {
	expected := errors.New("compact failed")
	_, err := Run(cli.Options{Command: cli.CommandMetadata, Action: cli.ActionCompact}, Runners{
		Compact: func(opts cli.Options) ([]state.MetadataCompactionResult, error) {
			return nil, expected
		},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected propagated runner error, got %v", err)
	}
}

func TestRunMetadataCommandRejectsUnsupportedAction(t *testing.T) {
	_, err := Run(cli.Options{Command: cli.CommandMetadata, Action: "unknown"}, Runners{})
	if err == nil || !strings.Contains(err.Error(), "metadata action unknown not implemented") {
		t.Fatalf("expected unsupported action error, got %v", err)
	}
}
