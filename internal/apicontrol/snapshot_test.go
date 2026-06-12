package apicontrol

import (
	"reflect"
	"testing"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

func TestSnapshotMarkersResponseProjectsStateMarkers(t *testing.T) {
	response := SnapshotMarkersResponse([]state.SnapshotMarker{{
		ID:          "snap-1",
		FolderID:    "folder-a",
		Cursor:      42,
		StateHash:   "hash-a",
		CreatedAt:   "2026-06-02T00:00:00Z",
		Description: "before update",
		Pinned:      true,
		Deprecated:  true,
	}})

	if len(response.Markers) != 1 {
		t.Fatalf("expected one marker, got %d", len(response.Markers))
	}
	marker := response.Markers[0]
	if marker.ID != "snap-1" || marker.FolderID != "folder-a" || marker.Cursor != 42 || marker.StateHash != "hash-a" || marker.Description != "before update" || !marker.Pinned || !marker.Deprecated {
		t.Fatalf("snapshot marker was not projected correctly: %#v", marker)
	}
}

func TestRestorePlanResponseProjectsFilesAndMissingBlocks(t *testing.T) {
	missing := block.Block{Index: 7, Offset: 4096, Size: 1024, Hash: []byte("missing-hash")}
	response := RestorePlanResponse(backup.RestorePlan{
		SnapshotID:    "snap-2",
		FolderID:      "folder-b",
		Destination:   "/restore",
		DryRun:        true,
		TotalFiles:    2,
		TotalBytes:    99,
		MissingBlocks: 1,
		Files: []backup.RestorePlanFile{{
			Path:             "docs/a.txt",
			DestinationPath:  "/restore/docs/a.txt",
			Size:             99,
			Blocks:           2,
			ArchiveAvailable: false,
			MissingBlocks:    []block.Block{missing},
		}},
	})

	if response.SnapshotID != "snap-2" || response.FolderID != "folder-b" || !response.DryRun || response.TotalBytes != 99 || response.MissingBlocks != 1 {
		t.Fatalf("restore plan summary was not projected correctly: %#v", response)
	}
	if len(response.Files) != 1 || response.Files[0].Path != "docs/a.txt" || response.Files[0].DestinationPath != "/restore/docs/a.txt" || !reflect.DeepEqual(response.Files[0].MissingBlocks[0], missing) {
		t.Fatalf("restore plan files were not projected correctly: %#v", response.Files)
	}
}

func TestRestoreAndRetentionResponsesProjectProgressCounters(t *testing.T) {
	restore := RestoreResponse(backup.RestoreResult{JobID: "restore-job", SnapshotID: "snap-3", FolderID: "folder-c", Destination: "/out", TotalFiles: 4, RestoredFiles: 2, RestoredBytes: 128, SkippedFiles: 1, RemainingFiles: 1})
	if restore.JobID != "restore-job" || restore.RestoredFiles != 2 || restore.SkippedFiles != 1 || restore.RemainingFiles != 1 {
		t.Fatalf("restore result was not projected correctly: %#v", restore)
	}

	retention := SnapshotRetentionResponse(backup.SnapshotRetentionPlan{JobID: "retention-job", KeepLast: 3, DeprecateSnapshots: []string{"old"}, DeleteSnapshots: []string{"older"}, Promotions: []backup.SnapshotRetentionPromotion{{Path: "a.txt"}}, ArchiveBlocksEligibleForSweep: []backup.ArchiveBlockRef{{Hash: "h1"}, {Hash: "h2"}}})
	if retention.JobID != "retention-job" || retention.KeepLast != 3 || len(retention.DeprecatedSnapshots) != 1 || len(retention.DeletedSnapshots) != 1 || retention.PromotedManifests != 1 || retention.SweepEligibleBlocks != 2 {
		t.Fatalf("retention plan was not projected correctly: %#v", retention)
	}
}
