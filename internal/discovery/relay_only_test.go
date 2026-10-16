package discovery

import (
	"context"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"

	"filesyncengine/internal/nat"
	ma "github.com/multiformats/go-multiaddr"
)

type allowEveryone struct{}

func (allowEveryone) AllowReserve(peer.ID, ma.Multiaddr) bool { return true }
func (allowEveryone) AllowConnect(peer.ID, ma.Multiaddr, peer.ID) bool {
	return true
}

func TestRelayOnlyConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	relayHost := newTestHost(t,
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(relayv2.WithACL(allowEveryone{})),
	)
	left := newTestHost(t, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"), libp2p.ForceReachabilityPrivate(), libp2p.EnableRelay())
	right := newTestHost(t, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"), libp2p.ForceReachabilityPrivate(), libp2p.EnableRelay())
	relayInfo := peer.AddrInfo{ID: relayHost.ID(), Addrs: relayHost.Addrs()}
	if err := left.Connect(ctx, relayInfo); err != nil {
		t.Fatal(err)
	}
	if err := right.Connect(ctx, relayInfo); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reserve(ctx, right, relayInfo); err != nil {
		t.Fatal(err)
	}
	relayAddr := ""
	for _, addr := range relayHost.Addrs() {
		if addr != nil && !strings.Contains(addr.String(), "quic") && !strings.Contains(addr.String(), "webtransport") {
			relayAddr = addr.String()
			break
		}
	}
	if relayAddr == "" {
		t.Fatal("relay has no TCP address")
	}
	circuit, err := ma.NewMultiaddr(relayAddr + "/p2p/" + relayHost.ID().String() + "/p2p-circuit")
	if err != nil {
		t.Fatal(err)
	}
	if nat.Classify(circuit.String()) != nat.PathRelay {
		t.Fatalf("circuit %s classified as %s", circuit, nat.Classify(circuit.String()))
	}
	if err := left.Connect(ctx, peer.AddrInfo{ID: right.ID(), Addrs: []ma.Multiaddr{circuit}}); err != nil {
		t.Fatal(err)
	}
	conns := left.Network().ConnsToPeer(right.ID())
	if len(conns) == 0 {
		t.Fatal("relay connection was not established")
	}
	foundRelay := false
	for _, conn := range conns {
		if nat.Classify(conn.RemoteMultiaddr().String()) == nat.PathRelay {
			foundRelay = true
		}
	}
	if !foundRelay {
		t.Fatalf("active connection was not a relay: %+v", conns)
	}
}

func newTestHost(t *testing.T, opts ...libp2p.Option) host.Host {
	t.Helper()
	host, err := libp2p.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host
}
