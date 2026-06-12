package discovery

import "testing"

func TestPeerExchangeSharesKnownGraphAndRelaysNewPeer(t *testing.T) {
	localKnown := []Peer{
		{ID: "peer-b", Addresses: []string{"tcp://10.0.0.2:22420"}},
		{ID: "peer-c", Addresses: []string{"tcp://10.0.0.3:22420"}},
	}
	newPeer := Peer{ID: "peer-d", Addresses: []string{"tcp://10.0.0.4:22420"}}

	plan := PlanPeerExchange("peer-a", localKnown, newPeer, nil)

	if len(plan.ShareWithRemote) != 2 {
		t.Fatalf("ShareWithRemote = %+v, want peer-b and peer-c", plan.ShareWithRemote)
	}
	if plan.ShareWithRemote[0].ID != "peer-b" || plan.ShareWithRemote[1].ID != "peer-c" {
		t.Fatalf("ShareWithRemote order = %+v, want peer-b then peer-c", plan.ShareWithRemote)
	}
	for _, peerID := range []string{"peer-b", "peer-c"} {
		relay := plan.RelayToKnown[peerID]
		if len(relay) != 1 || relay[0].ID != "peer-d" || relay[0].Addresses[0] != "tcp://10.0.0.4:22420" {
			t.Fatalf("RelayToKnown[%s] = %+v, want new peer-d address", peerID, relay)
		}
	}
}

func TestPeerExchangeLearnsRemoteGraphWithoutSelfOrDuplicates(t *testing.T) {
	localKnown := []Peer{
		{ID: "peer-b", Addresses: []string{"tcp://10.0.0.2:22420"}},
	}
	remote := Peer{ID: "peer-d", Addresses: []string{"tcp://10.0.0.4:22420"}}
	remoteKnown := []Peer{
		{ID: "peer-a", Addresses: []string{"tcp://10.0.0.1:22420"}},
		{ID: "peer-b", Addresses: []string{"tcp://10.0.0.2:22420", "tcp://10.0.0.2:22420"}},
		{ID: "peer-c", Addresses: []string{"tcp://10.0.0.3:22420"}},
		{ID: "peer-c", Addresses: []string{"tcp://10.0.0.3:22421"}},
	}

	plan := PlanPeerExchange("peer-a", localKnown, remote, remoteKnown)

	if len(plan.Learned) != 1 {
		t.Fatalf("Learned = %+v, want one new peer-c", plan.Learned)
	}
	if plan.Learned[0].ID != "peer-c" {
		t.Fatalf("Learned[0].ID = %q, want peer-c", plan.Learned[0].ID)
	}
	wantAddresses := []string{"tcp://10.0.0.3:22420", "tcp://10.0.0.3:22421"}
	if len(plan.Learned[0].Addresses) != len(wantAddresses) {
		t.Fatalf("learned addresses = %+v, want %+v", plan.Learned[0].Addresses, wantAddresses)
	}
	for i := range wantAddresses {
		if plan.Learned[0].Addresses[i] != wantAddresses[i] {
			t.Fatalf("learned address %d = %q, want %q", i, plan.Learned[0].Addresses[i], wantAddresses[i])
		}
	}
}
