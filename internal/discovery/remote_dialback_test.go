package discovery

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2"
	ma "github.com/multiformats/go-multiaddr"
)

func TestPublicLibp2pProbeAddrsPreferAnnouncedTCP(t *testing.T) {
	addrs := publicLibp2pProbeAddrs([]ma.Multiaddr{
		ma.StringCast("/ip4/192.168.1.5/tcp/22421"),
		ma.StringCast("/ip4/138.199.21.250/tcp/22421"),
		ma.StringCast("/dns4/published-a/tcp/22420/https"),
		ma.StringCast("/dns4/published-a/udp/22421/quic-v1"),
		ma.StringCast("/dns4/published-a/tcp/22421"),
		ma.StringCast("/dns4/relay.example/tcp/4001/p2p/12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU/p2p-circuit"),
	})
	got := strings.Join(multiaddrStrings(addrs), " ")
	want := "/dns4/published-a/tcp/22421 /ip4/138.199.21.250/tcp/22421"
	if got != want {
		t.Fatalf("probe addrs = %s", got)
	}
}

func TestRemoteAutoNATDialbackPromotesAndBootstraps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	left, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "dialback-left"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = left.Close() })
	right, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "dialback-right"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = right.Close() })
	if servesKad(left) || servesKad(right) {
		t.Fatal("auto mode started as a DHT server")
	}
	leftPort := listenTCPPort(left.host)
	rightPort := listenTCPPort(right.host)
	if leftPort == "" || rightPort == "" {
		t.Fatalf("listen ports %q %q", leftPort, rightPort)
	}
	// 1.2.3.4 is a public address. Nothing is listening on it until the test
	// adds it to the loopback interface, so a local hairpin cannot succeed yet.
	publicLeft := ma.StringCast("/ip4/1.2.3.4/tcp/" + leftPort)
	if left.tryRemoteDialback(ctx, []ma.Multiaddr{publicLeft}) {
		t.Fatal("a dial-back with no remote peer promoted the node")
	}
	if servesKad(left) {
		t.Fatal("a public address promoted the node before any peer dialed it")
	}
	rightInfo, err := peer.AddrInfoFromP2pAddr(ma.StringCast("/ip4/127.0.0.1/tcp/" + rightPort + "/p2p/" + right.PeerID()))
	if err != nil {
		t.Fatal(err)
	}
	if err := left.host.Connect(ctx, *rightInfo); err != nil {
		t.Fatal(err)
	}
	waitForAutoNAT(t, left.host, right.host.ID())
	waitForAutoNAT(t, right.host, left.host.ID())
	if left.tryRemoteDialback(ctx, []ma.Multiaddr{ma.StringCast("/ip4/192.168.1.5/tcp/" + leftPort)}) {
		t.Fatal("a private address was treated as a public dial-back")
	}
	if servesKad(left) || servesKad(right) {
		t.Fatal("connecting on loopback promoted a node")
	}
	addLoopbackIP(t, "1.2.3.4")
	if !left.tryRemoteDialback(ctx, []ma.Multiaddr{publicLeft}) {
		t.Fatal("remote AutoNAT dial-back did not promote the node")
	}
	waitServesKad(t, left, true)
	addLoopbackIP(t, "1.2.3.5")
	publicRight := ma.StringCast("/ip4/1.2.3.5/tcp/" + rightPort)
	if !right.tryRemoteDialback(ctx, []ma.Multiaddr{publicRight}) {
		t.Fatal("remote AutoNAT dial-back did not promote the other node")
	}
	waitServesKad(t, right, true)
	dial := "/ip4/127.0.0.1/tcp/" + rightPort + "/p2p/" + right.PeerID()
	if err := left.Bootstrap(ctx, []string{dial}); err != nil {
		t.Fatal(err)
	}
	if left.routingTableSize() == 0 {
		t.Fatal("routing table stayed empty after the remote dial-back")
	}
}

func addLoopbackIP(t *testing.T, ip string) {
	t.Helper()
	cmd := exec.Command("sudo", "-n", "ip", "addr", "add", ip+"/32", "dev", "lo")
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "File exists") {
		t.Fatalf("ip addr add %s: %v %s", ip, err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("sudo", "-n", "ip", "addr", "del", ip+"/32", "dev", "lo").Run()
	})
}

func listenTCPPort(h host.Host) string {
	for _, addr := range h.Network().ListenAddresses() {
		if addr == nil || !strings.HasPrefix(addr.String(), "/ip4/") {
			continue
		}
		protos := addr.Protocols()
		if len(protos) == 0 || protos[len(protos)-1].Code != ma.P_TCP {
			continue
		}
		port, err := addr.ValueForProtocol(ma.P_TCP)
		if err != nil || port == "" {
			continue
		}
		return port
	}
	return ""
}

func waitServesKad(t *testing.T, router *Libp2pDHTRouter, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if servesKad(router) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("serves kad = %v, want %v", servesKad(router), want)
}

func waitForAutoNAT(t *testing.T, h host.Host, remote peer.ID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		supported, err := h.Peerstore().SupportsProtocols(remote, autonatv2.DialProtocol)
		if err == nil && len(supported) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("peer did not advertise AutoNAT v2")
}
