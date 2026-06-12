package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgraph-io/badger/v4"

	"filesyncengine/internal/block"
)

func TestJSONStorePersistsAndLoadsFolderManifests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	manifest := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1, 2, 3}}}}
	if err := store.SaveManifest("docs", "alpha.txt", manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	loaded, ok, err := store.LoadManifest("docs", "alpha.txt")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !ok {
		t.Fatalf("manifest not found")
	}
	if loaded.Size != 3 || len(loaded.Blocks) != 1 || string(loaded.Blocks[0].Hash) != string([]byte{1, 2, 3}) {
		t.Fatalf("loaded manifest mismatch: %+v", loaded)
	}
}

func TestJSONStoreListsAndDeletesFolderManifests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	beta := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{2}}}}
	if err := store.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "beta.txt", beta); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("photos", "keep.jpg", block.Manifest{Path: "keep.jpg"}); err != nil {
		t.Fatal(err)
	}

	listed, err := store.ListManifests("docs")
	if err != nil {
		t.Fatalf("ListManifests: %v", err)
	}
	if len(listed) != 2 || listed["alpha.txt"].Path != "alpha.txt" || listed["beta.txt"].Path != "beta.txt" {
		t.Fatalf("unexpected listing: %+v", listed)
	}
	listed["alpha.txt"] = block.Manifest{Path: "mutated.txt"}

	if err := store.DeleteManifest("docs", "alpha.txt"); err != nil {
		t.Fatalf("DeleteManifest: %v", err)
	}
	if _, ok, err := store.LoadManifest("docs", "alpha.txt"); err != nil || ok {
		t.Fatalf("deleted manifest still loads: ok=%v err=%v", ok, err)
	}
	if loaded, ok, err := store.LoadManifest("docs", "beta.txt"); err != nil || !ok || loaded.Path != "beta.txt" {
		t.Fatalf("remaining manifest missing/mutated: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if _, ok, err := store.LoadManifest("photos", "keep.jpg"); err != nil || !ok {
		t.Fatalf("delete leaked into other folder: ok=%v err=%v", ok, err)
	}
}

func TestJSONStoreReportsDeterministicFolderChangesSinceCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	beta := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{2}}}}
	if err := store.SaveManifest("docs", "beta.txt", beta); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	baseline, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if baseline.Cursor != 2 || baseline.Files != 2 || baseline.Tombstones != 0 || baseline.StateHash == "" {
		t.Fatalf("unexpected baseline summary: %+v", baseline)
	}
	if err := store.DeleteManifest("docs", "beta.txt"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "gamma.txt", block.Manifest{Path: "gamma.txt", Size: 5, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{3}}}}); err != nil {
		t.Fatal(err)
	}

	changes, err := store.ChangesSince("docs", baseline.Cursor)
	if err != nil {
		t.Fatalf("ChangesSince: %v", err)
	}
	if changes.FromCursor != 2 || changes.ToCursor != 4 || len(changes.Changes) != 2 {
		t.Fatalf("unexpected changes: %+v", changes)
	}
	if changes.Changes[0].Path != "beta.txt" || changes.Changes[0].Kind != ChangeDelete || changes.Changes[0].Manifest != nil {
		t.Fatalf("delete tombstone not first/deterministic: %+v", changes.Changes[0])
	}
	if changes.Changes[1].Path != "gamma.txt" || changes.Changes[1].Kind != ChangeUpsert || changes.Changes[1].Manifest == nil {
		t.Fatalf("upsert change missing manifest: %+v", changes.Changes[1])
	}
	updated, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary after changes: %v", err)
	}
	if updated.Cursor != 4 || updated.Files != 2 || updated.Tombstones != 1 || updated.StateHash == baseline.StateHash {
		t.Fatalf("summary did not reflect changed state: before=%+v after=%+v", baseline, updated)
	}
}

func TestFolderSummaryAtCursorUsesHistoricalManifestRevision(t *testing.T) {
	oldAlpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 3, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	beta := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{2}}}}
	newAlpha := block.Manifest{Path: "alpha.txt", Size: 8, BlockSize: 8, Blocks: []block.Block{{Index: 0, Size: 8, Hash: []byte{3}}}}
	want, err := folderSummary(snapshot{
		Folders:    map[string]map[string]block.Manifest{"docs": {"alpha.txt": oldAlpha, "beta.txt": beta}},
		Revisions:  map[string]map[string]uint64{"docs": {"alpha.txt": 1, "beta.txt": 2}},
		Tombstones: map[string]map[string]uint64{"docs": {}},
		Cursors:    map[string]uint64{"docs": 2},
	}, "docs")
	if err != nil {
		t.Fatalf("folderSummary expected model: %v", err)
	}
	got, err := folderSummaryAtCursor(snapshot{
		Folders: map[string]map[string]block.Manifest{"docs": {"alpha.txt": newAlpha, "beta.txt": beta}},
		ManifestHistory: map[string]map[string]map[uint64]block.Manifest{"docs": {
			"alpha.txt": {1: oldAlpha, 3: newAlpha},
			"beta.txt":  {2: beta},
		}},
		Revisions:  map[string]map[string]uint64{"docs": {"alpha.txt": 3, "beta.txt": 2}},
		Tombstones: map[string]map[string]uint64{"docs": {}},
		Cursors:    map[string]uint64{"docs": 3},
	}, "docs", 2)
	if err != nil {
		t.Fatalf("folderSummaryAtCursor: %v", err)
	}
	if got.StateHash != want.StateHash {
		t.Fatalf("historical summary used wrong manifest revision: got=%+v want=%+v", got, want)
	}
}

func TestJSONStoreReportsMoveMetadataChangeForRenamedManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	manifest := block.Manifest{Path: "old/name.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{1, 2, 3}}}}
	if err := store.SaveManifest("docs", "old/name.txt", manifest); err != nil {
		t.Fatal(err)
	}
	baseline, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}

	movedManifest := manifest
	movedManifest.Path = "new/name.txt"
	if err := store.MoveManifest("docs", "old/name.txt", "new/name.txt", movedManifest); err != nil {
		t.Fatalf("MoveManifest: %v", err)
	}

	changes, err := store.ChangesSince("docs", baseline.Cursor)
	if err != nil {
		t.Fatalf("ChangesSince: %v", err)
	}
	if changes.FromCursor != baseline.Cursor || changes.ToCursor != baseline.Cursor+1 || len(changes.Changes) != 1 {
		t.Fatalf("unexpected move changes: %+v", changes)
	}
	move := changes.Changes[0]
	if move.Kind != ChangeMove || move.FromPath != "old/name.txt" || move.Path != "new/name.txt" || move.Revision != baseline.Cursor+1 || move.Manifest == nil || move.Manifest.Path != "new/name.txt" {
		t.Fatalf("move change did not preserve rename hint and destination manifest: %+v", move)
	}
	if _, ok, err := store.LoadManifest("docs", "old/name.txt"); err != nil || ok {
		t.Fatalf("old path should be removed after move: ok=%v err=%v", ok, err)
	}
	loaded, ok, err := store.LoadManifest("docs", "new/name.txt")
	if err != nil || !ok || loaded.Path != "new/name.txt" {
		t.Fatalf("new path should load after move: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
}

func TestJSONStoreAppliesPeerMoveMetadataWithoutMutatingLocalFolder(t *testing.T) {
	store := NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	manifest := block.Manifest{Path: "new/name.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{1, 2, 3}}}}
	remoteModel := NewJSONStore(filepath.Join(t.TempDir(), "remote-model.json"))
	oldManifest := manifest
	oldManifest.Path = "old/name.txt"
	if err := remoteModel.SaveManifest("docs", "old/name.txt", oldManifest); err != nil {
		t.Fatal(err)
	}
	if err := remoteModel.MoveManifest("docs", "old/name.txt", "new/name.txt", manifest); err != nil {
		t.Fatal(err)
	}
	remoteSummary, err := remoteModel.FolderSummary("docs")
	if err != nil {
		t.Fatalf("remote FolderSummary: %v", err)
	}

	changes := FolderChanges{FolderID: "docs", FromCursor: 0, ToCursor: remoteSummary.Cursor, StateHash: remoteSummary.StateHash, Changes: []FolderChange{
		{Kind: ChangeUpsert, Path: "old/name.txt", Revision: 1, Manifest: &oldManifest},
		{Kind: ChangeMove, FromPath: "old/name.txt", Path: "new/name.txt", Revision: 2, Manifest: &manifest},
	}}
	if err := store.ApplyPeerFolderChanges("peer-b", changes); err != nil {
		t.Fatalf("ApplyPeerFolderChanges: %v", err)
	}

	if _, ok, err := store.LoadPeerManifest("peer-b", "docs", "old/name.txt"); err != nil || ok {
		t.Fatalf("old peer path should be removed by move: ok=%v err=%v", ok, err)
	}
	loaded, ok, err := store.LoadPeerManifest("peer-b", "docs", "new/name.txt")
	if err != nil || !ok || loaded.Path != "new/name.txt" {
		t.Fatalf("new peer path missing after move: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if _, ok, err := store.LoadManifest("docs", "new/name.txt"); err != nil || ok {
		t.Fatalf("peer move metadata should not mutate local manifests: ok=%v err=%v", ok, err)
	}
}

func TestJSONStorePersistsBackupSnapshotMarkers(t *testing.T) {
	store := NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 5}); err != nil {
		t.Fatal(err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	marker := SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: "2026-05-24T12:00:00Z", Description: "before cleanup"}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}
	if err := store.SaveSnapshotMarker(SnapshotMarker{ID: "snap-002", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: "2026-05-24T13:00:00Z", Deprecated: true}); err != nil {
		t.Fatalf("SaveSnapshotMarker second: %v", err)
	}

	listed, err := store.ListSnapshotMarkers("docs")
	if err != nil {
		t.Fatalf("ListSnapshotMarkers: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "snap-001" || listed[1].ID != "snap-002" {
		t.Fatalf("snapshot markers should list deterministically by creation/id: %+v", listed)
	}
	loaded, ok, err := store.LoadSnapshotMarker("snap-001")
	if err != nil || !ok {
		t.Fatalf("LoadSnapshotMarker: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.FolderID != "docs" || loaded.Cursor != summary.Cursor || loaded.StateHash != summary.StateHash || loaded.Description != "before cleanup" {
		t.Fatalf("snapshot marker did not preserve root metadata: %+v", loaded)
	}
	loaded.Pinned = true
	loaded.Deprecated = true
	if err := store.SaveSnapshotMarker(loaded); err != nil {
		t.Fatalf("SaveSnapshotMarker update: %v", err)
	}
	updated, ok, err := store.LoadSnapshotMarker("snap-001")
	if err != nil || !ok || !updated.Pinned || !updated.Deprecated {
		t.Fatalf("snapshot marker update not persisted: %+v ok=%v err=%v", updated, ok, err)
	}
	if err := store.DeleteSnapshotMarker("snap-002"); err != nil {
		t.Fatalf("DeleteSnapshotMarker: %v", err)
	}
	listed, err = store.ListSnapshotMarkers("docs")
	if err != nil {
		t.Fatalf("ListSnapshotMarkers after delete: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "snap-001" {
		t.Fatalf("snapshot marker delete affected wrong records: %+v", listed)
	}
}

func TestSnapshotManifestsPreserveHistoricalStateAfterLiveChanges(t *testing.T) {
	store := NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	oldAlpha := block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{1}}}}
	beta := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{2}}}}
	if err := store.SaveManifest("docs", "alpha.txt", oldAlpha); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "beta.txt", beta); err != nil {
		t.Fatal(err)
	}
	baseline, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	marker := SnapshotMarker{ID: "snap-before-live-change", FolderID: "docs", Cursor: baseline.Cursor, StateHash: baseline.StateHash, CreatedAt: "2026-05-24T14:00:00Z"}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}

	newAlpha := oldAlpha
	newAlpha.Size = 8
	newAlpha.Blocks = []block.Block{{Index: 0, Size: 8, Hash: []byte{9}}}
	if err := store.SaveManifest("docs", "alpha.txt", newAlpha); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteManifest("docs", "beta.txt"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "gamma.txt", block.Manifest{Path: "gamma.txt", Size: 7}); err != nil {
		t.Fatal(err)
	}

	snapshotManifests, err := store.SnapshotManifests("snap-before-live-change")
	if err != nil {
		t.Fatalf("SnapshotManifests: %v", err)
	}
	if len(snapshotManifests) != 2 {
		t.Fatalf("snapshot should preserve two baseline files, got %+v", snapshotManifests)
	}
	if snapshotManifests["alpha.txt"].Size != oldAlpha.Size || string(snapshotManifests["alpha.txt"].Blocks[0].Hash) != string(oldAlpha.Blocks[0].Hash) {
		t.Fatalf("snapshot alpha should keep historical manifest, got %+v", snapshotManifests["alpha.txt"])
	}
	if _, ok := snapshotManifests["beta.txt"]; !ok {
		t.Fatalf("snapshot should preserve beta despite live delete: %+v", snapshotManifests)
	}
	if _, ok := snapshotManifests["gamma.txt"]; ok {
		t.Fatalf("snapshot should not include post-snapshot gamma: %+v", snapshotManifests)
	}

	live, err := store.ListManifests("docs")
	if err != nil {
		t.Fatalf("ListManifests: %v", err)
	}
	if len(live) != 2 || live["alpha.txt"].Size != newAlpha.Size || live["gamma.txt"].Path != "gamma.txt" {
		t.Fatalf("live state should continue changing independently: %+v", live)
	}
}

func TestBadgerSnapshotManifestsPreserveHistoricalStateAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-snapshot-history")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	oldAlpha := block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{1}}}}
	if err := store.SaveManifest("docs", "alpha.txt", oldAlpha); err != nil {
		t.Fatal(err)
	}
	baseline, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if err := store.SaveSnapshotMarker(SnapshotMarker{ID: "snap-badger", FolderID: "docs", Cursor: baseline.Cursor, StateHash: baseline.StateHash, CreatedAt: "2026-05-24T14:30:00Z"}); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}
	newAlpha := oldAlpha
	newAlpha.Size = 9
	if err := store.SaveManifest("docs", "alpha.txt", newAlpha); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()

	snapshotManifests, err := reopened.SnapshotManifests("snap-badger")
	if err != nil {
		t.Fatalf("SnapshotManifests: %v", err)
	}
	if len(snapshotManifests) != 1 || snapshotManifests["alpha.txt"].Size != oldAlpha.Size {
		t.Fatalf("badger snapshot should preserve historical alpha after reopen: %+v", snapshotManifests)
	}
}

func TestBadgerStorePersistsBackupRetentionJobsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-retention-jobs")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	job := BackupRetentionJob{ID: "retention-1", Status: "completed", KeepLast: 2, DeletedSnapshots: 1, PromotedManifests: 3, SweptArchiveBlocks: 4, TotalOperations: 8, RemainingOperations: 0, StartedAt: "2026-05-25T12:00:00Z", UpdatedAt: "2026-05-25T12:01:00Z", CompletedAt: "2026-05-25T12:01:00Z"}
	if err := store.SaveBackupRetentionJob(job); err != nil {
		t.Fatalf("SaveBackupRetentionJob: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadBackupRetentionJob("retention-1")
	if err != nil || !ok {
		t.Fatalf("LoadBackupRetentionJob after reopen: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.Status != "completed" || loaded.KeepLast != 2 || loaded.DeletedSnapshots != 1 || loaded.PromotedManifests != 3 || loaded.SweptArchiveBlocks != 4 {
		t.Fatalf("Badger retention job did not preserve progress counters: %+v", loaded)
	}
	listed, err := reopened.ListBackupRetentionJobs()
	if err != nil || len(listed) != 1 || listed[0].ID != "retention-1" {
		t.Fatalf("ListBackupRetentionJobs mismatch: listed=%+v err=%v", listed, err)
	}
}

func TestBadgerStorePersistsBackupRepairJobsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-repair-jobs")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	job := BackupRepairJob{ID: "repair-1", Status: "completed", TotalBlocks: 3, RepairedBlocks: 2, UnresolvedBlocks: 1, RemainingBlocks: 0, StartedAt: "2026-05-25T12:00:00Z", UpdatedAt: "2026-05-25T12:01:00Z", CompletedAt: "2026-05-25T12:01:00Z", Blocks: []BackupRepairJobBlock{{SnapshotID: "snap", JobID: "archive-job", FolderID: "docs", Path: "file.txt", Hash: "abcd", Status: "repaired"}}}
	if err := store.SaveBackupRepairJob(job); err != nil {
		t.Fatalf("SaveBackupRepairJob: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadBackupRepairJob("repair-1")
	if err != nil || !ok {
		t.Fatalf("LoadBackupRepairJob after reopen: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.Status != "completed" || loaded.TotalBlocks != 3 || loaded.RepairedBlocks != 2 || loaded.UnresolvedBlocks != 1 || loaded.RemainingBlocks != 0 {
		t.Fatalf("Badger repair job did not preserve progress counters: %+v", loaded)
	}
	listed, err := reopened.ListBackupRepairJobs()
	if err != nil || len(listed) != 1 || listed[0].ID != "repair-1" || len(listed[0].Blocks) != 1 || listed[0].Blocks[0].Status != "repaired" {
		t.Fatalf("ListBackupRepairJobs mismatch: listed=%+v err=%v", listed, err)
	}
}

func TestBadgerStorePersistsBackupSnapshotMarkersAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-snapshot-markers")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 5}); err != nil {
		t.Fatal(err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	marker := SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: "2026-05-24T12:00:00Z", Description: "before cleanup"}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadSnapshotMarker("snap-001")
	if err != nil || !ok {
		t.Fatalf("LoadSnapshotMarker after reopen: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.FolderID != "docs" || loaded.Cursor != summary.Cursor || loaded.StateHash != summary.StateHash || loaded.Description != "before cleanup" {
		t.Fatalf("Badger snapshot marker did not preserve root metadata: %+v", loaded)
	}
	listed, err := reopened.ListSnapshotMarkers("docs")
	if err != nil {
		t.Fatalf("ListSnapshotMarkers after reopen: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "snap-001" {
		t.Fatalf("Badger snapshot marker list mismatch: %+v", listed)
	}
	if err := reopened.DeleteSnapshotMarker("snap-001"); err != nil {
		t.Fatalf("DeleteSnapshotMarker: %v", err)
	}
	if listed, err := reopened.ListSnapshotMarkers("docs"); err != nil || len(listed) != 0 {
		t.Fatalf("Badger snapshot marker delete mismatch: listed=%+v err=%v", listed, err)
	}
}

func TestBadgerSnapshotMarkerHotPathsDoNotRewriteWholeKeyspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-direct-snapshot-markers")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	defer store.Close()
	backend := store.backend.(badgerSnapshotBackend)
	probeKey := []byte(badgerKeyPrefix + "probe/snapshot-marker-keep")
	if err := backend.db.Update(func(txn *badger.Txn) error { return txn.Set(probeKey, []byte("still-here")) }); err != nil {
		t.Fatalf("write probe key: %v", err)
	}

	marker := SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: 7, StateHash: "hash-7", CreatedAt: "2026-05-24T12:00:00Z"}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}
	marker.Pinned = true
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("SaveSnapshotMarker update: %v", err)
	}
	if err := store.DeleteSnapshotMarker("snap-001"); err != nil {
		t.Fatalf("DeleteSnapshotMarker: %v", err)
	}

	if err := backend.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(probeKey)
		if err != nil {
			return fmt.Errorf("probe key should survive snapshot marker hot paths: %w", err)
		}
		return item.Value(func(data []byte) error {
			if string(data) != "still-here" {
				return fmt.Errorf("probe key changed to %q", string(data))
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestJSONStoreMigratesExistingManifestsIntoCursorState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacyJSON := `{"folders":{"docs":{"beta.txt":{"path":"beta.txt","size":4},"alpha.txt":{"path":"alpha.txt","size":3}}}}`
	if err := os.WriteFile(path, []byte(legacyJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewJSONStore(path)

	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if summary.Cursor != 2 || summary.Files != 2 || summary.StateHash == "" {
		t.Fatalf("legacy manifests were not assigned deterministic revisions: %+v", summary)
	}
	changes, err := store.ChangesSince("docs", 0)
	if err != nil {
		t.Fatalf("ChangesSince: %v", err)
	}
	if len(changes.Changes) != 2 || changes.Changes[0].Path != "alpha.txt" || changes.Changes[1].Path != "beta.txt" {
		t.Fatalf("legacy changes should be sorted deterministically by assigned revision: %+v", changes)
	}
}

func TestJSONStoreTracksPeerScopedStateVectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("media", "photo.jpg", block.Manifest{Path: "photo.jpg", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{2}}}}); err != nil {
		t.Fatal(err)
	}
	initialDocs, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if err := store.SavePeerFolderState("peer-b", initialDocs); err != nil {
		t.Fatalf("SavePeerFolderState: %v", err)
	}
	if err := store.SaveManifest("docs", "beta.txt", block.Manifest{Path: "beta.txt", Size: 5, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{3}}}}); err != nil {
		t.Fatal(err)
	}

	vector, err := store.PeerStateVector("peer-b")
	if err != nil {
		t.Fatalf("PeerStateVector: %v", err)
	}
	if len(vector.Folders) != 1 || vector.Folders[0].FolderID != "docs" || vector.Folders[0].Cursor != initialDocs.Cursor || vector.Folders[0].StateHash != initialDocs.StateHash {
		t.Fatalf("unexpected peer vector: %+v", vector)
	}
	statuses, err := store.PeerFolderStatuses("peer-b")
	if err != nil {
		t.Fatalf("PeerFolderStatuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected docs plus unsynced media status, got %+v", statuses)
	}
	if statuses[0].FolderID != "docs" || statuses[0].PeerCursor != initialDocs.Cursor || statuses[0].LocalCursor != 2 || statuses[0].InSync {
		t.Fatalf("docs status should show peer behind local state: %+v", statuses[0])
	}
	if statuses[1].FolderID != "media" || statuses[1].PeerCursor != 0 || statuses[1].LocalCursor != 1 || statuses[1].InSync {
		t.Fatalf("media status should show no peer cursor yet: %+v", statuses[1])
	}
}

func TestJSONStoreAppliesPeerMetadataChangesWithoutMutatingLocalFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}

	remoteModel := NewJSONStore(filepath.Join(t.TempDir(), "remote-model.json"))
	if err := remoteModel.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	if err := remoteModel.DeleteManifest("docs", "removed.txt"); err != nil {
		t.Fatal(err)
	}
	remoteSummary, err := remoteModel.FolderSummary("docs")
	if err != nil {
		t.Fatalf("remote FolderSummary: %v", err)
	}

	changes := FolderChanges{FolderID: "docs", FromCursor: 0, ToCursor: remoteSummary.Cursor, StateHash: remoteSummary.StateHash, Changes: []FolderChange{
		{Kind: ChangeUpsert, Path: "alpha.txt", Revision: 1, Manifest: &alpha},
		{Kind: ChangeDelete, Path: "removed.txt", Revision: 2},
	}}
	if err := store.ApplyPeerFolderChanges("peer-b", changes); err != nil {
		t.Fatalf("ApplyPeerFolderChanges: %v", err)
	}

	loaded, ok, err := store.LoadPeerManifest("peer-b", "docs", "alpha.txt")
	if err != nil {
		t.Fatalf("LoadPeerManifest: %v", err)
	}
	if !ok || loaded.Size != alpha.Size || len(loaded.Blocks) != 1 {
		t.Fatalf("peer manifest not applied: loaded=%+v ok=%v", loaded, ok)
	}
	if _, ok, err := store.LoadManifest("docs", "alpha.txt"); err != nil || ok {
		t.Fatalf("peer metadata application should not mutate local manifests: ok=%v err=%v", ok, err)
	}
	vector, err := store.PeerStateVector("peer-b")
	if err != nil {
		t.Fatalf("PeerStateVector: %v", err)
	}
	if len(vector.Folders) != 1 || vector.Folders[0].Cursor != remoteSummary.Cursor || vector.Folders[0].StateHash != remoteSummary.StateHash || vector.Folders[0].Files != 1 || vector.Folders[0].Tombstones != 1 {
		t.Fatalf("peer summary was not advanced to applied changes: %+v", vector)
	}
}

func TestJSONStorePersistsPeerMetadataApplyCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}

	remoteModel := NewJSONStore(filepath.Join(t.TempDir(), "remote-model.json"))
	if err := remoteModel.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	remoteSummary, err := remoteModel.FolderSummary("docs")
	if err != nil {
		t.Fatalf("remote FolderSummary: %v", err)
	}

	changes := FolderChanges{FolderID: "docs", FromCursor: 0, ToCursor: remoteSummary.Cursor, StateHash: remoteSummary.StateHash, Changes: []FolderChange{{Kind: ChangeUpsert, Path: "alpha.txt", Revision: 1, Manifest: &alpha}}}
	if err := store.ApplyPeerFolderChanges("peer-b", changes); err != nil {
		t.Fatalf("ApplyPeerFolderChanges: %v", err)
	}

	reloaded := NewJSONStore(path)
	checkpoint, ok, err := reloaded.PeerApplyCheckpoint("peer-b", "docs")
	if err != nil {
		t.Fatalf("PeerApplyCheckpoint: %v", err)
	}
	if !ok {
		t.Fatalf("expected persisted peer apply checkpoint")
	}
	if checkpoint.FromCursor != 0 || checkpoint.ToCursor != remoteSummary.Cursor || checkpoint.LastVerifiedCursor != remoteSummary.Cursor || checkpoint.LastVerifiedStateHash != remoteSummary.StateHash || checkpoint.ChangeCount != 1 {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
}

func TestJSONStoreTreatsAlreadyCommittedPeerMetadataBatchAsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}

	remoteModel := NewJSONStore(filepath.Join(t.TempDir(), "remote-model.json"))
	if err := remoteModel.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	remoteSummary, err := remoteModel.FolderSummary("docs")
	if err != nil {
		t.Fatalf("remote FolderSummary: %v", err)
	}
	changes := FolderChanges{FolderID: "docs", FromCursor: 0, ToCursor: remoteSummary.Cursor, StateHash: remoteSummary.StateHash, Changes: []FolderChange{{Kind: ChangeUpsert, Path: "alpha.txt", Revision: 1, Manifest: &alpha}}}

	if err := store.ApplyPeerFolderChanges("peer-b", changes); err != nil {
		t.Fatalf("first ApplyPeerFolderChanges: %v", err)
	}
	reloaded := NewJSONStore(path)
	if err := reloaded.ApplyPeerFolderChanges("peer-b", changes); err != nil {
		t.Fatalf("already committed batch should resume as noop, got %v", err)
	}
}

func TestJSONStoreRejectsOutOfOrderPeerMetadataChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	summary := FolderSummary{FolderID: "docs", Cursor: 1, Files: 1, StateHash: "existing"}
	if err := store.SavePeerFolderState("peer-b", summary); err != nil {
		t.Fatalf("SavePeerFolderState: %v", err)
	}

	err := store.ApplyPeerFolderChanges("peer-b", FolderChanges{FolderID: "docs", FromCursor: 0, ToCursor: 2, StateHash: "bad", Changes: []FolderChange{{Kind: ChangeUpsert, Path: "alpha.txt", Revision: 2, Manifest: &alpha}}})
	if err == nil || !strings.Contains(err.Error(), "cursor mismatch") {
		t.Fatalf("expected cursor mismatch, got %v", err)
	}
	if _, ok, err := store.LoadPeerManifest("peer-b", "docs", "alpha.txt"); err != nil || ok {
		t.Fatalf("out-of-order changes should not be applied: ok=%v err=%v", ok, err)
	}
}

func TestJSONStorePlansAndAppliesSafeMetadataCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	beta := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{2}}}}
	if err := store.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteManifest("docs", "alpha.txt"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "beta.txt", beta); err != nil {
		t.Fatal(err)
	}
	current, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if current.Cursor != 3 || current.Tombstones != 1 {
		t.Fatalf("unexpected setup summary: %+v", current)
	}
	if err := store.SavePeerFolderState("peer-current", current); err != nil {
		t.Fatalf("SavePeerFolderState current: %v", err)
	}
	if err := store.SavePeerFolderState("peer-behind", FolderSummary{FolderID: "docs", Cursor: 1, StateHash: "behind"}); err != nil {
		t.Fatalf("SavePeerFolderState behind: %v", err)
	}

	blocked, err := store.MetadataCompactionPlan("docs", MetadataCompactionPolicy{PeerIDs: []string{"peer-current", "peer-behind"}})
	if err != nil {
		t.Fatalf("MetadataCompactionPlan blocked: %v", err)
	}
	if blocked.SafeCursor != 1 || blocked.EligibleTombstones != 0 || len(blocked.BlockedPeers) != 1 || blocked.BlockedPeers[0] != "peer-behind" {
		t.Fatalf("behind peer should block tombstone compaction: %+v", blocked)
	}

	if err := store.SavePeerFolderState("peer-behind", current); err != nil {
		t.Fatalf("SavePeerFolderState caught up: %v", err)
	}
	plan, err := store.MetadataCompactionPlan("docs", MetadataCompactionPolicy{PeerIDs: []string{"peer-current", "peer-behind"}})
	if err != nil {
		t.Fatalf("MetadataCompactionPlan ready: %v", err)
	}
	if plan.SafeCursor != 3 || plan.EligibleTombstones != 1 || plan.RetainedTombstones != 0 {
		t.Fatalf("unexpected ready compaction plan: %+v", plan)
	}
	result, err := store.CompactFolderMetadata("docs", MetadataCompactionPolicy{PeerIDs: []string{"peer-current", "peer-behind"}})
	if err != nil {
		t.Fatalf("CompactFolderMetadata: %v", err)
	}
	if result.CompactedTombstones != 1 || result.Snapshot.Cursor != current.Cursor || result.Snapshot.StateHash != current.StateHash {
		t.Fatalf("compaction should record pre-prune snapshot and compact one tombstone: %+v", result)
	}
	updated, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary after compact: %v", err)
	}
	if updated.Cursor != current.Cursor || updated.Tombstones != 0 || updated.Files != 1 {
		t.Fatalf("compaction should preserve cursor/files while pruning safe tombstones: %+v", updated)
	}
	snapshots, err := store.MetadataCompactionSnapshots("docs")
	if err != nil {
		t.Fatalf("MetadataCompactionSnapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Cursor != current.Cursor || snapshots[0].StateHash != current.StateHash || snapshots[0].CompactedTombstones != 1 {
		t.Fatalf("snapshot not persisted: %+v", snapshots)
	}
}

func TestJSONStoreReportsFullRefreshNeededWhenRequestedCursorWasCompacted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	if err := store.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteManifest("docs", "alpha.txt"); err != nil {
		t.Fatal(err)
	}
	current, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if err := store.SavePeerFolderState("peer-current", current); err != nil {
		t.Fatalf("SavePeerFolderState: %v", err)
	}
	if _, err := store.CompactFolderMetadata("docs", MetadataCompactionPolicy{PeerIDs: []string{"peer-current"}}); err != nil {
		t.Fatalf("CompactFolderMetadata: %v", err)
	}

	_, err = store.ChangesSince("docs", 0)
	var compacted *MetadataCompactedError
	if !errors.As(err, &compacted) {
		t.Fatalf("expected MetadataCompactedError, got %v", err)
	}
	if compacted.FolderID != "docs" || compacted.RequestedCursor != 0 || compacted.SafeCursor != current.Cursor || compacted.SnapshotStateHash != current.StateHash {
		t.Fatalf("unexpected compacted error details: %+v", compacted)
	}

	changes, err := store.ChangesSince("docs", current.Cursor)
	if err != nil {
		t.Fatalf("current cursor should not require full refresh: %v", err)
	}
	if changes.FromCursor != current.Cursor || changes.ToCursor != current.Cursor || len(changes.Changes) != 0 {
		t.Fatalf("unexpected current-cursor changes: %+v", changes)
	}
}

func TestJSONStoreReplacesPeerFolderFromFullRefresh(t *testing.T) {
	store := NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.ReplacePeerFolderFromFullRefresh("peer-a", "docs", FolderSummary{FolderID: "docs", Cursor: 3, Files: 1, Tombstones: 0, StateHash: "old-state"}, map[string]block.Manifest{
		"old.txt": {Path: "old.txt", Size: 1, BlockSize: 1, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 1, Hash: []byte{1}}}},
	}, map[string]uint64{"old.txt": 3}); err != nil {
		t.Fatalf("initial ReplacePeerFolderFromFullRefresh: %v", err)
	}

	if err := store.ReplacePeerFolderFromFullRefresh("peer-a", "docs", FolderSummary{FolderID: "docs", Cursor: 7, Files: 1, Tombstones: 0, StateHash: "remote-current"}, map[string]block.Manifest{
		"new.txt": {Path: "new.txt", Size: 3, BlockSize: 3, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{2}}}},
	}, map[string]uint64{"new.txt": 7}); err != nil {
		t.Fatalf("ReplacePeerFolderFromFullRefresh: %v", err)
	}

	if _, ok, err := store.LoadPeerManifest("peer-a", "docs", "old.txt"); err != nil || ok {
		t.Fatalf("old peer manifest should be replaced: ok=%v err=%v", ok, err)
	}
	manifest, ok, err := store.LoadPeerManifest("peer-a", "docs", "new.txt")
	if err != nil || !ok {
		t.Fatalf("new peer manifest missing: ok=%v err=%v", ok, err)
	}
	if manifest.Path != "new.txt" || manifest.Size != 3 {
		t.Fatalf("unexpected full-refresh manifest: %+v", manifest)
	}
	vector, err := store.PeerStateVector("peer-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(vector.Folders) != 1 || vector.Folders[0].Cursor != 7 || vector.Folders[0].StateHash != "remote-current" {
		t.Fatalf("peer state was not replaced with advertised full-refresh summary: %+v", vector)
	}
}

func TestJSONStorePersistsApplyDeleteGateState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	hash := []byte{0xaa}
	manifest := block.Manifest{Path: "new.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: hash}}}

	if err := store.SavePendingWrite(PendingWrite{FolderID: "docs", Path: "new.txt", Manifest: manifest, RequiredMetadataCursor: 7, RequiredMetadataStateHash: "state-7"}); err != nil {
		t.Fatalf("SavePendingWrite: %v", err)
	}
	if err := store.AddVerifiedStagedBlock("docs", "new.txt", VerifiedStagedBlock{Index: 0, Offset: 0, Size: 4, Hash: hash}); err != nil {
		t.Fatalf("AddVerifiedStagedBlock: %v", err)
	}
	if err := store.SaveSkippedDelete(SkippedDelete{FolderID: "docs", Path: "old.txt", RequiredMetadataCursor: 7, RequiredMetadataStateHash: "state-7", RequiredWrites: []string{"new.txt"}, Reason: "waiting for metadata and writes"}); err != nil {
		t.Fatalf("SaveSkippedDelete: %v", err)
	}

	reloaded := NewJSONStore(path)
	write, ok, err := reloaded.PendingWrite("docs", "new.txt")
	if err != nil || !ok {
		t.Fatalf("pending write did not persist: ok=%v err=%v", ok, err)
	}
	if write.RequiredMetadataCursor != 7 || write.RequiredMetadataStateHash != "state-7" || write.Manifest.Path != "new.txt" || len(write.VerifiedBlocks) != 1 || write.VerifiedBlocks[0].Index != 0 || write.Committed {
		t.Fatalf("unexpected pending write: %+v", write)
	}
	deletes, err := reloaded.ReadySkippedDeletes("docs", FolderSummary{FolderID: "docs", Cursor: 7, StateHash: "state-7"})
	if err != nil {
		t.Fatalf("ReadySkippedDeletes before committed write: %v", err)
	}
	if len(deletes) != 0 {
		t.Fatalf("delete became ready before required write commit: %+v", deletes)
	}
	if err := reloaded.MarkPendingWriteCommitted("docs", "new.txt"); err != nil {
		t.Fatalf("MarkPendingWriteCommitted: %v", err)
	}
	deletes, err = NewJSONStore(path).ReadySkippedDeletes("docs", FolderSummary{FolderID: "docs", Cursor: 7, StateHash: "state-7"})
	if err != nil {
		t.Fatalf("ReadySkippedDeletes after committed write: %v", err)
	}
	if len(deletes) != 1 || deletes[0].Path != "old.txt" || deletes[0].Reason == "" {
		t.Fatalf("skipped delete should be ready after metadata and writes are complete: %+v", deletes)
	}
}

func TestFindBlocksDoesNotAdvertiseDamagedManifests(t *testing.T) {
	store := NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	damaged := block.Manifest{Path: "damaged.bin", Size: 4, BlockSize: 4, Damaged: true, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	healthy := block.Manifest{Path: "healthy.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	if err := store.SaveManifest("docs", "damaged.bin", damaged); err != nil {
		t.Fatalf("SaveManifest damaged: %v", err)
	}
	if err := store.SaveManifest("docs", "healthy.bin", healthy); err != nil {
		t.Fatalf("SaveManifest healthy: %v", err)
	}

	refs, err := store.FindBlocks(4, []byte{0xaa})
	if err != nil {
		t.Fatalf("FindBlocks: %v", err)
	}
	if len(refs) != 1 || refs[0].RelativePath != "healthy.bin" {
		t.Fatalf("refs=%+v, want only healthy manifest advertised for reuse", refs)
	}
}

func TestBadgerFindBlocksDoesNotAdvertiseDamagedManifests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-damaged-block-index")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	damaged := block.Manifest{Path: "damaged.bin", Size: 4, BlockSize: 4, Damaged: true, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	healthy := block.Manifest{Path: "healthy.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	if err := store.SaveManifest("docs", "damaged.bin", damaged); err != nil {
		t.Fatalf("SaveManifest damaged: %v", err)
	}
	if err := store.SaveManifest("docs", "healthy.bin", healthy); err != nil {
		t.Fatalf("SaveManifest healthy: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()
	refs, err := reopened.FindBlocks(4, []byte{0xaa})
	if err != nil {
		t.Fatalf("FindBlocks: %v", err)
	}
	if len(refs) != 1 || refs[0].RelativePath != "healthy.bin" {
		t.Fatalf("refs=%+v, want only healthy manifest advertised for reuse", refs)
	}
}

func TestBadgerStorePersistsMetadataAndBlockIndexAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-state")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	manifest := block.Manifest{Path: "alpha.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	if err := store.SaveManifest("docs", "alpha.bin", manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if err := store.SavePendingWrite(PendingWrite{FolderID: "docs", Path: "alpha.bin", Manifest: manifest, RequiredMetadataCursor: 1}); err != nil {
		t.Fatalf("SavePendingWrite: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadManifest("docs", "alpha.bin")
	if err != nil || !ok || loaded.Path != "alpha.bin" {
		t.Fatalf("manifest did not persist across reopen: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	refs, err := reopened.FindBlocks(4, []byte{0xaa})
	if err != nil || len(refs) != 1 || refs[0].RelativePath != "alpha.bin" {
		t.Fatalf("block index lookup should work on durable backend: refs=%+v err=%v", refs, err)
	}
	write, ok, err := reopened.PendingWrite("docs", "alpha.bin")
	if err != nil || !ok || write.RequiredMetadataCursor != 1 {
		t.Fatalf("pending write did not persist across reopen: write=%+v ok=%v err=%v", write, ok, err)
	}
}

func TestBadgerStoreMigratesLegacySnapshotToKeyLevelSchemaOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-legacy-migration")
	legacyManifest := block.Manifest{Path: "legacy.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xdd}}}}
	legacySnapshot := snapshot{
		Folders:   map[string]map[string]block.Manifest{"docs": {"legacy.bin": legacyManifest}},
		Revisions: map[string]map[string]uint64{"docs": {"legacy.bin": 5}},
		Cursors:   map[string]uint64{"docs": 5},
	}
	data, err := json.Marshal(legacySnapshot)
	if err != nil {
		t.Fatalf("marshal legacy snapshot: %v", err)
	}
	db, err := badger.Open(badger.DefaultOptions(path).WithLogger(nil))
	if err != nil {
		t.Fatalf("open raw badger: %v", err)
	}
	if err := db.Update(func(txn *badger.Txn) error { return txn.Set(badgerSnapshotKey, data) }); err != nil {
		t.Fatalf("write legacy snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw badger: %v", err)
	}

	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	loaded, ok, err := store.LoadManifest("docs", "legacy.bin")
	if err != nil || !ok || loaded.Path != "legacy.bin" {
		t.Fatalf("legacy manifest should be available through direct hot paths after open migration: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	refs, err := store.FindBlocks(4, []byte{0xdd})
	if err != nil || len(refs) != 1 || refs[0].RelativePath != "legacy.bin" {
		t.Fatalf("legacy block index should be rebuilt into key-level schema: refs=%+v err=%v", refs, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err = badger.Open(badger.DefaultOptions(path).WithLogger(nil))
	if err != nil {
		t.Fatalf("reopen raw badger: %v", err)
	}
	defer db.Close()
	err = db.View(func(txn *badger.Txn) error {
		if _, err := txn.Get(badgerSnapshotKey); !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("legacy whole-snapshot key should be removed after migration: %v", err)
		}
		for _, key := range [][]byte{badgerManifestKey("docs", "legacy.bin"), badgerRevisionKey("docs", "legacy.bin"), badgerCursorKey("docs"), badgerBlockIndexKey(4, []byte{0xdd}, "docs", "legacy.bin", 0)} {
			if _, err := txn.Get(key); err != nil {
				return fmt.Errorf("expected migrated key-level record %q: %w", key, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBadgerStoreUsesKeyLevelSchemaForCoreRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-key-schema")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	manifest := block.Manifest{Path: "nested/alpha.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	if err := store.SaveManifest("docs", "nested/alpha.bin", manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := badger.Open(badger.DefaultOptions(path).WithLogger(nil))
	if err != nil {
		t.Fatalf("open raw badger: %v", err)
	}
	defer db.Close()
	err = db.View(func(txn *badger.Txn) error {
		if _, err := txn.Get(badgerSnapshotKey); !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("legacy whole-snapshot key should not exist: %v", err)
		}
		for _, key := range [][]byte{
			badgerManifestKey("docs", "nested/alpha.bin"),
			badgerRevisionKey("docs", "nested/alpha.bin"),
			badgerCursorKey("docs"),
			badgerBlockIndexKey(4, []byte{0xaa}, "docs", "nested/alpha.bin", 0),
		} {
			if _, err := txn.Get(key); err != nil {
				return fmt.Errorf("expected key-level record %q: %w", key, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBadgerLocalManifestHotPathsDoNotRewriteWholeKeyspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-direct-local")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	initial := block.Manifest{Path: "alpha.bin", Size: 8, BlockSize: 4, Blocks: []block.Block{
		{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}},
		{Index: 1, Offset: 4, Size: 4, Hash: []byte{0xbb}},
	}}
	if err := store.SaveManifest("docs", "alpha.bin", initial); err != nil {
		t.Fatalf("SaveManifest initial: %v", err)
	}
	backend := store.backend.(badgerSnapshotBackend)
	probeKey := []byte(badgerKeyPrefix + "probe/keep")
	if err := backend.db.Update(func(txn *badger.Txn) error { return txn.Set(probeKey, []byte("still-here")) }); err != nil {
		t.Fatalf("write probe key: %v", err)
	}

	updated := block.Manifest{Path: "alpha.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xcc}}}}
	if err := store.SaveManifest("docs", "alpha.bin", updated); err != nil {
		t.Fatalf("SaveManifest update: %v", err)
	}
	refs, err := store.FindBlocks(4, []byte{0xaa})
	if err != nil {
		t.Fatalf("FindBlocks stale hash: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("old block-index entry survived manifest update: %+v", refs)
	}
	refs, err = store.FindBlocks(4, []byte{0xcc})
	if err != nil || len(refs) != 1 || refs[0].RelativePath != "alpha.bin" {
		t.Fatalf("new block-index entry missing after manifest update: refs=%+v err=%v", refs, err)
	}
	if err := store.DeleteManifest("docs", "alpha.bin"); err != nil {
		t.Fatalf("DeleteManifest: %v", err)
	}
	refs, err = store.FindBlocks(4, []byte{0xcc})
	if err != nil || len(refs) != 0 {
		t.Fatalf("deleted manifest still contributes block-index entries: refs=%+v err=%v", refs, err)
	}
	err = backend.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(probeKey)
		if err != nil {
			return err
		}
		return item.Value(func(data []byte) error {
			if string(data) != "still-here" {
				return fmt.Errorf("probe key value changed to %q", data)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("direct local operations should not clear unrelated key-level records: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBadgerPeerMetadataHotPathsDoNotRewriteWholeKeyspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-direct-peer")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	backend := store.backend.(badgerSnapshotBackend)
	probeKey := []byte(badgerKeyPrefix + "probe/peer-keep")
	if err := backend.db.Update(func(txn *badger.Txn) error { return txn.Set(probeKey, []byte("still-here")) }); err != nil {
		t.Fatalf("write probe key: %v", err)
	}
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	beta := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{2}}}}
	remoteModel := NewJSONStore(filepath.Join(t.TempDir(), "remote-model.json"))
	if err := remoteModel.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	if err := remoteModel.SaveManifest("docs", "beta.txt", beta); err != nil {
		t.Fatal(err)
	}
	remoteSummary, err := remoteModel.FolderSummary("docs")
	if err != nil {
		t.Fatalf("remote FolderSummary: %v", err)
	}
	changes := FolderChanges{FolderID: "docs", FromCursor: 0, ToCursor: remoteSummary.Cursor, StateHash: remoteSummary.StateHash, Changes: []FolderChange{
		{Kind: ChangeUpsert, Path: "alpha.txt", Revision: 1, Manifest: &alpha},
		{Kind: ChangeUpsert, Path: "beta.txt", Revision: 2, Manifest: &beta},
	}}
	if err := store.ApplyPeerFolderChanges("peer-b", changes); err != nil {
		t.Fatalf("ApplyPeerFolderChanges: %v", err)
	}
	if err := store.SavePeerFolderState("peer-c", FolderSummary{FolderID: "docs", Cursor: 9, Files: 2, StateHash: "peer-c-state"}); err != nil {
		t.Fatalf("SavePeerFolderState: %v", err)
	}
	if err := store.ReplacePeerFolderFromFullRefresh("peer-b", "docs", FolderSummary{FolderID: "docs", Cursor: 3, Files: 1, StateHash: "refresh-state"}, map[string]block.Manifest{
		"gamma.txt": {Path: "gamma.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{3}}}},
	}, map[string]uint64{"gamma.txt": 3}); err != nil {
		t.Fatalf("ReplacePeerFolderFromFullRefresh: %v", err)
	}
	if _, ok, err := store.LoadPeerManifest("peer-b", "docs", "alpha.txt"); err != nil || ok {
		t.Fatalf("full refresh should remove old peer manifest: ok=%v err=%v", ok, err)
	}
	loaded, ok, err := store.LoadPeerManifest("peer-b", "docs", "gamma.txt")
	if err != nil || !ok || loaded.Path != "gamma.txt" {
		t.Fatalf("full-refresh peer manifest missing: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	checkpoint, ok, err := store.PeerApplyCheckpoint("peer-b", "docs")
	if err != nil || !ok || checkpoint.ToCursor != 3 || checkpoint.LastVerifiedStateHash != "refresh-state" {
		t.Fatalf("peer checkpoint missing after direct full-refresh write: checkpoint=%+v ok=%v err=%v", checkpoint, ok, err)
	}
	err = backend.db.View(func(txn *badger.Txn) error {
		if _, err := txn.Get(probeKey); err != nil {
			return fmt.Errorf("probe key should survive direct peer metadata operations: %w", err)
		}
		for _, key := range [][]byte{
			badgerPeerStateKey("peer-b", "docs"),
			badgerPeerManifestKey("peer-b", "docs", "gamma.txt"),
			badgerPeerRevisionKey("peer-b", "docs", "gamma.txt"),
			badgerPeerCursorKey("peer-b", "docs"),
			badgerPeerApplyCheckpointKey("peer-b", "docs"),
			badgerPeerStateKey("peer-c", "docs"),
		} {
			if _, err := txn.Get(key); err != nil {
				return fmt.Errorf("expected direct peer key %q: %w", key, err)
			}
		}
		if _, err := txn.Get(badgerPeerManifestKey("peer-b", "docs", "alpha.txt")); !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("old peer manifest key should be removed by direct full refresh: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBadgerApplyGateAndCompactionHotPathsDoNotRewriteWholeKeyspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-direct-apply-gates")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	backend := store.backend.(badgerSnapshotBackend)
	manifest := block.Manifest{Path: "new.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	if err := store.SavePendingWrite(PendingWrite{FolderID: "docs", Path: "new.txt", Manifest: manifest, RequiredMetadataCursor: 2, RequiredMetadataStateHash: "state-2"}); err != nil {
		t.Fatalf("SavePendingWrite: %v", err)
	}
	probeKey := []byte(badgerKeyPrefix + "probe/apply-gate-keep")
	if err := backend.db.Update(func(txn *badger.Txn) error { return txn.Set(probeKey, []byte("still-here")) }); err != nil {
		t.Fatalf("write probe key: %v", err)
	}
	if err := store.AddVerifiedStagedBlock("docs", "new.txt", VerifiedStagedBlock{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}); err != nil {
		t.Fatalf("AddVerifiedStagedBlock: %v", err)
	}
	if err := store.MarkPendingWriteCommitted("docs", "new.txt"); err != nil {
		t.Fatalf("MarkPendingWriteCommitted: %v", err)
	}
	if err := store.SaveSkippedDelete(SkippedDelete{FolderID: "docs", Path: "old.txt", RequiredMetadataCursor: 2, RequiredMetadataStateHash: "state-2", RequiredWrites: []string{"new.txt"}, Reason: "metadata_catchup_pending"}); err != nil {
		t.Fatalf("SaveSkippedDelete: %v", err)
	}
	ready, err := store.ReadySkippedDeletes("docs", FolderSummary{FolderID: "docs", Cursor: 2, StateHash: "state-2"})
	if err != nil || len(ready) != 1 || ready[0].Path != "old.txt" {
		t.Fatalf("ReadySkippedDeletes: ready=%+v err=%v", ready, err)
	}
	if err := store.RemoveSkippedDelete("docs", "old.txt"); err != nil {
		t.Fatalf("RemoveSkippedDelete: %v", err)
	}
	if err := store.SaveManifest("docs", "stale.txt", block.Manifest{Path: "stale.txt", Size: 1, BlockSize: 1, Blocks: []block.Block{{Index: 0, Size: 1, Hash: []byte{0x01}}}}); err != nil {
		t.Fatalf("SaveManifest stale: %v", err)
	}
	if err := store.DeleteManifest("docs", "stale.txt"); err != nil {
		t.Fatalf("DeleteManifest stale: %v", err)
	}
	if err := store.SavePeerFolderState("peer-a", FolderSummary{FolderID: "docs", Cursor: 2, StateHash: "peer-current"}); err != nil {
		t.Fatalf("SavePeerFolderState: %v", err)
	}
	result, err := store.CompactFolderMetadata("docs", MetadataCompactionPolicy{PeerIDs: []string{"peer-a"}})
	if err != nil || result.CompactedTombstones != 1 {
		t.Fatalf("CompactFolderMetadata: result=%+v err=%v", result, err)
	}
	snapshots, err := store.MetadataCompactionSnapshots("docs")
	if err != nil || len(snapshots) != 1 || snapshots[0].CompactedTombstones != 1 {
		t.Fatalf("MetadataCompactionSnapshots: snapshots=%+v err=%v", snapshots, err)
	}
	err = backend.db.View(func(txn *badger.Txn) error {
		if _, err := txn.Get(probeKey); err != nil {
			return fmt.Errorf("probe key should survive direct apply-gate/compaction operations: %w", err)
		}
		for _, key := range [][]byte{
			badgerPendingWriteKey("docs", "new.txt"),
			badgerCompactionSnapshotKey("docs", 0),
		} {
			if _, err := txn.Get(key); err != nil {
				return fmt.Errorf("expected direct key-level record %q: %w", key, err)
			}
		}
		if _, err := txn.Get(badgerSkippedDeleteKey("docs", "old.txt")); !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("removed skipped delete key should be absent: %v", err)
		}
		if _, err := txn.Get(badgerTombstoneKey("docs", "stale.txt")); !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("compacted tombstone key should be absent: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestPerFolderAggregateBlockLookupDoesNotRequireSnapshotLoad(t *testing.T) {
	sharedHash := []byte{0xaa, 0xbb, 0xcc}
	store := JSONStore{backend: perFolderBadgerBackend{stores: map[string]JSONStore{
		"docs":  {backend: blockLookupOnlyBackend{refs: []BlockRef{{FolderID: "docs", RelativePath: "alpha.bin", Block: block.Block{Index: 0, Size: 4, Hash: sharedHash}}}}},
		"media": {backend: blockLookupOnlyBackend{refs: []BlockRef{{FolderID: "media", RelativePath: "beta.bin", Block: block.Block{Index: 1, Size: 4, Hash: sharedHash}}}}},
	}}}

	matches, err := store.FindBlocks(4, sharedHash)
	if err != nil {
		t.Fatalf("FindBlocks should use child block indexes without snapshot loads: %v", err)
	}
	if len(matches) != 2 || matches[0].FolderID != "docs" || matches[1].FolderID != "media" {
		t.Fatalf("unexpected aggregate matches: %+v", matches)
	}
}

func TestPerFolderBadgerStoreFindsBlocksByContentHashAcrossFolders(t *testing.T) {
	root := t.TempDir()
	store, err := NewPerFolderBadgerStore(map[string]string{
		"docs":  filepath.Join(root, "docs.badger"),
		"media": filepath.Join(root, "media.badger"),
	})
	if err != nil {
		t.Fatalf("NewPerFolderBadgerStore: %v", err)
	}
	defer store.Close()
	sharedHash := []byte{0xaa, 0xbb, 0xcc}
	if err := store.SaveManifest("docs", "alpha.bin", block.Manifest{Path: "alpha.bin", BlockSize: 4, Blocks: []block.Block{
		{Index: 0, Offset: 0, Size: 4, Hash: sharedHash},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("media", "nested/beta.bin", block.Manifest{Path: "nested/beta.bin", BlockSize: 4, Blocks: []block.Block{
		{Index: 2, Offset: 8, Size: 4, Hash: sharedHash},
	}}); err != nil {
		t.Fatal(err)
	}

	matches, err := store.FindBlocks(4, sharedHash)
	if err != nil {
		t.Fatalf("FindBlocks: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected two shared block matches, got %+v", matches)
	}
	if matches[0].FolderID != "docs" || matches[0].RelativePath != "alpha.bin" {
		t.Fatalf("first match not stable/sorted docs alpha: %+v", matches[0])
	}
	if matches[1].FolderID != "media" || matches[1].RelativePath != "nested/beta.bin" {
		t.Fatalf("second match not stable/sorted media beta: %+v", matches[1])
	}

	docsRefs, err := store.ListBlockRefs("docs")
	if err != nil {
		t.Fatalf("ListBlockRefs docs: %v", err)
	}
	if len(docsRefs) != 1 || docsRefs[0].FolderID != "docs" || docsRefs[0].RelativePath != "alpha.bin" {
		t.Fatalf("folder-filtered refs did not stay scoped to docs: %+v", docsRefs)
	}
}

func TestJSONStorePersistsPerNodeSettingsDocumentsWithoutCrossNodeApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	doc := NodeSettingsDocument{
		NodeID:      "node-a",
		Revision:    7,
		UpdatedAt:   "2026-05-31T12:00:00Z",
		Settings:    map[string]any{"logging.level": "warn", "transfer.sendBytesPerSecond": float64(1024)},
		Source:      "local-config",
		ApplyStatus: "owned",
	}
	if err := store.SaveNodeSettingsDocument("node-b", doc); err == nil {
		t.Fatalf("saving a settings document under a different node id must fail closed")
	}
	if err := store.SaveNodeSettingsDocument("node-a", doc); err != nil {
		t.Fatalf("SaveNodeSettingsDocument: %v", err)
	}

	reopened := NewJSONStore(path)
	loaded, ok, err := reopened.LoadNodeSettingsDocument("node-a")
	if err != nil || !ok {
		t.Fatalf("LoadNodeSettingsDocument: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.NodeID != "node-a" || loaded.Revision != 7 || loaded.ApplyStatus != "owned" {
		t.Fatalf("unexpected loaded document: %+v", loaded)
	}
	if got := loaded.Settings["logging.level"]; got != "warn" {
		t.Fatalf("settings not preserved: %+v", loaded.Settings)
	}

	docs, err := reopened.ListNodeSettingsDocuments()
	if err != nil {
		t.Fatalf("ListNodeSettingsDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].NodeID != "node-a" {
		t.Fatalf("documents should be listed deterministically by owner node only: %+v", docs)
	}
}

func TestBadgerStorePersistsPerNodeSettingsDocumentsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-node-settings")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	if err := store.SaveNodeSettingsDocument("node-b", NodeSettingsDocument{NodeID: "node-a", Revision: 1}); err == nil {
		t.Fatalf("badger settings documents must reject owner-key mismatches")
	}
	if err := store.SaveNodeSettingsDocument("node-a", NodeSettingsDocument{
		NodeID:      "node-a",
		Revision:    3,
		UpdatedAt:   "2026-05-31T12:00:00Z",
		Settings:    map[string]any{"metadata.perFolder": true},
		Source:      "api",
		ApplyStatus: "owned",
	}); err != nil {
		t.Fatalf("SaveNodeSettingsDocument: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadNodeSettingsDocument("node-a")
	if err != nil || !ok || loaded.Revision != 3 {
		t.Fatalf("settings document did not persist: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	docs, err := reopened.ListNodeSettingsDocuments()
	if err != nil || len(docs) != 1 || docs[0].NodeID != "node-a" {
		t.Fatalf("badger document list mismatch: docs=%+v err=%v", docs, err)
	}
}

func TestJSONStorePersistsPendingSettingsChangesByTargetAndIdempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	change := PendingSettingsChange{
		ID:             "change-2",
		TargetNodeID:   "node-b",
		OriginNodeID:   "node-a",
		IdempotencyKey: "node-a:change-2",
		Revision:       4,
		Status:         "pending",
		CreatedAt:      "2026-05-31T13:00:00Z",
		UpdatedAt:      "2026-05-31T13:00:00Z",
		SettingsPatch:  map[string]any{"logging.level": "debug"},
	}
	if err := store.SavePendingSettingsChange("node-c", change); err == nil {
		t.Fatalf("saving a pending settings change under a different target node must fail closed")
	}
	if err := store.SavePendingSettingsChange("node-b", change); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}
	if err := store.SavePendingSettingsChange("node-b", PendingSettingsChange{ID: "change-1", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:change-1", Revision: 3, Status: "acked"}); err != nil {
		t.Fatalf("SavePendingSettingsChange second: %v", err)
	}

	reopened := NewJSONStore(path)
	loaded, ok, err := reopened.LoadPendingSettingsChange("node-b", "change-2")
	if err != nil || !ok {
		t.Fatalf("LoadPendingSettingsChange: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.IdempotencyKey != "node-a:change-2" || loaded.Status != "pending" || loaded.SettingsPatch["logging.level"] != "debug" {
		t.Fatalf("pending change fields not preserved: %+v", loaded)
	}
	changes, err := reopened.ListPendingSettingsChanges("node-b")
	if err != nil {
		t.Fatalf("ListPendingSettingsChanges: %v", err)
	}
	if len(changes) != 2 || changes[0].ID != "change-1" || changes[1].ID != "change-2" {
		t.Fatalf("pending changes should list deterministically by target/id: %+v", changes)
	}
	allChanges, err := reopened.ListPendingSettingsChanges("")
	if err != nil || len(allChanges) != 2 {
		t.Fatalf("all pending changes mismatch: changes=%+v err=%v", allChanges, err)
	}
}

func TestJSONStoreAppliesPendingSettingsChangeIdempotentlyToTargetDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	if err := store.SaveNodeSettingsDocument("node-b", NodeSettingsDocument{NodeID: "node-b", Revision: 5, Settings: map[string]any{"logging.level": "info", "nodeName": "old"}}); err != nil {
		t.Fatalf("SaveNodeSettingsDocument: %v", err)
	}
	if err := store.SavePendingSettingsChange("node-b", PendingSettingsChange{ID: "change-apply", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-b:apply", Revision: 9, Status: "queued", CreatedAt: "2026-05-31T15:59:00Z", SettingsPatch: map[string]any{"logging.level": "warn", "transfer.receiveBytesPerSecond": float64(4096)}}); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}

	doc, change, err := store.ApplyPendingSettingsChange("node-b", "change-apply", "2026-05-31T16:00:00Z")
	if err != nil {
		t.Fatalf("ApplyPendingSettingsChange: %v", err)
	}
	if doc.NodeID != "node-b" || doc.Revision != 9 || doc.ApplyStatus != "applied" || doc.Settings["logging.level"] != "warn" || doc.Settings["nodeName"] != "old" || doc.Settings["transfer.receiveBytesPerSecond"] != float64(4096) {
		t.Fatalf("document not patched as owner-owned state: %+v", doc)
	}
	if change.Status != "applied" || change.UpdatedAt != "2026-05-31T16:00:00Z" {
		t.Fatalf("change not marked applied: %+v", change)
	}
	if len(change.AuditTrail) != 2 || change.AuditTrail[0].Transition != "queued" || change.AuditTrail[0].At != "2026-05-31T15:59:00Z" || change.AuditTrail[1].Transition != "applied" || change.AuditTrail[1].At != "2026-05-31T16:00:00Z" {
		t.Fatalf("settings change audit trail missing queue/apply transitions: %+v", change.AuditTrail)
	}

	doc, change, err = store.ApplyPendingSettingsChange("node-b", "change-apply", "2026-05-31T16:01:00Z")
	if err != nil {
		t.Fatalf("second ApplyPendingSettingsChange: %v", err)
	}
	if doc.Revision != 9 || change.UpdatedAt != "2026-05-31T16:00:00Z" {
		t.Fatalf("second apply must be idempotent without rewriting revision/timestamps: doc=%+v change=%+v", doc, change)
	}
	if _, _, err := store.ApplyPendingSettingsChange("node-c", "change-apply", "2026-05-31T16:02:00Z"); err == nil {
		t.Fatalf("different target node must not apply another node's pending settings change")
	}
}

func TestJSONStoreRejectsUnauthorizedPendingSettingsChangeOrigin(t *testing.T) {
	store := NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveNodeSettingsDocument("node-b", NodeSettingsDocument{NodeID: "node-b", Revision: 5, Settings: map[string]any{"mesh.authorizedSettingsPeers": []any{"node-a"}}}); err != nil {
		t.Fatalf("SaveNodeSettingsDocument: %v", err)
	}
	if err := store.SavePendingSettingsChange("node-b", PendingSettingsChange{ID: "unauthorized-change", TargetNodeID: "node-b", OriginNodeID: "node-c", IdempotencyKey: "node-c:node-b:unauthorized", Revision: 9, Status: "queued", SettingsPatch: map[string]any{"logging.level": "debug"}}); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}
	if _, _, err := store.ApplyPendingSettingsChange("node-b", "unauthorized-change", "2026-06-01T11:00:00Z"); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("expected unauthorized origin rejection, got %v", err)
	}
	doc, ok, err := store.LoadNodeSettingsDocument("node-b")
	if err != nil || !ok {
		t.Fatalf("LoadNodeSettingsDocument: doc=%+v ok=%v err=%v", doc, ok, err)
	}
	if doc.Settings["logging.level"] == "debug" {
		t.Fatalf("unauthorized change modified document: %+v", doc)
	}
	change, ok, err := store.LoadPendingSettingsChange("node-b", "unauthorized-change")
	if err != nil || !ok {
		t.Fatalf("LoadPendingSettingsChange: change=%+v ok=%v err=%v", change, ok, err)
	}
	if change.Status != "failed" || !strings.Contains(change.LastError, "not authorized") {
		t.Fatalf("unauthorized change was not failed with audit-safe reason: %+v", change)
	}
}

func TestJSONStoreRejectsStalePendingSettingsChangeWithoutOverwritingOwnerDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	if err := store.SaveNodeSettingsDocument("node-b", NodeSettingsDocument{NodeID: "node-b", Revision: 10, Settings: map[string]any{"logging.level": "error", "nodeName": "current"}}); err != nil {
		t.Fatalf("SaveNodeSettingsDocument: %v", err)
	}
	if err := store.SavePendingSettingsChange("node-b", PendingSettingsChange{ID: "stale-change", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-b:stale", Revision: 9, Status: "queued", SettingsPatch: map[string]any{"logging.level": "debug"}}); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}

	if _, _, err := store.ApplyPendingSettingsChange("node-b", "stale-change", "2026-06-01T10:00:00Z"); err == nil || !strings.Contains(err.Error(), "stale pending settings change") {
		t.Fatalf("stale pending settings change should fail closed, got err=%v", err)
	}
	loaded, ok, err := store.LoadNodeSettingsDocument("node-b")
	if err != nil || !ok {
		t.Fatalf("LoadNodeSettingsDocument: doc=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.Revision != 10 || loaded.Settings["logging.level"] != "error" || loaded.Settings["nodeName"] != "current" {
		t.Fatalf("stale remote edit overwrote the owner document: %+v", loaded)
	}
	change, ok, err := store.LoadPendingSettingsChange("node-b", "stale-change")
	if err != nil || !ok {
		t.Fatalf("LoadPendingSettingsChange: change=%+v ok=%v err=%v", change, ok, err)
	}
	if change.Status != "failed" || !strings.Contains(change.LastError, "stale") {
		t.Fatalf("stale change should be durably marked failed for GUI/mesh status: %+v", change)
	}
}

func TestBadgerStorePersistsPendingSettingsChangesAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badger-pending-settings")
	store, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	if err := store.SavePendingSettingsChange("node-b", PendingSettingsChange{ID: "badger-change", TargetNodeID: "node-a", OriginNodeID: "node-c", IdempotencyKey: "idem-1", Status: "pending"}); err == nil {
		t.Fatalf("badger pending settings changes must reject target mismatches")
	}
	if err := store.SavePendingSettingsChange("node-a", PendingSettingsChange{ID: "badger-change", TargetNodeID: "node-a", OriginNodeID: "node-c", IdempotencyKey: "idem-1", Revision: 9, Status: "delivered", CreatedAt: "2026-06-01T09:00:00Z", SettingsPatch: map[string]any{"transfer.receiveBytesPerSecond": float64(2048)}}); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}
	if err := store.UpdatePendingSettingsChangeStatus("node-a", "badger-change", "acked", "2026-06-01T09:05:00Z", ""); err != nil {
		t.Fatalf("UpdatePendingSettingsChangeStatus: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewBadgerStore(path)
	if err != nil {
		t.Fatalf("reopen NewBadgerStore: %v", err)
	}
	defer reopened.Close()
	loaded, ok, err := reopened.LoadPendingSettingsChange("node-a", "badger-change")
	if err != nil || !ok || loaded.Revision != 9 || loaded.Status != "acked" {
		t.Fatalf("badger pending setting change did not persist: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if len(loaded.AuditTrail) != 2 || loaded.AuditTrail[0].Transition != "delivered" || loaded.AuditTrail[1].Transition != "acked" || loaded.AuditTrail[1].At != "2026-06-01T09:05:00Z" {
		t.Fatalf("badger settings change audit trail did not persist transitions: %+v", loaded.AuditTrail)
	}
	changes, err := reopened.ListPendingSettingsChanges("node-a")
	if err != nil || len(changes) != 1 || changes[0].ID != "badger-change" {
		t.Fatalf("badger pending setting changes list mismatch: changes=%+v err=%v", changes, err)
	}
}

func TestJSONStoreFindsBlocksByContentHashAcrossFolders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewJSONStore(path)
	sharedHash := []byte{0xaa, 0xbb, 0xcc}
	otherHash := []byte{0x01}
	if err := store.SaveManifest("docs", "alpha.bin", block.Manifest{Path: "alpha.bin", BlockSize: 4, Blocks: []block.Block{
		{Index: 0, Offset: 0, Size: 4, Hash: sharedHash},
		{Index: 1, Offset: 4, Size: 4, Hash: otherHash},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("media", "nested/beta.bin", block.Manifest{Path: "nested/beta.bin", BlockSize: 4, Blocks: []block.Block{
		{Index: 2, Offset: 8, Size: 4, Hash: sharedHash},
	}}); err != nil {
		t.Fatal(err)
	}

	matches, err := store.FindBlocks(4, sharedHash)
	if err != nil {
		t.Fatalf("FindBlocks: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected two shared block matches, got %+v", matches)
	}
	if matches[0].FolderID != "docs" || matches[0].RelativePath != "alpha.bin" || matches[0].Block.Index != 0 {
		t.Fatalf("first match not stable/sorted docs alpha: %+v", matches[0])
	}
	if matches[1].FolderID != "media" || matches[1].RelativePath != "nested/beta.bin" || matches[1].Block.Index != 2 {
		t.Fatalf("second match not stable/sorted media beta: %+v", matches[1])
	}

	if err := store.DeleteManifest("docs", "alpha.bin"); err != nil {
		t.Fatal(err)
	}
	matches, err = store.FindBlocks(4, sharedHash)
	if err != nil {
		t.Fatalf("FindBlocks after delete: %v", err)
	}
	if len(matches) != 1 || matches[0].FolderID != "media" {
		t.Fatalf("deleted manifest still contributes block matches: %+v", matches)
	}
}

type blockLookupOnlyBackend struct {
	refs []BlockRef
}

func (b blockLookupOnlyBackend) Load() (snapshot, error) {
	return snapshot{}, fmt.Errorf("snapshot load should not be required for aggregate block lookup")
}

func (b blockLookupOnlyBackend) Save(snapshot) error { return nil }
func (b blockLookupOnlyBackend) Close() error        { return nil }

func (b blockLookupOnlyBackend) FindBlocks(size int, hash []byte) ([]BlockRef, error) {
	refs := make([]BlockRef, 0)
	for _, ref := range b.refs {
		if ref.Block.Size == size && string(ref.Block.Hash) == string(hash) {
			refs = append(refs, ref)
		}
	}
	return sortBlockRefs(refs), nil
}

func (b blockLookupOnlyBackend) ListBlockRefs(folderID string) ([]BlockRef, error) {
	refs := make([]BlockRef, 0)
	for _, ref := range b.refs {
		if folderID == "" || ref.FolderID == folderID {
			refs = append(refs, ref)
		}
	}
	return sortBlockRefs(refs), nil
}
