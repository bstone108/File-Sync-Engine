package apicontrol

import (
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/foldersync"
)

func TestApplyFolderWarningProjectionsUpdatesMatchingFoldersAndEvents(t *testing.T) {
	state := api.State{FoldersState: []api.FolderState{{ID: "docs"}, {ID: "media"}}}
	warnings := []foldersync.InaccessibleWarning{
		{FolderID: "docs", Role: "source", Path: "locked.txt", Error: "permission denied"},
		{FolderID: "missing", Role: "target", Path: "gone.txt", Error: "not found"},
	}

	updated, events := ApplyFolderWarningProjections(state, warnings)

	if updated.FoldersState[0].Warnings.InaccessibleFiles != 1 {
		t.Fatalf("docs inaccessible count = %d", updated.FoldersState[0].Warnings.InaccessibleFiles)
	}
	if len(updated.FoldersState[0].Warnings.Recent) != 1 {
		t.Fatalf("docs recent warning count = %d", len(updated.FoldersState[0].Warnings.Recent))
	}
	warning := updated.FoldersState[0].Warnings.Recent[0]
	if warning.Kind != "inaccessible" || warning.Path != "locked.txt" {
		t.Fatalf("unexpected warning: %#v", warning)
	}
	if warning.Message != "source scan could not read locked.txt: permission denied" {
		t.Fatalf("warning message = %q", warning.Message)
	}
	if updated.FoldersState[1].Warnings.InaccessibleFiles != 0 || len(updated.FoldersState[1].Warnings.Recent) != 0 {
		t.Fatalf("unrelated folder mutated: %#v", updated.FoldersState[1].Warnings)
	}
	if len(events) != 2 {
		t.Fatalf("events count = %d", len(events))
	}
	if events[0].Type != "folder.warning" || events[0].FolderID != "docs" || events[0].Path != "locked.txt" || events[0].Message != warning.Message {
		t.Fatalf("first event = %#v", events[0])
	}
	if events[1].FolderID != "missing" || events[1].Message != "target scan could not read gone.txt: not found" {
		t.Fatalf("missing-folder event = %#v", events[1])
	}
}

func TestApplyFolderWarningProjectionsNoopsWithoutWarnings(t *testing.T) {
	state := api.State{FoldersState: []api.FolderState{{ID: "docs"}}}

	updated, events := ApplyFolderWarningProjections(state, nil)

	if len(events) != 0 {
		t.Fatalf("events count = %d", len(events))
	}
	if updated.FoldersState[0].Warnings.InaccessibleFiles != 0 || len(updated.FoldersState[0].Warnings.Recent) != 0 {
		t.Fatalf("state mutated: %#v", updated.FoldersState[0].Warnings)
	}
}
