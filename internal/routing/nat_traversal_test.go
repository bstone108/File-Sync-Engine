package routing

import "testing"

func TestPlanNATPunchUsesReachablePublicPeerThatCanReachBothSides(t *testing.T) {
	plan, ok := PlanNATPunch(NATPunchRequest{
		LocalPeerID:           "local",
		TargetPeerID:          "remote-nat",
		TargetDirectReachable: false,
		AssistPeers: []NATAssistPeer{
			{PeerID: "offline-helper", PublicEndpoint: "tcp://203.0.113.10:22000", Reachable: false, CanReachPeerIDs: []string{"local", "remote-nat"}},
			{PeerID: "public-helper", PublicEndpoint: "tcp://203.0.113.11:22000", Reachable: true, CanReachPeerIDs: []string{"remote-nat", "local"}},
		},
	})
	if !ok {
		t.Fatalf("expected a NAT punch assist plan")
	}
	if plan.AssistPeerID != "public-helper" {
		t.Fatalf("expected public-helper assist peer, got %q", plan.AssistPeerID)
	}
	if plan.TargetPeerID != "remote-nat" {
		t.Fatalf("expected target remote-nat, got %q", plan.TargetPeerID)
	}
	if plan.Reason != NATReasonPeerAssistedDirect {
		t.Fatalf("expected peer-assisted-direct reason, got %q", plan.Reason)
	}
}

func TestPlanNATPunchReturnsNoPlanWhenTargetAlreadyDirect(t *testing.T) {
	_, ok := PlanNATPunch(NATPunchRequest{
		LocalPeerID:           "local",
		TargetPeerID:          "remote",
		TargetDirectReachable: true,
		AssistPeers:           []NATAssistPeer{{PeerID: "helper", PublicEndpoint: "tcp://203.0.113.11:22000", Reachable: true, CanReachPeerIDs: []string{"local", "remote"}}},
	})
	if ok {
		t.Fatalf("did not expect NAT assist when target is already directly reachable")
	}
}

func TestPlanNATPunchRequiresAssistPeerReachBothSides(t *testing.T) {
	_, ok := PlanNATPunch(NATPunchRequest{
		LocalPeerID:  "local",
		TargetPeerID: "remote-nat",
		AssistPeers: []NATAssistPeer{
			{PeerID: "one-sided-helper", PublicEndpoint: "tcp://203.0.113.11:22000", Reachable: true, CanReachPeerIDs: []string{"remote-nat"}},
		},
	})
	if ok {
		t.Fatalf("did not expect NAT assist when no helper can reach both peers")
	}
}

func TestPlanControlPlaneNegotiationPathUsesRelayMeshOnlyForDirectSetup(t *testing.T) {
	plan, ok := PlanControlPlaneNegotiationPath(ControlPlaneNegotiationRequest{
		LocalPeerID:  "local",
		TargetPeerID: "remote-nat",
		Links: []ControlPlaneLink{
			{FromPeerID: "local", ToPeerID: "relay-a", Reachable: true, Path: MeshRelayPath},
			{FromPeerID: "relay-a", ToPeerID: "remote-nat", Reachable: true, Path: RelayPath},
			{FromPeerID: "local", ToPeerID: "offline-shortcut", Reachable: false, Path: DirectPath},
			{FromPeerID: "offline-shortcut", ToPeerID: "remote-nat", Reachable: true, Path: DirectPath},
		},
	})
	if !ok {
		t.Fatalf("expected control-plane negotiation path")
	}
	wantHops := []string{"local", "relay-a", "remote-nat"}
	if len(plan.PeerPath) != len(wantHops) {
		t.Fatalf("expected hops %v, got %v", wantHops, plan.PeerPath)
	}
	for i, want := range wantHops {
		if plan.PeerPath[i] != want {
			t.Fatalf("expected hops %v, got %v", wantHops, plan.PeerPath)
		}
	}
	if !plan.ControlPlaneOnly {
		t.Fatalf("expected relay/mesh path to be marked control-plane only, not a data-transfer route")
	}
	if plan.Reason != NATReasonPeerAssistedDirect {
		t.Fatalf("expected peer-assisted-direct reason, got %q", plan.Reason)
	}
}

func TestBuildEndpointCandidatesIncludesSidecarAndRendezvousHints(t *testing.T) {
	candidates := BuildEndpointCandidates(EndpointCandidateRequest{
		LocalPeerID:  "local",
		TargetPeerID: "remote-nat",
		ManualEndpoints: []EndpointObservation{
			{PeerID: "remote-nat", Address: "http://198.51.100.20:22000", Reachable: false, Path: DirectPath},
		},
		SidecarEndpoints: []EndpointObservation{
			{PeerID: "remote-nat", Address: "http://172.18.0.1:32200", Reachable: true, Path: DirectPath},
		},
		AssistPeers: []NATAssistPeer{
			{PeerID: "public-helper", PublicEndpoint: "tcp://203.0.113.11:22000", Reachable: true, CanReachPeerIDs: []string{"local", "remote-nat"}},
		},
		NetworkHints: NetworkHints{PublishedPortMappings: []PublishedPortMapping{{HostIP: "172.18.0.1", HostPort: 32200}}},
	})

	if len(candidates) != 3 {
		t.Fatalf("expected manual, sidecar, and rendezvous candidates, got %+v", candidates)
	}
	if candidates[0].Source != EndpointSourceSidecar || candidates[0].Network != LocalNetwork || !candidates[0].Reachable {
		t.Fatalf("expected reachable sidecar local candidate first, got %+v", candidates[0])
	}
	if candidates[1].Source != EndpointSourceRendezvous || !candidates[1].ControlPlaneOnly || candidates[1].ViaPeerID != "public-helper" {
		t.Fatalf("expected control-plane rendezvous candidate second, got %+v", candidates[1])
	}
	if candidates[2].Source != EndpointSourceManual || candidates[2].Reachable {
		t.Fatalf("expected unreachable manual candidate retained last for diagnostics, got %+v", candidates[2])
	}
}
