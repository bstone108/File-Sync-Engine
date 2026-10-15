package discovery

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

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
	if err := publisher.storePairing(namespace, payload); err != nil {
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
