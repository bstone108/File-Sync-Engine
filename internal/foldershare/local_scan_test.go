package foldershare

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/config"
)

func TestLocalIndexAdvancesWhenNoPeerIsReachable(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		NodeName: "kiki",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendReceive,
		}},
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	Reconcile(cfg, configPath, nil)
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("local-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	RequestFullScan("shared")
	Reconcile(cfg, configPath, nil)
	entries, err := LoadJournal(configPath, "shared")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Path == "new.txt" && !entry.Deleted && string(entry.SHA256) != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("local file was not indexed without a peer: %+v", entries)
	}
}

func TestLocalIndexAdvancesWhilePeerDialIsStuck(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() { _, _ = conn.Read(make([]byte, 1)) }()
		}
	}()
	previous := peerSyncAsync
	peerSyncAsync = true
	t.Cleanup(func() { peerSyncAsync = previous })

	root := t.TempDir()
	cfg := localConfig(root, "https://"+listener.Addr().String(), "pin")
	cfg.Peers[0].APIKey = "secret"
	configPath := filepath.Join(t.TempDir(), "config.json")
	Reconcile(cfg, configPath, nil)
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "arrived.txt"), []byte("while-dialing"), 0o644); err != nil {
		t.Fatal(err)
	}
	RequestFullScan("shared")
	done := make(chan struct{})
	go func() {
		Reconcile(cfg, configPath, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("local scan waited for the stuck peer dial")
	}
	entries, err := LoadJournal(configPath, "shared")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Path == "arrived.txt" && !entry.Deleted && entry.SHA256 != "" {
			return
		}
	}
	t.Fatalf("file added while the peer was unreachable was not indexed: %+v", entries)
}
