package metadatacontrol

import (
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestCompactUsesConfiguredPeersAndSelectedFolder(t *testing.T) {
	store := state.NewJSONStore(t.TempDir() + "/state.json")
	defer store.Close()
	if err := store.SaveManifest("docs", "old.txt", block.Manifest{Path: "old.txt", Size: 3}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteManifest("docs", "old.txt"); err != nil {
		t.Fatal(err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePeerFolderState("peer-a", summary); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePeerFolderState("peer-b", summary); err != nil {
		t.Fatal(err)
	}

	results, err := Compact(config.Config{
		Folders: []config.FolderConfig{{ID: "docs"}, {ID: "photos"}},
		Peers:   []config.PeerConfig{{ID: "peer-a"}, {ID: "peer-b"}},
	}, store, "docs")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one selected folder result, got %+v", results)
	}
	result := results[0]
	if result.Plan.FolderID != "docs" || result.CompactedTombstones != 1 {
		t.Fatalf("unexpected compaction result: %+v", result)
	}
	updated, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Tombstones != 0 || updated.Cursor != summary.Cursor {
		t.Fatalf("compaction did not prune safe tombstone while preserving cursor: %+v", updated)
	}
}

func TestCompactReturnsMissingFolderError(t *testing.T) {
	store := state.NewJSONStore(t.TempDir() + "/state.json")
	defer store.Close()

	_, err := Compact(config.Config{Folders: []config.FolderConfig{{ID: "docs"}}}, store, "missing")
	if err == nil || err.Error() != "folder \"missing\" not found" {
		t.Fatalf("expected missing folder error, got %v", err)
	}
}
