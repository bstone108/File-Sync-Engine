package apistate

import (
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/block"
	"filesyncengine/internal/engine"
	"filesyncengine/internal/state"
)

func TestFolderSyncStateProjectsDeferredDeleteCatchup(t *testing.T) {
	store := state.NewJSONStore(t.TempDir() + "/state.json")
	if err := store.SaveManifest("docs", "current.txt", block.Manifest{Size: 7}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSkippedDelete(state.SkippedDelete{
		FolderID:                  "docs",
		Path:                      "stale.txt",
		RequiredMetadataCursor:    5,
		RequiredMetadataStateHash: "peer-current",
		Reason:                    "metadata_catchup_pending",
	}); err != nil {
		t.Fatal(err)
	}

	sync, err := FolderSyncState(store, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if sync.LocalCursor == 0 || sync.LocalStateHash == "" {
		t.Fatalf("local metadata summary missing from folder sync state: %+v", sync)
	}
	if sync.DeferredDeletes != 1 || sync.ReadyDeferredDeletes != 0 || !sync.MetadataCatchupPending {
		t.Fatalf("deferred delete metadata catch-up state not projected: %+v", sync)
	}
}

func TestFolderWarningStateProjectsOnlyUncommittedLockedApplies(t *testing.T) {
	store := state.NewJSONStore(t.TempDir() + "/state.json")
	for _, write := range []state.PendingWrite{
		{FolderID: "docs", Path: "locked.txt", Reason: "locked_apply_pending"},
		{FolderID: "docs", Path: "write-locked.txt", Reason: "write_locked"},
		{FolderID: "docs", Path: "committed.txt", Reason: "locked_apply_pending", Committed: true},
		{FolderID: "docs", Path: "other.txt", Reason: "metadata_catchup_pending"},
	} {
		if err := store.SavePendingWrite(write); err != nil {
			t.Fatal(err)
		}
	}

	warnings, err := FolderWarningState(store, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if warnings.PendingLockedApplies != 2 || len(warnings.Recent) != 2 {
		t.Fatalf("locked apply warnings not projected: %+v", warnings)
	}
	if warnings.Recent[0].Kind != "locked_apply_pending" || warnings.Recent[0].Path != "locked.txt" {
		t.Fatalf("first warning mismatch: %+v", warnings.Recent[0])
	}
	if warnings.Recent[1].Kind != "locked_apply_pending" || warnings.Recent[1].Path != "write-locked.txt" {
		t.Fatalf("second warning mismatch: %+v", warnings.Recent[1])
	}
}

func TestPeerMetadataAndFolderIndexProjection(t *testing.T) {
	peer := PeerMetadataState([]state.PeerFolderStatus{{FolderID: "docs", PeerCursor: 1, PeerStateHash: "peer", LocalCursor: 2, LocalStateHash: "local", InSync: false}})
	if len(peer.Folders) != 1 || peer.Folders[0].FolderID != "docs" || peer.Folders[0].PeerStateHash != "peer" || peer.Folders[0].LocalCursor != 2 || peer.Folders[0].InSync {
		t.Fatalf("peer metadata projection mismatch: %+v", peer)
	}

	index := FolderIndexState(engine.FolderIndexState{Mode: "quick", TotalFiles: 3, VerifiedFiles: 1, UnknownFiles: 2, UnverifiedSeedFiles: 1, KnownBlocks: 4, BadBlocks: 1, QueuedHashJobs: 2, ActiveHashJobs: 1, DateCorrectionsPending: 1, ProvisionalReadOnly: true})
	want := api.FolderIndexState{Mode: "quick", TotalFiles: 3, VerifiedFiles: 1, UnknownFiles: 2, UnverifiedSeedFiles: 1, KnownBlocks: 4, BadBlocks: 1, QueuedHashJobs: 2, ActiveHashJobs: 1, DateCorrectionsPending: 1, ProvisionalReadOnly: true}
	if index != want {
		t.Fatalf("folder index projection mismatch: got %+v want %+v", index, want)
	}
}
