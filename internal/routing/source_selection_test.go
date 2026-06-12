package routing

import (
	"strings"
	"testing"
	"time"
)

func TestPlanManagementCommandPathAllowsRelayMeshForControlPlane(t *testing.T) {
	plan, ok := PlanManagementCommandPath(ManagementCommandPathRequest{
		Purpose: "settings_update",
		Candidates: []EndpointCandidate{
			{PeerID: "offline-direct", Address: "tcp://198.51.100.10:22000", Path: DirectPath, Network: WANNetwork, Reachable: false},
			{PeerID: "reachable-relay", Address: "relay://relay-a/offline-direct", Path: MeshRelayPath, Network: WANNetwork, Reachable: true, ViaPeerID: "relay-a", ControlPlaneOnly: true},
		},
	})
	if !ok {
		t.Fatalf("expected reachable management path")
	}
	if plan.PeerID != "reachable-relay" || plan.Path != MeshRelayPath {
		t.Fatalf("expected mesh relay control-plane path, got %+v", plan)
	}
	if !plan.AllowedForManagement || !plan.RelayedMayBeSlow {
		t.Fatalf("expected relayed management path to be explicitly allowed and marked slow, got %+v", plan)
	}
	if !strings.Contains(plan.UserMessage, "relayed") || !strings.Contains(plan.UserMessage, "may take longer") {
		t.Fatalf("expected user-facing relay latency message, got %q", plan.UserMessage)
	}
}

func TestPlanManagementCommandPathPrefersDirectButDoesNotRejectControlPlaneOnlyRelay(t *testing.T) {
	plan, ok := PlanManagementCommandPath(ManagementCommandPathRequest{
		Purpose: "status",
		Candidates: []EndpointCandidate{
			{PeerID: "mesh-control", Address: "relay://relay-a/peer", Path: MeshRelayPath, Network: WANNetwork, Reachable: true, ViaPeerID: "relay-a", ControlPlaneOnly: true},
			{PeerID: "direct-peer", Address: "tcp://192.0.2.10:22000", Path: DirectPath, Network: WANNetwork, Reachable: true},
		},
	})
	if !ok {
		t.Fatalf("expected reachable management path")
	}
	if plan.PeerID != "direct-peer" || plan.RelayedMayBeSlow {
		t.Fatalf("expected direct management path to win without relay warning, got %+v", plan)
	}
}

func TestPlanMeshSettingsRetryDeliveryUsesReachableIdentityHopsUntilAck(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	plan := PlanMeshSettingsRetryDelivery(MeshSettingsRetryRequest{
		Now:         now,
		MaxAttempts: 3,
		Changes: []MeshSettingsRetryChange{
			{ID: "change-acked", TargetNodeID: "node-b", Status: "acked"},
			{ID: "change-too-early", TargetNodeID: "node-b", Status: "queued", NextAttemptAt: now.Add(time.Minute)},
			{ID: "change-exhausted", TargetNodeID: "node-b", Status: "queued", Attempts: 3},
			{ID: "change-direct", TargetNodeID: "node-b", Status: "queued", Attempts: 1},
			{ID: "change-relay", TargetNodeID: "node-c", Status: "queued"},
		},
		Candidates: []EndpointCandidate{
			{PeerID: "node-b", Address: "tcp://192.0.2.20:22000", Path: DirectPath, Network: WANNetwork, Reachable: true},
			{PeerID: "node-c", Address: "mesh://relay-a/node-c", Path: MeshRelayPath, Network: WANNetwork, Reachable: true, ViaPeerID: "relay-a", ControlPlaneOnly: true},
		},
	})

	if len(plan.Deliveries) != 2 {
		t.Fatalf("expected two retry deliveries, got %+v", plan.Deliveries)
	}
	if got := plan.Deliveries[0]; got.ChangeID != "change-direct" || got.TargetNodeID != "node-b" || got.Attempt != 2 || got.Path != DirectPath || got.RelayedMayBeSlow {
		t.Fatalf("expected direct retry for node-b as second attempt, got %+v", got)
	}
	if got := plan.Deliveries[1]; got.ChangeID != "change-relay" || got.TargetNodeID != "node-c" || got.Attempt != 1 || got.Path != MeshRelayPath || got.ViaPeerID != "relay-a" || !got.RelayedMayBeSlow {
		t.Fatalf("expected relayed retry for node-c through identity mesh, got %+v", got)
	}
	if len(plan.Skipped) != 3 {
		t.Fatalf("expected three skipped changes, got %+v", plan.Skipped)
	}
	wantReasons := map[string]string{
		"change-acked":     "terminal_status",
		"change-too-early": "not_due",
		"change-exhausted": "max_attempts_reached",
	}
	for _, skipped := range plan.Skipped {
		if wantReasons[skipped.ChangeID] != skipped.Reason {
			t.Fatalf("unexpected skip %+v, want reason %q", skipped, wantReasons[skipped.ChangeID])
		}
	}
}

func TestChooseTransferSourcePrefersDirectPeerOverRelayForSameBlock(t *testing.T) {
	candidates := []CandidateSource{
		{PeerID: "relay-only-copy", ContentID: "block-a", Path: RelayPath, Reachable: true},
		{PeerID: "direct-copy", ContentID: "block-a", Path: DirectPath, Reachable: true},
	}

	choice, ok := ChooseTransferSource(SourceSelectionRequest{ContentID: "block-a", Candidates: candidates})
	if !ok {
		t.Fatalf("expected a transfer source")
	}
	if choice.PeerID != "direct-copy" {
		t.Fatalf("expected direct-copy, got %q", choice.PeerID)
	}
	if choice.Path != DirectPath {
		t.Fatalf("expected direct path, got %q", choice.Path)
	}
	if choice.Reason != ReasonDirectPreferred {
		t.Fatalf("expected direct-preferred reason, got %q", choice.Reason)
	}
}

func TestChooseTransferSourceAllowsRelayWhenItIsOnlyReachableSource(t *testing.T) {
	candidates := []CandidateSource{
		{PeerID: "offline-direct", ContentID: "block-a", Path: DirectPath, Reachable: false},
		{PeerID: "relay-copy", ContentID: "block-a", Path: MeshRelayPath, Reachable: true},
	}

	choice, ok := ChooseTransferSource(SourceSelectionRequest{ContentID: "block-a", Candidates: candidates})
	if !ok {
		t.Fatalf("expected a transfer source")
	}
	if choice.PeerID != "relay-copy" {
		t.Fatalf("expected relay-copy, got %q", choice.PeerID)
	}
	if choice.Reason != ReasonOnlyAvailableSource {
		t.Fatalf("expected only-available reason, got %q", choice.Reason)
	}
}

func TestChooseTransferSourceIgnoresCandidatesWithoutRequestedContent(t *testing.T) {
	candidates := []CandidateSource{
		{PeerID: "direct-wrong-block", ContentID: "block-b", Path: DirectPath, Reachable: true},
		{PeerID: "relay-copy", ContentID: "block-a", Path: RelayPath, Reachable: true},
	}

	choice, ok := ChooseTransferSource(SourceSelectionRequest{ContentID: "block-a", Candidates: candidates})
	if !ok {
		t.Fatalf("expected a transfer source")
	}
	if choice.PeerID != "relay-copy" {
		t.Fatalf("expected relay-copy, got %q", choice.PeerID)
	}
}

func TestChooseTransferSourcePrefersLocalNetworkPeerOverDirectWANPeer(t *testing.T) {
	candidates := []CandidateSource{
		{PeerID: "direct-wan", ContentID: "block-a", Path: DirectPath, Network: WANNetwork, Reachable: true},
		{PeerID: "direct-lan", ContentID: "block-a", Path: DirectPath, Network: LocalNetwork, Reachable: true},
	}

	choice, ok := ChooseTransferSource(SourceSelectionRequest{ContentID: "block-a", Candidates: candidates})
	if !ok {
		t.Fatalf("expected a transfer source")
	}
	if choice.PeerID != "direct-lan" {
		t.Fatalf("expected direct-lan, got %q", choice.PeerID)
	}
	if choice.Reason != ReasonLocalPreferred {
		t.Fatalf("expected local-preferred reason, got %q", choice.Reason)
	}
}

func TestChooseTransferSourcePrefersRelayCarrierPeerOverPeerBehindSameRelay(t *testing.T) {
	candidates := []CandidateSource{
		{PeerID: "peer-a-behind-relay", ContentID: "block-a", Path: RelayPath, Network: WANNetwork, RelayViaPeerID: "peer-z-carrier", Reachable: true},
		{PeerID: "peer-z-carrier", ContentID: "block-a", Path: RelayPath, Network: WANNetwork, Reachable: true},
	}

	choice, ok := ChooseTransferSource(SourceSelectionRequest{ContentID: "block-a", Candidates: candidates})
	if !ok {
		t.Fatalf("expected a transfer source")
	}
	if choice.PeerID != "peer-z-carrier" {
		t.Fatalf("expected relay carrier peer to provide the block directly, got %q", choice.PeerID)
	}
	if choice.Reason != ReasonRelayCarrierPreferred {
		t.Fatalf("expected relay-carrier-preferred reason, got %q", choice.Reason)
	}
}

func TestPlanCooperativeBlockFetchAssignsOneWANFetcherAndLocalRedistribution(t *testing.T) {
	plan := PlanCooperativeBlockFetch(CooperativeBlockFetchRequest{
		BlockID: "block-internet-only",
		Peers: []CooperativeFetchPeer{
			{PeerID: "lan-b", Network: LocalNetwork, NeedsBlock: true, CanFetchWAN: true, LocalReachablePeerIDs: []string{"lan-c"}},
			{PeerID: "lan-a", Network: LocalNetwork, NeedsBlock: true, CanFetchWAN: true, LocalReachablePeerIDs: []string{"lan-b", "lan-c"}},
			{PeerID: "lan-c", Network: LocalNetwork, NeedsBlock: true, CanFetchWAN: true, LocalReachablePeerIDs: []string{"lan-a", "lan-b"}},
		},
	})

	if len(plan.Assignments) != 3 {
		t.Fatalf("expected one assignment per needing local peer, got %+v", plan.Assignments)
	}
	if got := plan.Assignments[0]; got.PeerID != "lan-a" || got.Action != CooperativeFetchWAN || got.SourcePeerID != "" {
		t.Fatalf("expected lexically stable lan-a WAN fetcher, got %+v", got)
	}
	for _, got := range plan.Assignments[1:] {
		if got.Action != CooperativeFetchLocal || got.SourcePeerID != "lan-a" {
			t.Fatalf("expected remaining peers to fetch locally from lan-a, got %+v", plan.Assignments)
		}
	}
	if plan.Reason != ReasonCooperativeLocalRedistribution {
		t.Fatalf("expected cooperative redistribution reason, got %q", plan.Reason)
	}
}

func TestPlanCooperativeBlockFetchLeavesIndependentWANFetchesWhenPeersAreNotLocal(t *testing.T) {
	plan := PlanCooperativeBlockFetch(CooperativeBlockFetchRequest{
		BlockID: "block-internet-only",
		Peers: []CooperativeFetchPeer{
			{PeerID: "wan-a", Network: WANNetwork, NeedsBlock: true, CanFetchWAN: true},
			{PeerID: "lan-a", Network: LocalNetwork, NeedsBlock: true, CanFetchWAN: true},
		},
	})

	if len(plan.Assignments) != 2 {
		t.Fatalf("expected two independent assignments, got %+v", plan.Assignments)
	}
	for _, got := range plan.Assignments {
		if got.Action != CooperativeFetchWAN || got.SourcePeerID != "" {
			t.Fatalf("expected independent WAN fetches when peers are not true-local partners, got %+v", plan.Assignments)
		}
	}
}

func TestClassifyEndpointNetworkTreatsDockerBridgeAsContainerNotTrueLAN(t *testing.T) {
	if got := ClassifyEndpointNetwork("http://172.17.0.1:22000"); got != ContainerBridgeNetwork {
		t.Fatalf("expected Docker bridge address to be container_bridge, got %q", got)
	}
	if got := ClassifyEndpointNetwork("http://172.18.4.5:22000"); got != ContainerBridgeNetwork {
		t.Fatalf("expected Docker custom bridge address to be container_bridge, got %q", got)
	}
}

func TestClassifyEndpointNetworkWithHintsPromotesDockerGatewayToTrueLocal(t *testing.T) {
	got := ClassifyEndpointNetworkWithHints("http://172.17.0.1:22000", NetworkHints{LocalContainerGatewayIPs: []string{"172.17.0.1"}})
	if got != LocalNetwork {
		t.Fatalf("expected hinted Docker gateway to be true local, got %q", got)
	}
}

func TestClassifyEndpointNetworkWithHintsPromotesConfiguredLocalCIDR(t *testing.T) {
	got := ClassifyEndpointNetworkWithHints("http://172.20.4.10:22000", NetworkHints{LocalCIDRs: []string{"172.20.0.0/16"}})
	if got != LocalNetwork {
		t.Fatalf("expected configured LAN CIDR to promote endpoint to local, got %q", got)
	}
}

func TestClassifyEndpointNetworkWithHintsPromotesPublishedPortMapping(t *testing.T) {
	got := ClassifyEndpointNetworkWithHints("http://172.18.0.1:32200", NetworkHints{PublishedPortMappings: []PublishedPortMapping{{HostIP: "172.18.0.1", HostPort: 32200}}})
	if got != LocalNetwork {
		t.Fatalf("expected published port mapping to promote endpoint to local, got %q", got)
	}

	otherPort := ClassifyEndpointNetworkWithHints("http://172.18.0.1:32201", NetworkHints{PublishedPortMappings: []PublishedPortMapping{{HostIP: "172.18.0.1", HostPort: 32200}}})
	if otherPort != ContainerBridgeNetwork {
		t.Fatalf("published port mapping should not promote every port on the host IP, got %q", otherPort)
	}
}

func TestClassifyEndpointNetworkTreatsTailscaleIPv6AsVPNOverlay(t *testing.T) {
	if got := ClassifyEndpointNetwork("https://[fd7a:115c:a1e0::1]:22000"); got != VPNNetwork {
		t.Fatalf("expected Tailscale IPv6 endpoint to be vpn_overlay, got %q", got)
	}
}

func TestDiagnoseEndpointCandidatesExplainsContainerBridgeIsolation(t *testing.T) {
	diagnostics := DiagnoseEndpointCandidates([]EndpointCandidate{
		{PeerID: "container-peer", Address: "http://172.18.0.5:22000", Path: DirectPath, Network: ContainerBridgeNetwork, Reachable: false, Source: EndpointSourceManual},
		{PeerID: "container-peer", Address: "relay://relay-a/container-peer", Path: MeshRelayPath, Network: WANNetwork, Reachable: true, Source: EndpointSourceRendezvous, ViaPeerID: "relay-a", ControlPlaneOnly: true},
	})

	if len(diagnostics) != 1 {
		t.Fatalf("expected one container networking diagnostic, got %+v", diagnostics)
	}
	got := diagnostics[0]
	if got.Code != DiagnosticContainerBridgeIsolated || got.Network != ContainerBridgeNetwork || got.RoutePath != MeshRelayPath {
		t.Fatalf("unexpected diagnostic identity: %+v", got)
	}
	for _, want := range []string{"container bridge", "published-port", "localContainerGatewayIPs", "localCIDRs", "sidecar"} {
		if !strings.Contains(got.Guidance, want) {
			t.Fatalf("guidance should mention %q, got %q", want, got.Guidance)
		}
	}
}
