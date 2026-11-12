package discovery

import (
	"strings"
	"testing"
)

func TestReservedRelaysStayBoundedAndBuildACircuit(t *testing.T) {
	mesh.mu.Lock()
	prev := append([]string(nil), mesh.reservedRelays...)
	mesh.reservedRelays = nil
	mesh.mu.Unlock()
	t.Cleanup(func() {
		mesh.mu.Lock()
		mesh.reservedRelays = prev
		mesh.mu.Unlock()
	})
	var addrs []string
	for i := 0; i < 20; i++ {
		addrs = append(addrs, "/ip4/198.51.100."+itoa(i)+"/tcp/4001/p2p/12D3KooWRelay"+itoa(i))
	}
	NoteReservedRelays(addrs)
	mesh.mu.Lock()
	got := len(mesh.reservedRelays)
	mesh.mu.Unlock()
	if got != maxReservedRelays {
		t.Fatalf("reserved = %d, want %d", got, maxReservedRelays)
	}
	circuits := ReservedRelayCircuits("12D3KooWPeer")
	if len(circuits) == 0 || !containsAll(circuits, "/p2p-circuit/p2p/12D3KooWPeer") {
		t.Fatalf("circuits = %v", circuits)
	}
}

func TestDiscoveredRelayIsSharedAndBootstrapIsNot(t *testing.T) {
	mesh.mu.Lock()
	prevDiscovered := append([]string(nil), mesh.discoveredRelays...)
	prevBootstrap := mesh.bootstrapPeers
	mesh.discoveredRelays = nil
	mesh.bootstrapPeers = nil
	mesh.mu.Unlock()
	t.Cleanup(func() {
		mesh.mu.Lock()
		mesh.discoveredRelays = prevDiscovered
		mesh.bootstrapPeers = prevBootstrap
		mesh.mu.Unlock()
	})
	NoteBootstrapPeer("12D3KooWBootstrapPeerBootstrapPeerBootstrap")
	NoteDiscoveredRelay("/ip4/104.131.131.82/tcp/4001/p2p/12D3KooWBootstrapPeerBootstrapPeerBootstrap")
	NoteDiscoveredRelay("/ip4/172.30.72.10/tcp/22421/p2p/12D3KooWRelayPeerRelayPeerRelayPeerRelayPe")
	circuits := ReservedRelayCircuits("12D3KooWIdentityPeerIdentityPeerIdentityPee")
	joined := strings.Join(circuits, " ")
	if strings.Contains(joined, "104.131.131.82") || strings.Contains(joined, "Bootstrap") {
		t.Fatalf("bootstrap was dialed as a relay: %s", joined)
	}
	if !strings.Contains(joined, "172.30.72.10") || !strings.Contains(joined, "p2p-circuit/p2p/12D3KooWIdentityPeerIdentityPeerIdentityPee") {
		t.Fatalf("discovered relay circuit = %s", joined)
	}
	if len(OrderedRelays()) == 0 {
		t.Fatal("ordered relays dropped the discovered relay")
	}
	for _, addr := range OrderedRelays() {
		if strings.Contains(addr, "bootstrap.libp2p.io") {
			t.Fatalf("bootstrap list is a relay candidate: %s", addr)
		}
	}
}

func containsAll(addrs []string, part string) bool {
	for _, addr := range addrs {
		if len(addr) >= len(part) && addr[len(addr)-len(part):] == part {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [4]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
