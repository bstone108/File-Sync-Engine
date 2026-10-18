package foldershare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/nat"
)

type relayAllowAll struct{}

func (relayAllowAll) AllowReserve(peer.ID, ma.Multiaddr) bool          { return true }
func (relayAllowAll) AllowConnect(peer.ID, ma.Multiaddr, peer.ID) bool { return true }

// TestSyncUsesRelayWhenAdvertisedURLIsUnreachable reproduces the live failure
// where both sides stored circuit addresses and then timed out on an
// unforwarded public API URL. The file must arrive on a libp2p stream.
func TestSyncUsesRelayWhenAdvertisedURLIsUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	nat.DefaultPaths.Reset()
	t.Cleanup(nat.DefaultPaths.Reset)
	t.Cleanup(func() {
		discovery.SetAPIHandler(nil)
		discovery.SetAPIHost(nil)
	})

	relayHost := newRelayHost(t)
	sender := newPrivateHost(t)
	receiver := newPrivateHost(t)
	relayInfo := peer.AddrInfo{ID: relayHost.ID(), Addrs: relayHost.Addrs()}
	if err := sender.Connect(ctx, relayInfo); err != nil {
		t.Fatal(err)
	}
	if err := receiver.Connect(ctx, relayInfo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reserve(ctx, sender, relayInfo); err != nil {
		t.Fatal(err)
	}
	relayAddr := tcpMultiaddr(t, relayHost)
	circuit := relayAddr + "/p2p/" + relayHost.ID().String() + "/p2p-circuit"

	senderRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(senderRoot, "note.txt"), []byte("relayed-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	senderConfig := filepath.Join(t.TempDir(), "sender.json")
	Reconcile(config.Config{NodeName: "sender", Folders: []config.FolderConfig{{
		ID: "shared", Path: senderRoot, Enabled: true, Mode: config.ModeSendOnly,
	}}}, senderConfig, nil)

	var advertisedHits int
	advertised := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		advertisedHits++
		http.Error(w, "unforwarded", http.StatusBadGateway)
	}))
	defer advertised.Close()

	discovery.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync-journal":
			http.ServeFile(w, r, JournalPath(senderConfig, "shared"))
		case "/v1/folder-block":
			data, err := os.ReadFile(filepath.Join(senderRoot, r.URL.Query().Get("path")))
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
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
	Reconcile(config.Config{
		NodeName: "receiver",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: receiverRoot, Enabled: true, Mode: config.ModeSendReceive,
		}},
		Peers: []config.PeerConfig{{
			ID:        "sender-peer",
			APIKey:    "secret",
			DHTPeerID: sender.ID().String(),
			Relays:    []string{circuit},
			Endpoints: []config.EndpointConfig{{
				Kind: "manual", Address: advertised.URL, NetworkHint: "wan",
			}},
		}},
	}, receiverConfig, nil)

	got, err := os.ReadFile(filepath.Join(receiverRoot, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "relayed-bytes" {
		t.Fatalf("synced %q", got)
	}
	if advertisedHits != 0 {
		t.Fatalf("advertised URL was dialed %d times", advertisedHits)
	}
	obs, ok := nat.DefaultPaths.Active("sender-peer")
	if !ok || obs.Path != nat.PathRelay {
		t.Fatalf("path = %+v ok=%v", obs, ok)
	}
}

func newRelayHost(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.ForceReachabilityPublic(),
		// Unlimited, like a same-identity helper. A public kubo relay is limited
		// and must not carry this file; that case is covered in discovery.
		libp2p.EnableRelayService(relayv2.WithACL(relayAllowAll{}), relayv2.WithInfiniteLimits()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func newPrivateHost(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.ForceReachabilityPrivate(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func tcpMultiaddr(t *testing.T, h host.Host) string {
	t.Helper()
	for _, addr := range h.Addrs() {
		if addr == nil {
			continue
		}
		text := addr.String()
		if strings.Contains(text, "/tcp/") && !strings.Contains(text, "quic") && !strings.Contains(text, "webtransport") && !strings.Contains(text, "webrtc") {
			return text
		}
	}
	t.Fatal("host has no TCP address")
	return ""
}
