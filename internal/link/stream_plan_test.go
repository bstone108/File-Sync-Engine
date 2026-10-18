package link

import (
	"fmt"
	"strings"
	"testing"

	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

func TestSignedHostnameIsDialedBeforeRelayCircuit(t *testing.T) {
	const (
		peerID  = "12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU"
		relayID = "12D3KooWPjceQrSwdWXPyLLeABRXmuqt69Rg3sBYbU1Nft9HyQ6X"
	)
	peer := config.PeerConfig{
		DHTPeerID: peerID,
		Relays:    []string{"/ip4/10.200.0.1/tcp/4001/p2p/" + relayID + "/p2p-circuit"},
		Endpoints: []config.EndpointConfig{
			{Kind: "manual", Address: "https://fse-a:22420", Authenticated: true},
			{Kind: "manual", Address: "http://fse-a:22420", Authenticated: true},
			{Kind: "manual", Address: "https://203.0.113.9:22420", Authenticated: true},
			{Kind: "manual", Address: "https://stranger.example:22420", Authenticated: false},
		},
	}
	direct, circuits := streamDialPlan(peer)
	if len(circuits) != 1 || !strings.Contains(circuits[0], "p2p-circuit") || !strings.HasSuffix(circuits[0], "/p2p/"+peerID) {
		t.Fatalf("circuits = %v", circuits)
	}
	port := discovery.Libp2pListenPort()
	wantTCP := fmt.Sprintf("/dns4/fse-a/tcp/%d/p2p/%s", port, peerID)
	wantQUIC := fmt.Sprintf("/dns4/fse-a/udp/%d/quic-v1/p2p/%s", port, peerID)
	if !containsAddr(direct, wantTCP) || !containsAddr(direct, wantQUIC) {
		t.Fatalf("direct = %v, want %s and %s", direct, wantTCP, wantQUIC)
	}
	for _, addr := range []string{wantTCP, wantQUIC} {
		if _, err := ma.NewMultiaddr(addr); err != nil {
			t.Fatalf("direct address is not a multiaddr: %s: %v", addr, err)
		}
	}
	for _, addr := range direct {
		if strings.Contains(addr, "p2p-circuit") || strings.Contains(addr, "203.0.113.9") || strings.Contains(addr, "stranger.example") || strings.Contains(addr, "http://") {
			t.Fatalf("direct candidate is not the signed hostname stream: %s", addr)
		}
	}
}

func containsAddr(addrs []string, want string) bool {
	for _, addr := range addrs {
		if addr == want {
			return true
		}
	}
	return false
}
