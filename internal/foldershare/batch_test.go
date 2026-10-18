package foldershare

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"filesyncengine/internal/config"
)

func TestSmallFilesShareOneBatchRequest(t *testing.T) {
	remoteRoot := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if err := os.WriteFile(filepath.Join(remoteRoot, name), []byte("file-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	remoteConfig := filepath.Join(t.TempDir(), "remote.json")
	if err := os.WriteFile(remoteConfig, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	remoteCfg := config.Config{NodeName: "remote", Folders: []config.FolderConfig{{
		ID: "shared", Path: remoteRoot, Enabled: true, Mode: config.ModeSendReceive,
	}}}
	Reconcile(remoteCfg, remoteConfig, nil)
	var batches, blocks atomic.Int32
	server, pin := pinnedServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync-journal":
			http.ServeFile(w, r, JournalPath(remoteConfig, "shared"))
		case "/v1/folder-batch":
			batches.Add(1)
			var req batchRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			files := make([]BatchFile, 0, len(req.Files))
			for _, item := range req.Files {
				data, err := os.ReadFile(filepath.Join(remoteRoot, item.Path))
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				files = append(files, BatchFile{Path: item.Path, Data: data})
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			if err := WriteFileBatch(w, files); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		case "/v1/folder-block":
			blocks.Add(1)
			http.Error(w, "small files should use the batch", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))

	localRoot := t.TempDir()
	localConfig := filepath.Join(t.TempDir(), "local.json")
	localCfg := config.Config{
		NodeName: "local",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: localRoot, Enabled: true, Mode: config.ModeReceiveOnly,
		}},
		Peers: []config.PeerConfig{pinnedPeer("remote", "secret", server.URL, pin)},
	}
	Reconcile(localCfg, localConfig, nil)
	if batches.Load() != 1 || blocks.Load() != 0 {
		t.Fatalf("batches=%d blocks=%d", batches.Load(), blocks.Load())
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		got, err := os.ReadFile(filepath.Join(localRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "file-"+name {
			t.Fatalf("%s = %q", name, got)
		}
	}
}
