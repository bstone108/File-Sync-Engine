package discovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestAcceptedBootstrapRequiresAConnectedPeer(t *testing.T) {
	if err := acceptedBootstrap(nil, 2, []string{"/ip4/127.0.0.1/tcp/1"}); err != nil {
		t.Fatal(err)
	}
	err := acceptedBootstrap(nil, 0, []string{"/ip4/172.30.29.1/tcp/22423"})
	if err == nil || !strings.Contains(err.Error(), "172.30.29.1") {
		t.Fatalf("err = %v", err)
	}
	if err := acceptedBootstrap(context.DeadlineExceeded, 0, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestSelectLibp2pDHTModeStaysAutoBehindNAT(t *testing.T) {
	mode, reason, err := selectLibp2pDHTMode("", true, false)
	if err != nil || mode != dht.ModeAuto || reason != "auto" {
		t.Fatalf("published port: mode %v reason %s err %v", mode, reason, err)
	}
	mode, reason, err = selectLibp2pDHTMode("", false, true)
	if err != nil || mode != dht.ModeAuto || reason != "auto" {
		t.Fatalf("advertise address: mode %v reason %s err %v", mode, reason, err)
	}
	mode, reason, err = selectLibp2pDHTMode("", false, false)
	if err != nil || mode != dht.ModeAuto || reason != "auto" {
		t.Fatalf("default auto: mode %v reason %s err %v", mode, reason, err)
	}
	mode, reason, err = selectLibp2pDHTMode("auto", true, true)
	if err != nil || mode != dht.ModeAuto || reason != "env" {
		t.Fatalf("explicit auto: mode %v reason %s err %v", mode, reason, err)
	}
}

func TestPublishedPortStaysAutoAndCanBootstrap(t *testing.T) {
	t.Setenv("FSE_LIBP2P_DHT_MODE", "")
	t.Setenv(EnvPublishedLibp2pPort, "22421")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	left, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "left"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = left.Close() })
	right, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "right", DHTMode: "server"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = right.Close() })
	if left.modeReason != "auto" || left.dht.Mode() == dht.ModeServer {
		t.Fatalf("left mode %v reason %s", left.dht.Mode(), left.modeReason)
	}
	dial := loopbackDial(right)
	if dial == "" {
		t.Fatalf("no loopback address in %v", right.host.Addrs())
	}
	if err := left.Bootstrap(ctx, []string{dial}); err != nil {
		t.Fatal(err)
	}
	if left.routingTableSize() == 0 {
		t.Fatal("published-port server bootstrap left the routing table empty")
	}
}

func TestClientLookupDoesNotRequireBeingAServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	publisher, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "publisher", DHTMode: "server"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.Close() })
	friend, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "friend", DHTMode: "server"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = friend.Close() })
	client, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "client", DHTMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if client.dht.Mode() != dht.ModeAuto || servesKad(client) {
		t.Fatal("redeemer started as a DHT server")
	}
	publisherDial := loopbackDial(publisher)
	friendDial := loopbackDial(friend)
	if publisherDial == "" || friendDial == "" {
		t.Fatal("missing loopback address")
	}
	publisher.SetBootstrapPeers([]string{friendDial})
	namespace := "filesyncengine/pair/v1/client-lookup"
	payload := []byte("sealed-record")
	if err := publisher.PublishPairing(ctx, namespace, payload); err != nil {
		t.Fatal(err)
	}
	client.SetBootstrapPeers([]string{publisherDial})
	got, err := client.LookupPairing(ctx, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0]) != string(payload) {
		t.Fatalf("lookup = %q err %v", got, err)
	}
	if servesKad(client) {
		t.Fatal("lookup promoted the redeemer to a DHT server")
	}
}

func TestPrivateClientsPairOverTheConnectedPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	left, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "pair-left", DHTMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = left.Close() })
	right, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "pair-right", DHTMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = right.Close() })
	leftDial := loopbackDial(left)
	rightDial := loopbackDial(right)
	if leftDial == "" || rightDial == "" {
		t.Fatal("missing loopback address")
	}
	left.SetBootstrapPeers([]string{rightDial})
	right.SetBootstrapPeers([]string{leftDial})
	namespace := "filesyncengine/pair/v1/private-clients"
	payload := []byte("sealed-record")
	if err := left.PublishPairing(ctx, namespace, payload); err != nil {
		t.Fatal(err)
	}
	if servesKad(left) || servesKad(right) {
		t.Fatal("a private peer connection promoted a DHT server")
	}
	got, err := right.LookupPairing(ctx, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0]) != string(payload) {
		t.Fatalf("lookup = %q err %v", got, err)
	}
	if servesKad(left) || servesKad(right) {
		t.Fatal("lookup promoted a private node")
	}
}

func TestReachableAddressPromotesAutoModeToServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "auto", DHTMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	if servesKad(router) {
		t.Fatal("auto mode started as a server")
	}
	router.notePublicReachability("autonat-v2")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !servesKad(router) {
		time.Sleep(20 * time.Millisecond)
	}
	if !servesKad(router) {
		t.Fatal("reachable address did not start DHT server queries")
	}
}

func loopbackDial(router *Libp2pDHTRouter) string {
	for _, addr := range router.host.Addrs() {
		raw := addr.String()
		if strings.Contains(raw, "/ip4/127.0.0.1/tcp/") && !strings.Contains(raw, "/ws") {
			return raw + "/p2p/" + router.PeerID()
		}
	}
	return ""
}

func servesKad(router *Libp2pDHTRouter) bool {
	for _, id := range router.host.Mux().Protocols() {
		if id == dht.ProtocolDHT {
			return true
		}
	}
	return false
}

func TestAcceptPartialAdvertiseRequiresPeersAndALiveParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := acceptPartialAdvertise(context.DeadlineExceeded, 3, parent); err != nil {
		t.Fatal(err)
	}
	if err := acceptPartialAdvertise(nil, 0, parent); err != nil {
		t.Fatal(err)
	}
	if err := acceptPartialAdvertise(context.DeadlineExceeded, 0, parent); err == nil {
		t.Fatal("empty routing table was accepted")
	}
	cancel()
	if err := acceptPartialAdvertise(context.DeadlineExceeded, 4, parent); err == nil {
		t.Fatal("cancelled parent was accepted")
	}
	if err := acceptPartialAdvertise(fmt.Errorf("failed to find any peer in table"), 4, context.Background()); err == nil {
		t.Fatal("lookup failure was accepted")
	}
}

func TestPairingStreamReturnsStoredRecordUntilUnpublished(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	publisher, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "publisher"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.Close() })
	looker, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "looker"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = looker.Close() })
	namespace := "filesyncengine/pair/v1/stream-test"
	payload := []byte(`{"discoveryId":"publisher-key","certificateSha256":"` + "ab" + `"}`)
	if err := publisher.storePairing(ctx, namespace, payload); err != nil {
		t.Fatal(err)
	}
	info := peer.AddrInfo{ID: publisher.host.ID(), Addrs: publisher.host.Addrs()}
	got, err := looker.FetchPairing(ctx, info, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %s", got)
	}
	if err := publisher.UnpublishPairing(ctx, namespace); err != nil {
		t.Fatal(err)
	}
	got, err = looker.FetchPairing(ctx, info, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unpublished payload = %s", got)
	}
}
