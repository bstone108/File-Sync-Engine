package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/nat"
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
