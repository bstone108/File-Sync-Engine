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
