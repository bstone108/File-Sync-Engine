package apistate

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestBuildConfiguredStateFallsBackToDefaultJSONStoreWhenConfiguredStoreCannotOpen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")
	opened := false
	apiState := BuildConfiguredState(ConfiguredStateBuildOptions{
		Config:     config.Config{NodeName: "fallback-node"},
		ConfigPath: configPath,
		Version:    3,
		Status:     "degraded",
		StoreOpener: func(config.Config, string) (state.JSONStore, string, error) {
			opened = true
			return state.JSONStore{}, "", errors.New("boom")
		},
	})

	if !opened {
		t.Fatalf("configured store opener was not called")
	}
	if apiState.NodeName != "fallback-node" || apiState.ConfigPath != configPath || apiState.ConfigVersion != 3 || apiState.Status != "degraded" {
		t.Fatalf("fallback state not projected from config: %+v", apiState)
	}
}

func TestBuildStateProjectsConfigAndStoreDerivedStatus(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	defer store.Close()
	if err := store.SaveManifest("docs", "notes.txt", block.Manifest{Path: "notes.txt", Size: 5}); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	if err := store.SaveSkippedDelete(state.SkippedDelete{FolderID: "docs", Path: "old.txt", Reason: "metadata_catchup_pending", RequiredMetadataCursor: 99, RequiredMetadataStateHash: "future"}); err != nil {
		t.Fatalf("save skipped delete: %v", err)
	}
	if err := store.SavePeerFolderState("peer-a", state.FolderSummary{FolderID: "docs", Cursor: 1, StateHash: "remote"}); err != nil {
		t.Fatalf("save peer state: %v", err)
	}

	startedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	apiState := BuildState(StateBuildOptions{
		Config: config.Config{
			NodeName:    "node-a",
			Maintenance: config.MaintenanceConfig{Enabled: true},
			Folders:     []config.FolderConfig{{ID: "docs", Path: "/tmp/docs", Mode: config.ModeSendReceive}},
			Peers:       []config.PeerConfig{{ID: "peer-a", Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://peer.local"}}}},
		},
		ConfigPath: filepath.Join(t.TempDir(), "config.jsonc"),
		Version:    7,
		Status:     "running",
		Store:      store,
		StartedAt:  startedAt,
	})

	if apiState.NodeName != "node-a" || apiState.ConfigVersion != 7 || apiState.Status != "running" || !apiState.StartedAt.Equal(startedAt) {
		t.Fatalf("unexpected top-level state: %+v", apiState)
	}
	if apiState.Folders != 1 || len(apiState.FoldersState) != 1 {
		t.Fatalf("folder counts not projected: %+v", apiState)
	}
	if apiState.FoldersState[0].ID != "docs" || apiState.FoldersState[0].Sync.DeferredDeletes != 1 || !apiState.FoldersState[0].Sync.MetadataCatchupPending {
		t.Fatalf("folder state not projected: %+v", apiState.FoldersState[0])
	}
	if apiState.Peers != 1 || len(apiState.PeersState) != 1 || apiState.PeersState[0].Endpoint != "manual:http://peer.local" || len(apiState.PeersState[0].Metadata.Folders) != 1 {
		t.Fatalf("peer state not projected: %+v", apiState.PeersState)
	}
	if !apiState.Maintenance.Enabled {
		t.Fatalf("maintenance enabled flag not projected")
	}
}
