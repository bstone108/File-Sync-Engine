package discovery

import "testing"

func TestStaticSourceReturnsConfiguredManualPeerAddresses(t *testing.T) {
	source := NewStaticSource([]Peer{
		{ID: "peer-b", Addresses: []string{"/ip4/127.0.0.1/tcp/22001/p2p/peer-b"}},
	})
	peers, err := source.Peers()
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	if len(peers) != 1 || peers[0].ID != "peer-b" || peers[0].Addresses[0] == "" {
		t.Fatalf("unexpected peers: %+v", peers)
	}
}

func TestDiscoveryPlanCanDisableDHTWhileKeepingManualPeers(t *testing.T) {
	plan := Plan{EnableDHT: false, ManualPeers: []Peer{{ID: "peer-b"}}}
	if plan.RequiresDHT() {
		t.Fatalf("manual peers must not require DHT")
	}
	if !plan.HasManualPeers() {
		t.Fatalf("manual peer should be detected")
	}
}

func TestDiscoveryPlanCanDisableAllDiscoveryWhileKeepingManualPeers(t *testing.T) {
	plan := Plan{DisableAll: true, EnableDHT: true, EnableLocal: true, ManualPeers: []Peer{{ID: "peer-b"}}}
	if plan.RequiresDHT() || plan.RequiresLocal() {
		t.Fatalf("DisableAll must suppress DHT/local discovery")
	}
	if !plan.HasManualPeers() {
		t.Fatalf("manual peer should remain available when discovery is disabled")
	}
}
