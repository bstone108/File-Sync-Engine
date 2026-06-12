package discoverycontrol

import (
	"context"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

type runtimeSourceRouter struct {
	peers []discovery.Peer
}

func (r runtimeSourceRouter) Bootstrap(ctx context.Context, peers []string) error { return nil }
func (r runtimeSourceRouter) FindPeers(ctx context.Context, namespace string) ([]discovery.Peer, error) {
	return r.peers, nil
}

func TestBuildRuntimeSourcesInjectsDHTRouterOnlyWhenAutomaticDiscoveryEnabled(t *testing.T) {
	created := 0
	cfg := config.Config{NodeName: "node-a", Discovery: config.DiscoveryConfig{DHT: true}}

	sources := BuildRuntimeSources(cfg, func(config.Config) discovery.DHTRouter {
		created++
		return runtimeSourceRouter{peers: []discovery.Peer{{ID: "dht-peer", Addresses: []string{"/ip4/203.0.113.9/tcp/22000/p2p/dht-peer"}}}}
	})
	peers, events := PollSources(sources)

	if created != 1 {
		t.Fatalf("DHT router factory called %d times, want once", created)
	}
	if len(events) != 0 || len(peers) != 1 || peers[0].ID != "dht-peer" {
		t.Fatalf("DHT source not wired into runtime sources: peers=%+v events=%+v", peers, events)
	}
}

func TestBuildRuntimeSourcesKeepsManualPeersWhenDiscoveryDisabled(t *testing.T) {
	cfg := config.Config{
		NodeName:  "node-a",
		Discovery: config.DiscoveryConfig{Disabled: true, DHT: true},
		Peers:     []config.PeerConfig{{ID: "manual-peer", Addresses: []string{"/ip4/10.0.0.2/tcp/22000/p2p/manual-peer"}}},
	}

	sources := BuildRuntimeSources(cfg, func(config.Config) discovery.DHTRouter {
		t.Fatalf("disabled discovery must not create public DHT router")
		return nil
	})
	peers, events := PollSources(sources)

	if len(events) != 0 || len(peers) != 1 || peers[0].ID != "manual-peer" {
		t.Fatalf("manual peers should remain without DHT when discovery is disabled: peers=%+v events=%+v", peers, events)
	}
}
