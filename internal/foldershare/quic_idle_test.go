package foldershare

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

// TestDirectQUICSyncSurvivesIdle exchanges an index and a file over a direct
// QUIC connection, then keeps that connection up with a 15s ping. The old
// 30s QUIC idle timeout used to drop the session before any bytes moved.
func TestDirectQUICSyncSurvivesIdle(t *testing.T) {
	if testing.Short() {
		t.Skip("idle sync")
	}
	discovery.SetAPIHandler(nil)
	discovery.SetAPIHost(nil)
	t.Cleanup(func() {
		discovery.SetAPIHandler(nil)
		discovery.SetAPIHost(nil)
	})

	sender := newQUICHost(t)
	receiver := newQUICHost(t)
	addr := quicDirectAddr(t, sender)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := receiver.Connect(ctx, peer.AddrInfo{ID: sender.ID(), Addrs: sender.Addrs()}); err != nil {
		t.Fatal(err)
	}

	senderRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(senderRoot, "note.txt"), []byte("quic-direct"), 0o644); err != nil {
		t.Fatal(err)
	}
	senderConfig := filepath.Join(t.TempDir(), "sender.json")
	Reconcile(config.Config{NodeName: "kiki", Folders: []config.FolderConfig{{
		ID: "shared", Path: senderRoot, Enabled: true, Mode: config.ModeSendOnly,
	}}}, senderConfig, nil)

	discovery.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync-journal":
			http.ServeFile(w, r, JournalPath(senderConfig, "shared"))
		case "/v1/folder-block":
			data, err := os.ReadFile(filepath.Join(senderRoot, r.URL.Query().Get("path")))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	discovery.SetAPIHost(receiver)
	discovery.ServeAPI(sender)

	receiverRoot := t.TempDir()
	receiverConfig := filepath.Join(t.TempDir(), "receiver.json")
	peer := config.PeerConfig{
		ID:        "kiki",
		APIKey:    "secret",
		DHTPeerID: sender.ID().String(),
		Addresses: []string{addr},
	}
	Reconcile(config.Config{
		NodeName: "neko",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: receiverRoot, Enabled: true, Mode: config.ModeSendReceive, SharedWith: []string{"kiki"},
		}},
		Peers: []config.PeerConfig{peer},
	}, receiverConfig, nil)
	got, err := os.ReadFile(filepath.Join(receiverRoot, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "quic-direct" {
		t.Fatalf("synced %q", got)
	}

	pinger := ping.NewPingService(receiver)
	idle := 45 * time.Second
	deadline := time.Now().Add(idle)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
			select {
			case <-pinger.Ping(pingCtx, sender.ID()):
			case <-pingCtx.Done():
				pingCancel()
				t.Fatal("quic keepalive ping failed during idle")
			}
			pingCancel()
		case <-time.After(time.Until(deadline)):
		}
	}
	if receiver.Network().Connectedness(sender.ID()) != network.Connected {
		t.Fatal("direct QUIC connection closed during idle")
	}
	if err := os.WriteFile(filepath.Join(senderRoot, "again.txt"), []byte("after-idle"), 0o644); err != nil {
		t.Fatal(err)
	}
	Reconcile(config.Config{NodeName: "kiki", Folders: []config.FolderConfig{{
		ID: "shared", Path: senderRoot, Enabled: true, Mode: config.ModeSendOnly,
	}}}, senderConfig, nil)
	Reconcile(config.Config{
		NodeName: "neko",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: receiverRoot, Enabled: true, Mode: config.ModeSendReceive, SharedWith: []string{"kiki"},
		}},
		Peers: []config.PeerConfig{peer},
	}, receiverConfig, nil)
	got, err = os.ReadFile(filepath.Join(receiverRoot, "again.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "after-idle" {
		t.Fatalf("after idle synced %q", got)
	}
}

func TestEmptyFolderScanIsUpToDate(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "shared")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	Reconcile(config.Config{NodeName: "kiki", Folders: []config.FolderConfig{{
		ID: "shared", Path: folder, Enabled: true, Mode: config.ModeSendReceive,
	}}}, configPath, nil)
	if !JournalScanned(configPath, "shared") {
		t.Fatal("an empty folder stayed unscanned")
	}
}

func newQUICHost(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
		libp2p.ForceReachabilityPrivate(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func quicDirectAddr(t *testing.T, h host.Host) string {
	t.Helper()
	for _, addr := range h.Addrs() {
		if addr == nil {
			continue
		}
		text := addr.String()
		if strings.Contains(text, "/quic-v1") && !strings.Contains(text, "webtransport") {
			if !strings.Contains(text, "/p2p/") {
				text += "/p2p/" + h.ID().String()
			}
			return text
		}
	}
	t.Fatal("host has no quic address")
	return ""
}
