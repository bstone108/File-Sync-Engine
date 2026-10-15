package discovery

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	testPeerA = "QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"
	testPeerB = "QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ"
)

type mapResolver map[string][]string

func (m mapResolver) Resolve(_ context.Context, addr ma.Multiaddr) ([]ma.Multiaddr, error) {
	raw, ok := m[addr.String()]
	if !ok {
		return nil, fmt.Errorf("no fixture for %s", addr)
	}
	out := make([]ma.Multiaddr, 0, len(raw))
	for _, item := range raw {
		parsed, err := ma.NewMultiaddr(item)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

type failResolver struct{}

func (failResolver) Resolve(context.Context, ma.Multiaddr) ([]ma.Multiaddr, error) {
	return nil, fmt.Errorf("resolver should not be called")
}

func TestParseDefaultDNSAddrBootstrapResolvesToPeerID(t *testing.T) {
	nested := "/dnsaddr/ams-1.bootstrap.libp2p.io/p2p/" + testPeerA
	// A nested dnsaddr that already names a peer must not be resolved again.
	resolver := mapResolver{
		mustAddr(t, DefaultPublicDHTBootstrapPeers[0]).String(): {nested},
	}
	infos, err := parsePeerAddrInfosWith(context.Background(), DefaultPublicDHTBootstrapPeers, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].ID.String() != testPeerA {
		t.Fatalf("infos = %+v", infos)
	}
	if len(infos[0].Addrs) != 1 || infos[0].Addrs[0].String() != DefaultPublicDHTBootstrapPeers[0] {
		t.Fatalf("addrs = %v", infos[0].Addrs)
	}
}

func TestParseDirectPeerMultiaddrSkipsDNS(t *testing.T) {
	address := "/ip4/192.0.2.11/tcp/4001/p2p/" + testPeerB
	infos, err := parsePeerAddrInfosWith(context.Background(), []string{address}, failResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].ID.String() != testPeerB {
		t.Fatalf("infos = %+v", infos)
	}
}

func TestParsePeerAddrWithoutIDStillFails(t *testing.T) {
	_, err := parsePeerAddrInfosWith(context.Background(), []string{"/ip4/192.0.2.11/tcp/4001"}, failResolver{})
	if err == nil || !strings.Contains(err.Error(), "parse DHT bootstrap peer") || !strings.Contains(err.Error(), "invalid p2p multiaddr") {
		t.Fatalf("err = %v", err)
	}
}

func TestBootstrapPeerListUsesDefaultWhenUnset(t *testing.T) {
	router := &Libp2pDHTRouter{}
	if !reflect.DeepEqual(router.bootstrapPeerList(), DefaultPublicDHTBootstrapPeers) {
		t.Fatalf("peers = %v", router.bootstrapPeerList())
	}
	custom := []string{"/ip4/192.0.2.11/tcp/4001/p2p/" + testPeerB}
	router.SetBootstrapPeers(custom)
	if !reflect.DeepEqual(router.bootstrapPeerList(), custom) {
		t.Fatalf("custom peers = %v", router.bootstrapPeerList())
	}
	router.SetBootstrapPeers(nil)
	if !reflect.DeepEqual(router.bootstrapPeerList(), DefaultPublicDHTBootstrapPeers) {
		t.Fatalf("reset peers = %v", router.bootstrapPeerList())
	}
}

func TestParseDNSAddrKeepsDialHostnames(t *testing.T) {
	tcp := "/dns4/chic-1.bootstrap.ipni.io/tcp/4001/p2p/" + testPeerA
	wss := "/dns4/chic-1.bootstrap.ipni.io/tcp/4443/wss/p2p/" + testPeerA
	resolver := mapResolver{
		mustAddr(t, DefaultPublicDHTBootstrapPeers[0]).String(): {tcp, wss},
	}
	infos, err := parsePeerAddrInfosWith(context.Background(), DefaultPublicDHTBootstrapPeers, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].ID.String() != testPeerA {
		t.Fatalf("infos = %+v", infos)
	}
	if len(infos[0].Addrs) != 1 || infos[0].Addrs[0].String() != DefaultPublicDHTBootstrapPeers[0] {
		t.Fatalf("addrs = %v", infos[0].Addrs)
	}
}

func TestParseDNS4PeerSkipsResolver(t *testing.T) {
	address := "/dns4/bootstrap.example/tcp/4001/p2p/" + testPeerA
	infos, err := parsePeerAddrInfosWith(context.Background(), []string{address}, failResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].ID.String() != testPeerA {
		t.Fatalf("infos = %+v", infos)
	}
	if len(infos[0].Addrs) != 1 || infos[0].Addrs[0].String() != "/dns4/bootstrap.example/tcp/4001" {
		t.Fatalf("addrs = %v", infos[0].Addrs)
	}
}

func TestBootstrapAcceptsDefaultDNSAddrBootstrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	resolved := "/ip4/127.0.0.1/tcp/1/p2p/" + testPeerA
	router.SetResolver(mapResolver{
		mustAddr(t, DefaultPublicDHTBootstrapPeers[0]).String(): {resolved},
	})
	if err := router.Bootstrap(ctx, DefaultPublicDHTBootstrapPeers); err != nil {
		t.Fatal(err)
	}
}

func TestSingleTransportDialsSkipsQUIC(t *testing.T) {
	id, err := peer.Decode(testPeerA)
	if err != nil {
		t.Fatal(err)
	}
	dns := mustAddr(t, "/dnsaddr/ams-1.bootstrap.libp2p.io")
	quic := mustAddr(t, "/ip4/192.0.2.10/udp/4001/quic-v1")
	tcp := mustAddr(t, "/ip4/192.0.2.10/tcp/4001")
	got := singleTransportDials([]peer.AddrInfo{{ID: id, Addrs: []ma.Multiaddr{quic, dns, tcp}}})
	if len(got) != 2 || len(got[0].Addrs) != 1 || got[0].Addrs[0].String() != dns.String() {
		t.Fatalf("dials = %+v", got)
	}
	if len(got[1].Addrs) != 1 || got[1].Addrs[0].String() != tcp.String() {
		t.Fatalf("second = %v", got[1].Addrs)
	}
}

func TestPreferDialableTransportsPutsPlainTCPFirst(t *testing.T) {
	quic := mustAddr(t, "/ip4/192.0.2.10/udp/4001/quic-v1")
	ws := mustAddr(t, "/ip4/192.0.2.10/tcp/443/wss")
	tcp := mustAddr(t, "/ip4/192.0.2.10/tcp/4001")
	id, err := peer.Decode(testPeerA)
	if err != nil {
		t.Fatal(err)
	}
	got := preferDialableTransports([]peer.AddrInfo{{ID: id, Addrs: []ma.Multiaddr{quic, ws, tcp}}})
	if len(got) != 1 || len(got[0].Addrs) != 3 || got[0].Addrs[0].String() != tcp.String() {
		t.Fatalf("order = %v", got[0].Addrs)
	}
}

func TestObservedIPReadsFirstIPComponent(t *testing.T) {
	if got := observedIP(mustAddr(t, "/ip4/203.0.113.8/tcp/4001")); got != "203.0.113.8" {
		t.Fatalf("ipv4 = %q", got)
	}
	if got := observedIP(mustAddr(t, "/ip6/2001:db8::1/tcp/4001")); got != "2001:db8::1" {
		t.Fatalf("ipv6 = %q", got)
	}
	if got := observedIP(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
}

func mustAddr(t *testing.T, raw string) ma.Multiaddr {
	t.Helper()
	addr, err := ma.NewMultiaddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}
