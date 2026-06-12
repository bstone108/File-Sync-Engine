package discovery

import (
	"context"
	"reflect"
	"testing"
)

type recordingDHTRouter struct {
	bootstrapPeers []string
	bootstrapCalls int
	query          string
	peers          []Peer
}

func (r *recordingDHTRouter) Bootstrap(ctx context.Context, peers []string) error {
	r.bootstrapCalls++
	r.bootstrapPeers = append([]string(nil), peers...)
	return nil
}

func (r *recordingDHTRouter) FindPeers(ctx context.Context, namespace string) ([]Peer, error) {
	r.query = namespace
	return r.peers, nil
}

func TestPublicDHTSourceBootstrapsWithDefaultPublicServers(t *testing.T) {
	router := &recordingDHTRouter{peers: []Peer{{ID: "peer-b", Addresses: []string{"/ip4/203.0.113.10/tcp/22000/p2p/peer-b"}}}}
	source := NewPublicDHTSource(router, PublicDHTOptions{Namespace: "fse-test", SelfID: "peer-a"})

	peers, err := source.PeersContext(context.Background())
	if err != nil {
		t.Fatalf("PeersContext: %v", err)
	}
	if router.bootstrapCalls != 1 {
		t.Fatalf("bootstrap calls = %d, want 1", router.bootstrapCalls)
	}
	if len(router.bootstrapPeers) == 0 {
		t.Fatalf("expected default public DHT bootstrap peers")
	}
	for _, address := range router.bootstrapPeers {
		if address == "" || address[0] != '/' {
			t.Fatalf("bootstrap address %q is not a multiaddr-looking public server", address)
		}
	}
	if router.query != "fse-test" {
		t.Fatalf("query namespace = %q, want fse-test", router.query)
	}
	if !reflect.DeepEqual(peers, []Peer{{ID: "peer-b", Addresses: []string{"/ip4/203.0.113.10/tcp/22000/p2p/peer-b"}}}) {
		t.Fatalf("unexpected peers: %+v", peers)
	}
}

func TestPublicDHTSourceDeduplicatesAndIgnoresSelf(t *testing.T) {
	router := &recordingDHTRouter{peers: []Peer{
		{ID: "peer-a", Addresses: []string{"/ip4/127.0.0.1/tcp/22000/p2p/peer-a"}},
		{ID: "peer-b", Addresses: []string{"/ip4/203.0.113.10/tcp/22000/p2p/peer-b"}},
		{ID: "peer-b", Addresses: []string{"/ip4/203.0.113.10/tcp/22000/p2p/peer-b", "/ip4/203.0.113.11/tcp/22000/p2p/peer-b"}},
	}}
	source := NewPublicDHTSource(router, PublicDHTOptions{
		Namespace:      "fse-test",
		SelfID:         "peer-a",
		BootstrapPeers: []string{"/dnsaddr/bootstrap.libp2p.io"},
	})

	peers, err := source.PeersContext(context.Background())
	if err != nil {
		t.Fatalf("PeersContext: %v", err)
	}
	want := []Peer{{ID: "peer-b", Addresses: []string{"/ip4/203.0.113.10/tcp/22000/p2p/peer-b", "/ip4/203.0.113.11/tcp/22000/p2p/peer-b"}}}
	if !reflect.DeepEqual(peers, want) {
		t.Fatalf("peers = %+v, want %+v", peers, want)
	}
	if !reflect.DeepEqual(router.bootstrapPeers, []string{"/dnsaddr/bootstrap.libp2p.io"}) {
		t.Fatalf("bootstrap peers = %+v", router.bootstrapPeers)
	}
}

func TestPublicDHTSourceRejectsMissingNamespace(t *testing.T) {
	source := NewPublicDHTSource(&recordingDHTRouter{}, PublicDHTOptions{SelfID: "peer-a"})
	if _, err := source.PeersContext(context.Background()); err == nil {
		t.Fatalf("expected missing namespace to be rejected")
	}
}
