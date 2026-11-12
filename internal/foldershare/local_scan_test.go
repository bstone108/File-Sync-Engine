package foldershare

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
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
	root := t.TempDir()
	cfg := localConfig(root, "https://"+listener.Addr().String(), "pin")
	cfg.Peers[0].APIKey = "secret"
	configPath := filepath.Join(t.TempDir(), "config.json")
	dialPath := filepath.Join(t.TempDir(), "dial.json")
	go func() {
		_, _, _ = fetchRemoteJournal(context.Background(), dialPath, cfg.Peers[0], "shared")
	}()
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "arrived.txt"), []byte("while-dialing"), 0o644); err != nil {
		t.Fatal(err)
	}
	RequestFullScan("shared")
	done := make(chan struct{})
	go func() {
		_ = reconcileFolder(cfg, configPath, cfg.Folders[0])
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

func TestPeerPassesStayOnTheClockWhenADialIsSlow(t *testing.T) {
	previousFn := passFn
	previousOffline := passOffline
	previousDeadline := passDeadline
	previousAsync := peerSyncAsync
	var mu sync.Mutex
	var starts []time.Time
	passFn = func(ctx context.Context, _ config.Config, _ string, _ config.FolderConfig) bool {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		<-ctx.Done()
		return false
	}
	passOffline = 200 * time.Millisecond
	passDeadline = 40 * time.Millisecond
	peerSyncAsync = true
	scheduleOnce = sync.Once{}
	scheduleOn.Store(false)
	t.Cleanup(func() {
		passFn = previousFn
		passOffline = previousOffline
		passDeadline = previousDeadline
		peerSyncAsync = previousAsync
		scheduleOn.Store(false)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	folder := t.TempDir()
	StartSchedulers(ctx, t.TempDir(), func() config.Config {
		return config.Config{Folders: []config.FolderConfig{{
			ID: "shared", Path: folder, Enabled: true, Mode: config.ModeSendReceive,
			SharedWith: []string{"neko"},
		}}, Peers: []config.PeerConfig{{ID: "neko", APIKey: "secret"}}}
	})
	time.Sleep(520 * time.Millisecond)
	cancel()
	mu.Lock()
	defer mu.Unlock()
	if len(starts) < 3 {
		t.Fatalf("passes = %d, want at least 3: %v", len(starts), starts)
	}
	if starts[1].Sub(starts[0]) > 350*time.Millisecond || starts[2].Sub(starts[1]) > 350*time.Millisecond {
		t.Fatalf("pass starts slipped: %v", starts)
	}
}
