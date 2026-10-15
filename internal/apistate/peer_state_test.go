package apistate

import (
	"path/filepath"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/backup"
	"filesyncengine/internal/config"
	"filesyncengine/internal/linkstats"
	"filesyncengine/internal/state"
)

func TestFolderProgressLabelsPausedUnscannedAndVerifiedShare(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		index   api.FolderIndexState
		sync    api.FolderSyncState
		bytes   int64
		percent int
		label   string
	}{
		{name: "paused", enabled: false, index: api.FolderIndexState{TotalFiles: 2, VerifiedFiles: 2}, bytes: 10, label: "Paused"},
		{name: "unscanned", enabled: true, label: "Not scanned"},
		{name: "current", enabled: true, index: api.FolderIndexState{TotalFiles: 4, VerifiedFiles: 4}, bytes: 100, percent: 100, label: "Up to date"},
		{name: "half", enabled: true, index: api.FolderIndexState{TotalFiles: 4, VerifiedFiles: 2}, bytes: 100, percent: 50, label: "50%"},
		{name: "pending cap", enabled: true, index: api.FolderIndexState{TotalFiles: 4, VerifiedFiles: 4, UnknownFiles: 1}, bytes: 100, percent: 99, label: "99%"},
		{name: "deferred", enabled: true, index: api.FolderIndexState{TotalFiles: 4, VerifiedFiles: 4}, sync: api.FolderSyncState{DeferredDeletes: 1}, bytes: 100, percent: 99, label: "99%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			percent, label := FolderProgress(tc.enabled, tc.index, tc.sync, tc.bytes)
			if percent != tc.percent || label != tc.label {
				t.Fatalf("progress = %d %q, want %d %q", percent, label, tc.percent, tc.label)
			}
		})
	}
}

func TestBuildPeerStateCopiesLinkStats(t *testing.T) {
	linkstats.Default.SetPending("stats-peer", 4096)
	t.Cleanup(func() { linkstats.Default.SetPending("stats-peer", 0) })
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	defer store.Close()

	got := BuildPeerState(config.Config{}, config.PeerConfig{ID: "stats-peer"}, store)
	if got.PendingBytes != 4096 || got.AverageBytesPerSecond != 0 {
		t.Fatalf("peer stats = %+v", got)
	}
}

func TestPeerTransferStateReportsLocalConfiguredCauses(t *testing.T) {
	state := PeerTransferState(
		config.TransferConfig{SendBytesPerSecond: 800, ReceiveBytesPerSecond: 900},
		config.PeerConfig{ID: "peer-a", SendBytesPerSecond: 400},
	)

	if state.Effective.SendBytesPerSecond != 400 {
		t.Fatalf("expected peer send cap to override global cap, got %d", state.Effective.SendBytesPerSecond)
	}
	if state.Effective.ReceiveBytesPerSecond != 900 {
		t.Fatalf("expected global receive cap when peer receive cap is unset, got %d", state.Effective.ReceiveBytesPerSecond)
	}
	if state.SendCause != "local_peer" || state.ReceiveCause != "local_global" {
		t.Fatalf("unexpected causes: send=%q receive=%q", state.SendCause, state.ReceiveCause)
	}
}

func TestPeerNetworkDiagnosticsReportsUnpromotedContainerBridgeEndpoint(t *testing.T) {
	diagnostics := PeerNetworkDiagnostics(
		config.PeerConfig{
			ID:        "peer-a",
			Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://172.18.0.5:22000"}},
		},
		config.Config{},
	)

	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", diagnostics)
	}
	if diagnostics[0].Code != "container_bridge_isolated" {
		t.Fatalf("unexpected diagnostic code: %#v", diagnostics[0])
	}
	if diagnostics[0].RoutePath != "direct" || diagnostics[0].Network != "container_bridge" {
		t.Fatalf("unexpected route/network: %#v", diagnostics[0])
	}
}

func TestBackupSnapshotStateProjectsAvailabilityStatus(t *testing.T) {
	state := BackupSnapshotState(backup.SnapshotAvailabilityStatus{
		TotalSnapshots:            3,
		MetadataSnapshots:         2,
		ArchiveProtectedSnapshots: 1,
		DBCheckpointSnapshots:     1,
		Snapshots: map[string]backup.SnapshotAvailabilitySnapshotStatus{
			"snap-a": {
				SnapshotID:            "snap-a",
				FolderID:              "folder-a",
				MetadataPresent:       true,
				DBCheckpointAvailable: true,
				ArchiveFullyProtected: false,
				Archive: backup.ArchiveProtectionSnapshotStatus{
					TotalBlocks:          5,
					ProtectedBlocks:      3,
					PendingBlocks:        1,
					FailedBlocks:         1,
					MissingArchiveBlocks: 2,
				},
			},
		},
	})

	if state.TotalSnapshots != 3 || state.MetadataSnapshots != 2 || state.ArchiveProtectedSnapshots != 1 || state.DBCheckpointSnapshots != 1 {
		t.Fatalf("unexpected aggregate snapshot state: %#v", state)
	}
	item, ok := state.Items["snap-a"]
	if !ok {
		t.Fatalf("expected snap-a item, got %#v", state.Items)
	}
	if item.SnapshotID != "snap-a" || item.FolderID != "folder-a" || !item.MetadataPresent || !item.DBCheckpointAvailable || item.ArchiveFullyProtected {
		t.Fatalf("unexpected snapshot item: %#v", item)
	}
	if item.Archive.TotalBlocks != 5 || item.Archive.ProtectedBlocks != 3 || item.Archive.PendingBlocks != 1 || item.Archive.FailedBlocks != 1 || item.Archive.MissingArchiveBlocks != 2 {
		t.Fatalf("unexpected archive projection: %#v", item.Archive)
	}
}
