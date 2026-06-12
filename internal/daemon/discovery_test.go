package daemon

import (
	"context"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

type runtimeRecordingDHTRouter struct {
	bootstrap []string
	peers     []discovery.Peer
}

func (r *runtimeRecordingDHTRouter) Bootstrap(ctx context.Context, peers []string) error {
	r.bootstrap = append([]string(nil), peers...)
	return nil
}

func (r *runtimeRecordingDHTRouter) FindPeers(ctx context.Context, namespace string) ([]discovery.Peer, error) {
	return r.peers, nil
}

func TestDiscoverySourcesFromConfigWirePublicDHTAndManualPeers(t *testing.T) {
	router := &runtimeRecordingDHTRouter{peers: []discovery.Peer{{ID: "peer-dht", Addresses: []string{"/ip4/203.0.113.9/tcp/22000/p2p/peer-dht"}}}}
	cfg := config.Config{
		NodeName: "node-a",
		Discovery: config.DiscoveryConfig{
			DHT:               true,
			DHTNamespace:      "fse-test",
			DHTBootstrapPeers: []string{"/dnsaddr/bootstrap.libp2p.io"},
		},
		Peers: []config.PeerConfig{{ID: "peer-manual", Addresses: []string{"tcp://10.0.0.2:22420"}}},
	}

	sources := DiscoverySourcesFromConfig(cfg, router)
	if len(sources) != 2 {
		t.Fatalf("sources = %d, want manual + public DHT", len(sources))
	}
	manualPeers, err := sources[0].Peers()
	if err != nil {
		t.Fatalf("manual peers: %v", err)
	}
	if len(manualPeers) != 1 || manualPeers[0].ID != "peer-manual" {
		t.Fatalf("manual peers not preserved: %+v", manualPeers)
	}
	dhtPeers, err := sources[1].Peers()
	if err != nil {
		t.Fatalf("dht peers: %v", err)
	}
	if len(router.bootstrap) != 1 || router.bootstrap[0] != "/dnsaddr/bootstrap.libp2p.io" {
		t.Fatalf("DHT bootstrap not wired from config: %+v", router.bootstrap)
	}
	if len(dhtPeers) != 1 || dhtPeers[0].ID != "peer-dht" {
		t.Fatalf("DHT peers not returned: %+v", dhtPeers)
	}
}

func TestDiscoverySourcesFromConfigDisableAllSkipsAutomaticSources(t *testing.T) {
	router := &runtimeRecordingDHTRouter{peers: []discovery.Peer{{ID: "peer-dht"}}}
	cfg := config.Config{
		NodeName:  "node-a",
		Discovery: config.DiscoveryConfig{Disabled: true, DHT: true, Local: true},
		Peers:     []config.PeerConfig{{ID: "peer-manual", Addresses: []string{"tcp://10.0.0.2:22420"}}},
	}

	sources := DiscoverySourcesFromConfig(cfg, router)
	if len(sources) != 1 {
		t.Fatalf("sources = %d, want manual only", len(sources))
	}
	peers, err := sources[0].Peers()
	if err != nil {
		t.Fatalf("manual peers: %v", err)
	}
	if len(peers) != 1 || peers[0].ID != "peer-manual" {
		t.Fatalf("manual peers not preserved: %+v", peers)
	}
	if len(router.bootstrap) != 0 {
		t.Fatalf("disabled discovery should not bootstrap DHT: %+v", router.bootstrap)
	}
}
