package snapshotapi

import (
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestMarkerResponseListsExistingMarkers(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-1", FolderID: "docs", Cursor: 7}); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}

	response, err := MarkerResponse(api.SnapshotRequest{Action: "list", FolderID: "docs"}, config.Config{}, store, "config.jsonc", time.Now)
	if err != nil {
		t.Fatalf("MarkerResponse: %v", err)
	}
	if len(response.Markers) != 1 || response.Markers[0].ID != "snap-1" || response.Markers[0].Cursor != 7 {
		t.Fatalf("markers response mismatch: %+v", response.Markers)
	}
}

func TestMarkerResponseCreatesFolderMarkerWithInjectedClock(t *testing.T) {
	root := t.TempDir()
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 1, BlockSize: 1, Blocks: []block.Block{{Index: 0, Size: 1}}}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: filepath.Join(root, "docs"), Mode: config.ModeSendReceive}}}
	fixed := time.Date(2026, 6, 3, 4, 5, 6, 7, time.UTC)

	response, err := MarkerResponse(api.SnapshotRequest{Action: "create", FolderID: "docs", Description: "nightly"}, cfg, store, filepath.Join(root, "config.jsonc"), func() time.Time { return fixed })
	if err != nil {
		t.Fatalf("MarkerResponse create: %v", err)
	}
	if len(response.Markers) != 1 || response.Markers[0].FolderID != "docs" || response.Markers[0].Description != "nightly" || response.Markers[0].CreatedAt != fixed.Format(time.RFC3339Nano) {
		t.Fatalf("created marker response mismatch: %+v", response.Markers)
	}
}

func TestRetentionResponseAddsStartedAndFinishedTimes(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	cfg := config.Config{}
	start := time.Date(2026, 6, 3, 1, 0, 0, 0, time.UTC)
	finish := start.Add(2 * time.Second)
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return start
		}
		return finish
	}

	response, err := RetentionResponse(api.SnapshotRetentionRequest{KeepLast: 2}, cfg, store, filepath.Join(t.TempDir(), "config.jsonc"), clock)
	if err != nil {
		t.Fatalf("RetentionResponse: %v", err)
	}
	if response.KeepLast != 2 || !response.StartedAt.Equal(start) || !response.FinishedAt.Equal(finish) {
		t.Fatalf("retention timing/plan mismatch: %+v", response)
	}
}
