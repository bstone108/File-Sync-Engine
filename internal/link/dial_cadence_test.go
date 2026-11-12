package link

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/nat"
)

func TestOfflinePeerDialIsPromptThenSixtySeconds(t *testing.T) {
	forceDialGate = true
	resetPeerDialSchedule()
	t.Cleanup(func() {
		forceDialGate = false
		resetPeerDialSchedule()
	})
	if !allowPeerDial("kiki", nat.DefaultPaths) {
		t.Fatal("the first offline dial was deferred")
	}
	if !allowPeerDial("kiki", nat.DefaultPaths) {
		t.Fatal("the block read in the same sync pass was deferred")
	}
	peerDialMu.Lock()
	peerDialBurst["kiki"] = time.Now().Add(-time.Second)
	peerDialMu.Unlock()
	if allowPeerDial("kiki", nat.DefaultPaths) {
		t.Fatal("an offline peer was redialed before 60 seconds")
	}
	peerDialMu.Lock()
	peerDialNext["kiki"] = time.Now().Add(-time.Second)
	peerDialMu.Unlock()
	if !allowPeerDial("kiki", nat.DefaultPaths) {
		t.Fatal("the offline peer was not redialed at the 60 second interval")
	}
	if discovery.OfflineProbeInterval != 60*time.Second {
		t.Fatalf("offline interval = %s", discovery.OfflineProbeInterval)
	}
}

func TestForeignPrivateAddressIsNotDialed(t *testing.T) {
	const id = "12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU"
	if KeepPairedAddress("/ip4/192.168.1.101/tcp/22420/https/p2p/"+id, id) {
		t.Fatal("a foreign private multiaddr was dialable")
	}
	if !KeepPairedAddress("/ip4/192.168.1.101/tcp/22421/p2p/12D3KooWPjceQrSwdWXPyLLeABRXmuqt69Rg3sBYbU1Nft9HyQ6X/p2p-circuit/p2p/"+id, id) {
		t.Fatal("a relay circuit through a private hop was dropped")
	}
	if !KeepPairedAddress("https://127.0.0.1:22420", id) {
		t.Fatal("loopback was dropped")
	}
}

func TestDHTPeerWithNoAddressIsDialableThroughAStaticRelay(t *testing.T) {
	const relay = "12D3KooWPjceQrSwdWXPyLLeABRXmuqt69Rg3sBYbU1Nft9HyQ6X"
	const target = "12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU"
	t.Setenv("FSE_STATIC_RELAYS", "/ip4/172.30.72.10/tcp/22421/p2p/"+relay)
	peer := config.PeerConfig{ID: "neko", DHTPeerID: target}
	if !HasAPIEndpoint(peer) {
		t.Fatal("a DHT peer with a reserved relay was not dialable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := dialLibp2p(ctx, peer, func(context.Context, string, *http.Client) error { return nil })
	if err == nil || strings.Contains(err.Error(), "no libp2p address") {
		t.Fatalf("dial = %v, want the reserved circuit to be attempted", err)
	}
}
