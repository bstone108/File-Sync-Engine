package clioutput

import (
	"testing"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

func TestSnapshotMarkersOutputRendersRowsAndSummary(t *testing.T) {
	out := SnapshotMarkersOutput([]state.SnapshotMarker{
		{ID: "snap-1", FolderID: "docs", Cursor: 7, StateHash: "hash-1", Pinned: true, Deprecated: false, CreatedAt: "2026-06-03T00:00:00Z", Description: "baseline"},
		{ID: "snap-2", FolderID: "docs", Cursor: 8, StateHash: "hash-2", Pinned: false, Deprecated: true, CreatedAt: "2026-06-03T01:00:00Z", Description: "old"},
	})

	want := "snapshot: id=snap-1 folder=docs cursor=7 stateHash=hash-1 pinned=true deprecated=false created=2026-06-03T00:00:00Z description=\"baseline\"\n" +
		"snapshot: id=snap-2 folder=docs cursor=8 stateHash=hash-2 pinned=false deprecated=true created=2026-06-03T01:00:00Z description=\"old\"\n" +
		"snapshot summary: count=2\n"
	if out != want {
		t.Fatalf("unexpected snapshot marker output:\nwant %q\n got %q", want, out)
	}
}

func TestSnapshotDeletedOutputRendersDeletedID(t *testing.T) {
	out := SnapshotDeletedOutput("snap-1")

	want := "snapshot deleted: id=snap-1\n"
	if out != want {
		t.Fatalf("unexpected snapshot delete output:\nwant %q\n got %q", want, out)
	}
}

func TestRestorePlanOutputRendersFileRowsAndSummary(t *testing.T) {
	out := RestorePlanOutput(backup.RestorePlan{
		SnapshotID:    "snap-1",
		FolderID:      "docs",
		Destination:   "/restore",
		DryRun:        true,
		TotalFiles:    1,
		TotalBytes:    42,
		MissingBlocks: 1,
		Files: []backup.RestorePlanFile{{
			Path:             "dir/a.txt",
			DestinationPath:  "/restore/dir/a.txt",
			Size:             42,
			Blocks:           2,
			ArchiveAvailable: false,
			MissingBlocks:    []block.Block{{}},
		}},
	})

	want := "restore-plan file: path=dir/a.txt destination=/restore/dir/a.txt size=42 blocks=2 archiveAvailable=false missingBlocks=1\n" +
		"restore-plan summary: snapshot=snap-1 folder=docs destination=/restore dryRun=true files=1 bytes=42 missingBlocks=1\n"
	if out != want {
		t.Fatalf("unexpected restore plan output:\nwant %q\n got %q", want, out)
	}
}

func TestRestoreResultOutputRendersProgressSummary(t *testing.T) {
	out := RestoreResultOutput(backup.RestoreResult{
		SnapshotID:     "snap-1",
		FolderID:       "docs",
		Destination:    "/restore",
		TotalFiles:     3,
		RestoredFiles:  1,
		RestoredBytes:  99,
		SkippedFiles:   1,
		RemainingFiles: 1,
	})

	want := "restore summary: snapshot=snap-1 folder=docs destination=/restore files=3 restored=1 bytes=99 skipped=1 remaining=1\n"
	if out != want {
		t.Fatalf("unexpected restore result output:\nwant %q\n got %q", want, out)
	}
}

func TestSnapshotRetentionOutputRendersJobSummary(t *testing.T) {
	out := SnapshotRetentionOutput(backup.SnapshotRetentionPlan{
		JobID:                         "retention-job",
		KeepLast:                      2,
		DeprecateSnapshots:            []string{"old"},
		DeleteSnapshots:               []string{"older"},
		Promotions:                    []backup.SnapshotRetentionPromotion{{FromSnapshotID: "old", ToSnapshotID: "new", FolderID: "docs", Path: "a.txt"}},
		ArchiveBlocksEligibleForSweep: []backup.ArchiveBlockRef{{Hash: "aaa", Size: 1}, {Hash: "bbb", Size: 2}},
	})

	want := "snapshot retention summary: jobId=retention-job keepLast=2 deprecated=1 deleted=1 promoted=1 sweepEligibleBlocks=2\n"
	if out != want {
		t.Fatalf("unexpected snapshot retention output:\nwant %q\n got %q", want, out)
	}
}
