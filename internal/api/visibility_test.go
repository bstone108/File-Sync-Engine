package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/nat"
	"filesyncengine/internal/transferprogress"
)

func TestPeersRouteOmitsSelfStaleAndRemoved(t *testing.T) {
	nat.DefaultPaths.Reset()
	t.Cleanup(nat.DefaultPaths.Reset)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfg := config.Config{
		NodeName: "kiki",
		API:      config.APIConfig{Listen: "127.0.0.1:22420", Key: "secret"},
		Peers: []config.PeerConfig{{
			ID: "neko", APIKey: "remote",
		}},
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	nat.DefaultPaths.Observe("neko", "https://138.199.21.250:22420", nat.PathDirect)
	nat.DefaultPaths.Forget("neko")
	server := NewServer(State{
		NodeName:          "kiki",
		IdentityPublicKey: "kiki-public",
		ConfigPath:        configPath,
		PeersState: []PeerState{
			{ID: "kiki", Status: "configured", ConnectionPath: "direct"},
			{ID: "kiki-public", Status: "configured"},
			{ID: "stale-restart", Status: "configured", ConnectionPath: "direct"},
			{ID: "removed", Status: "configured"},
			{ID: "neighbor", Status: "discovered", Endpoint: "discovered:https://65.109.35.170:4001"},
			{ID: "neko", Status: "configured", ConnectionPath: "direct"},
		},
	}, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/peers", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	var peers []PeerState
	if err := json.Unmarshal(w.Body.Bytes(), &peers); err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].ID != "neko" || peers[0].ConnectionPath != "" {
		t.Fatalf("peers = %+v body=%s", peers, w.Body.String())
	}
}

func TestFolderTotalFilesComesFromTheJournal(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfg := config.Config{
		NodeName: "kiki",
		API:      config.APIConfig{Listen: "127.0.0.1:22420", Key: "secret"},
		Folders:  []config.FolderConfig{{ID: "docs", Path: filepath.Join(dir, "docs"), Enabled: true, Mode: config.ModeSendReceive}},
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dir, "sync-journals", "docs.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"cursor\":2,\"entries\":[{\"path\":\"a.txt\",\"size\":1,\"revision\":1},{\"path\":\"b.txt\",\"deleted\":true,\"revision\":2}]}\n")
	if err := os.WriteFile(journal, body, 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{
		ConfigPath:   configPath,
		FoldersState: []FolderState{{ID: "docs", Index: FolderIndexState{TotalFiles: 0}}},
	}, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/folders", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	var folders []FolderState
	if err := json.Unmarshal(w.Body.Bytes(), &folders); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(folders) != 1 || folders[0].Index.TotalFiles != 1 {
		t.Fatalf("folders = %+v", folders)
	}
}

func TestFolderJournalReplacesEmptyScan(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfg := config.Config{
		NodeName: "kiki",
		API:      config.APIConfig{Listen: "127.0.0.1:22420", Key: "secret"},
		Folders:  []config.FolderConfig{{ID: "docs", Path: filepath.Join(dir, "docs"), Enabled: true, Mode: config.ModeSendReceive}},
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dir, "sync-journals", "docs.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"entries\":[{\"path\":\"a.txt\",\"sha256\":\"abc\",\"size\":4,\"blocks\":[{\"index\":0,\"offset\":0,\"size\":4,\"hash\":\"abc\"}]}]}\n")
	if err := os.WriteFile(journal, body, 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{
		ConfigPath: configPath,
		FoldersState: []FolderState{{
			ID:            "docs",
			ProgressLabel: "Not scanned",
			Sync:          FolderSyncState{LocalStateHash: emptyStateHash},
		}},
	}, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/folders", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	var folders []FolderState
	if err := json.Unmarshal(w.Body.Bytes(), &folders); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(folders) != 1 {
		t.Fatalf("folders = %+v", folders)
	}
	folder := folders[0]
	if folder.Index.VerifiedFiles != 1 || folder.Index.KnownBlocks != 1 || folder.LocalBytes != 4 {
		t.Fatalf("index = %+v bytes=%d", folder.Index, folder.LocalBytes)
	}
	if folder.Sync.LocalStateHash == "" || folder.Sync.LocalStateHash == emptyStateHash {
		t.Fatalf("state hash = %s", folder.Sync.LocalStateHash)
	}
	if folder.ProgressLabel != "Up to date" {
		t.Fatalf("progress = %q", folder.ProgressLabel)
	}
}

func TestLimitedRelayPathIsVisibleToTheGUI(t *testing.T) {
	nat.DefaultPaths.Reset()
	t.Cleanup(nat.DefaultPaths.Reset)
	nat.DefaultPaths.ObserveLimited("neko", "/ip4/198.51.100.8/tcp/4001/p2p/relay/p2p-circuit")
	peers := peersVisibleToGUI(State{PeersState: []PeerState{{ID: "neko", Status: "configured"}}})
	if len(peers) != 1 || peers[0].ConnectionPath != "limited-relay" {
		t.Fatalf("peers = %+v", peers)
	}
	if len(peers[0].NetworkDiagnostics) != 1 || peers[0].NetworkDiagnostics[0].Code != "limited-relay" || peers[0].NetworkDiagnostics[0].Guidance == "" {
		t.Fatalf("diagnostics = %+v", peers[0].NetworkDiagnostics)
	}
}

func TestTransfersReportLiveBytesWithoutInventingARoute(t *testing.T) {
	transferprogress.Reset()
	t.Cleanup(transferprogress.Reset)
	transferprogress.Note("neko", 1000, 5000)
	time.Sleep(20 * time.Millisecond)
	transferprogress.Note("neko", 1000, 4000)
	server := NewServer(State{NodeName: "kiki"}, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/transfers", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	var response TransferReadModel
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.ByteProgressAvailable || !response.LiveRatesAvailable {
		t.Fatalf("live progress flags = %+v", response)
	}
	if len(response.Active) != 1 || response.Active[0].PeerID != "neko" {
		t.Fatalf("active = %+v", response.Active)
	}
	item := response.Active[0]
	if item.ReceivedBytes != 2000 || item.RemainingBytes != 4000 || item.BytesPerSecond <= 0 || item.RoutePath != "" {
		t.Fatalf("item = %+v", item)
	}
}

func TestTransferCountsDoNotClaimDirectWhenNothingMoved(t *testing.T) {
	server := NewServer(State{NodeName: "kiki"}, "secret")
	server.Publish(Event{Type: "peer.sync.finished", FolderID: "docs", PeerID: "neko", Message: "writes=2 deletes=0 moves=0 blocksFetched=3 blocksReused=4 routePath=relay"})
	req := httptest.NewRequest(http.MethodGet, "/v1/transfers", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	var response TransferReadModel
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.History) != 1 {
		t.Fatalf("history = %+v", response.History)
	}
	item := response.History[0]
	if item.Writes != 2 || item.BlocksFetched != 3 || item.BlocksReused != 4 || item.RoutePath != "relay" {
		t.Fatalf("item = %+v", item)
	}
}
