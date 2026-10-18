package link

import "testing"

func TestKeepPairedAddressRejectsOtherDHTNodes(t *testing.T) {
	const peerID = "12D3KooWNeKoPeer"
	circuit := CircuitForPeer("/ip4/10.1.1.1/tcp/4001/p2p/12D3KooWRelay/p2p-circuit", peerID)
	if !KeepPairedAddress(circuit, peerID) {
		t.Fatalf("own circuit dropped: %s", circuit)
	}
	if !KeepPairedAddress("https://10.1.1.5:22420", peerID) {
		t.Fatal("signed https API was dropped")
	}
	for _, foreign := range []string{
		"/ip4/65.109.35.170/tcp/4001/p2p/12D3KooWOther",
		"/ip4/80.255.2.15/tcp/4001/p2p/12D3KooWOther",
		"/ip4/10.2.3.24/tcp/4001/p2p/12D3KooWDHT",
		"http://3.133.83.75:4001",
		"/ip4/10.2.3.24/tcp/22420",
	} {
		if KeepPairedAddress(foreign, peerID) {
			t.Fatalf("foreign address kept: %s", foreign)
		}
	}
	other := CircuitForPeer("/ip4/10.1.1.1/tcp/4001/p2p/12D3KooWRelay/p2p-circuit/p2p/12D3KooWOther", peerID)
	if KeepPairedAddress(other, peerID) {
		t.Fatalf("circuit for a different peer kept: %s", other)
	}
}
