package discovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestServerModeBootstrapAddsThePeerToTheRoutingTable(t *testing.T) {
	t.Setenv("FSE_LIBP2P_DHT_MODE", "server")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	left, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "left"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = left.Close() })
	right, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "right"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = right.Close() })
	var dial string
	for _, addr := range right.host.Addrs() {
		raw := addr.String()
		if strings.Contains(raw, "/ip4/127.0.0.1/tcp/") && !strings.Contains(raw, "/ws") {
			dial = raw + "/p2p/" + right.PeerID()
			break
		}
	}
	if dial == "" {
		t.Fatalf("no loopback address in %v", right.host.Addrs())
	}
	if err := left.Bootstrap(ctx, []string{dial}); err != nil {
		t.Fatal(err)
	}
	if left.routingTableSize() == 0 {
		t.Fatal("server bootstrap left the routing table empty")
	}
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
