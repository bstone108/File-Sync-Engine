package foldershare

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestReconcileCopiesNewerRemoteRevision(t *testing.T) {
	remoteRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "note.txt"), []byte("from-remote"), 0o644); err != nil {
		t.Fatal(err)
	}
	remoteConfig := filepath.Join(t.TempDir(), "remote.json")
	if err := os.WriteFile(remoteConfig, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	remoteCfg := config.Config{
		NodeName: "remote",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: remoteRoot, Enabled: true, Mode: config.ModeSendReceive,
		}},
	}
	Reconcile(remoteCfg, remoteConfig, nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync-journal":
			http.ServeFile(w, r, JournalPath(remoteConfig, "shared"))
		case "/v1/folder-block":
			data, err := os.ReadFile(filepath.Join(remoteRoot, r.URL.Query().Get("path")))
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			serveBlockPayload(w, r, data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	localRoot := t.TempDir()
	localConfig := filepath.Join(t.TempDir(), "local.json")
	localCfg := config.Config{
		NodeName: "local",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: localRoot, Enabled: true, Mode: config.ModeSendReceive,
		}},
		Peers: []config.PeerConfig{{
			ID:     "remote",
			APIKey: "secret",
			Endpoints: []config.EndpointConfig{{
				Kind: "manual", Address: server.URL,
			}},
		}},
	}
	Reconcile(localCfg, localConfig, nil)
	got, err := os.ReadFile(filepath.Join(localRoot, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from-remote" {
		t.Fatalf("synced body = %q", got)
	}

	incompatible := config.Config{Folders: []config.FolderConfig{{
		ID: "other", Path: t.TempDir(), Enabled: true, Mode: config.ModeSendOnly,
	}}}
	Reconcile(incompatible, filepath.Join(t.TempDir(), "sendonly.json"), nil)
}

func TestApplyRemoteCountsDeletesAndMoves(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "gone"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone", "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stats PassStats
	SetPassReporter(func(folderID string, peer config.PeerConfig, got PassStats) {
		if folderID != "shared" || peer.ID != "remote" {
			t.Errorf("report %s %s", folderID, peer.ID)
		}
		stats = got
	})
	t.Cleanup(func() { SetPassReporter(nil) })
	byPath := map[string]Entry{
		"old.txt":    {Path: "old.txt", SHA256: "same-hash", Size: 4, Revision: 1},
		"gone/x.txt": {Path: "gone/x.txt", SHA256: "x-hash", Size: 1, Revision: 1},
	}
	remote := []Entry{
		{Path: "old.txt", Deleted: true, Revision: 2},
		{Path: "renamed.txt", SHA256: "same-hash", Size: 4, Revision: 2},
		{Path: "gone/x.txt", Deleted: true, Revision: 2},
	}
	folder := config.FolderConfig{ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendReceive}
	if err := applyRemote(config.PeerConfig{ID: "remote"}, folder, byPath, remote); err != nil {
		t.Fatal(err)
	}
	if stats.Moves != 1 || stats.Deletes != 1 || stats.Writes != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file stat = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "gone", "x.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file stat = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "renamed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "same" {
		t.Fatalf("moved body = %q", got)
	}
}

func TestJournalIndexCountsLiveFiles(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "node.json")
	journal := JournalPath(configPath, "docs")
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"entries":[{"path":"a.txt","sha256":"abc","size":4,"blocks":[{"index":0,"offset":0,"size":4,"hash":"abc"}]},{"path":"b.txt","deleted":true,"revision":2}]}`)
	if err := os.WriteFile(journal, body, 0o644); err != nil {
		t.Fatal(err)
	}
	live, verified, blocks, bytes, hash, ok := JournalIndex(configPath, "docs")
	if !ok || live != 1 || verified != 1 || blocks != 1 || bytes != 4 || hash == "" {
		t.Fatalf("index live=%d verified=%d blocks=%d bytes=%d hash=%q ok=%v", live, verified, blocks, bytes, hash, ok)
	}
}

func TestReconcileSkipsPausedFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{Folders: []config.FolderConfig{{
		ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendReceive,
	}}}
	Reconcile(cfg, configPath, func(string) bool { return true })
	if _, err := os.Stat(JournalPath(configPath, "shared")); !os.IsNotExist(err) {
		t.Fatalf("paused folder wrote a journal: %v", err)
	}
}
