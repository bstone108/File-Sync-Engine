package routing

import (
	"net"
	"net/url"
	"sort"
	"strings"
)

// PathKind describes how a peer would be reached for a transfer.
type PathKind string

const (
	DirectPath    PathKind = "direct"
	RelayPath     PathKind = "relay"
	MeshRelayPath PathKind = "mesh_relay"
)

// NetworkKind describes the network cost/topology class for a candidate path.
type NetworkKind string

const (
	UnknownNetwork         NetworkKind = ""
	LocalNetwork           NetworkKind = "local"
	WANNetwork             NetworkKind = "wan"
	VPNNetwork             NetworkKind = "vpn_overlay"
	ContainerBridgeNetwork NetworkKind = "container_bridge"
)

// SelectionReason is a stable, user/API-facing explanation for a source decision.
type SelectionReason string

const (
	ReasonDirectPreferred                SelectionReason = "direct_preferred"
	ReasonLocalPreferred                 SelectionReason = "local_preferred"
	ReasonRelayCarrierPreferred          SelectionReason = "relay_carrier_preferred"
	ReasonOnlyAvailableSource            SelectionReason = "only_available_source"
	ReasonCooperativeLocalRedistribution SelectionReason = "cooperative_local_redistribution"
)

// CandidateSource is a possible peer/source for one content block or file.
type CandidateSource struct {
	PeerID         string
	ContentID      string
	Path           PathKind
	Network        NetworkKind
	RelayViaPeerID string
	Reachable      bool
}

// SourceSelectionRequest asks the planner to choose a source for one content item.
type SourceSelectionRequest struct {
	ContentID  string
	Candidates []CandidateSource
}

// SourceChoice is the selected source plus the reason that should be surfaced in status/logs.
type SourceChoice struct {
	CandidateSource
	Reason SelectionReason
}

// ChooseTransferSource picks a reachable candidate that has the requested content.
// Direct paths are preferred over relay/mesh paths so data transfer avoids relays
// whenever an equivalent directly reachable peer can provide the same content.
func ChooseTransferSource(req SourceSelectionRequest) (SourceChoice, bool) {
	matches := make([]CandidateSource, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		if !candidate.Reachable || candidate.ContentID != req.ContentID {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) == 0 {
		return SourceChoice{}, false
	}

	relayCarriers := relayCarrierPeers(matches)
	sort.SliceStable(matches, func(i, j int) bool {
		left, right := matches[i], matches[j]
		if pathRank(left.Path) != pathRank(right.Path) {
			return pathRank(left.Path) < pathRank(right.Path)
		}
		if networkRank(left.Network) != networkRank(right.Network) {
			return networkRank(left.Network) < networkRank(right.Network)
		}
		if relayCarriers[left.PeerID] != relayCarriers[right.PeerID] {
			return relayCarriers[left.PeerID]
		}
		return left.PeerID < right.PeerID
	})

	choice := SourceChoice{CandidateSource: matches[0], Reason: ReasonOnlyAvailableSource}
	if relayCarriers[choice.PeerID] && hasPeerBehindRelay(matches, choice.PeerID) {
		choice.Reason = ReasonRelayCarrierPreferred
	} else if choice.Network == LocalNetwork && hasNonLocalCandidate(matches) {
		choice.Reason = ReasonLocalPreferred
	} else if choice.Path == DirectPath && hasRelayCandidate(matches) {
		choice.Reason = ReasonDirectPreferred
	}
	return choice, true
}

func pathRank(path PathKind) int {
	switch path {
	case DirectPath:
		return 0
	case RelayPath:
		return 1
	case MeshRelayPath:
		return 2
	default:
		return 3
	}
}

func networkRank(network NetworkKind) int {
	switch network {
	case LocalNetwork:
		return 0
	case WANNetwork, VPNNetwork, UnknownNetwork:
		return 1
	default:
		return 2
	}
}

// NetworkHints carries deployment knowledge that cannot be inferred safely from an
// endpoint address alone, such as a Docker host-gateway address that the embedding
// product already proved reaches a true local peer path.
type NetworkHints struct {
	LocalContainerGatewayIPs []string
	LocalCIDRs               []string
	PublishedPortMappings    []PublishedPortMapping
}

// PublishedPortMapping names a container/host published-port relationship that
// a deployment has already proven reaches a true local peer path. Matching is
// deliberately exact by host IP and host port so one published port does not
// promote every address on a Docker bridge host to true LAN.
type PublishedPortMapping struct {
	HostIP   string
	HostPort int
}

// ClassifyEndpointNetwork returns the conservative topology class used for source selection.
// Private/loopback/link-local addresses are true local candidates; VPN/overlay ranges are
// deliberately treated like WAN for bandwidth/source-cost decisions. Common Docker bridge
// ranges are not treated as true LAN unless the caller supplies an explicit local gateway hint.
func ClassifyEndpointNetwork(address string) NetworkKind {
	return ClassifyEndpointNetworkWithHints(address, NetworkHints{})
}

// ClassifyEndpointNetworkWithHints classifies an endpoint with caller-supplied container
// topology hints. Hints are intentionally explicit so Docker bridge/NAT addresses are not
// mistaken for real LAN paths merely because they use RFC1918 space.
func ClassifyEndpointNetworkWithHints(address string, hints NetworkHints) NetworkKind {
	host, port := endpointHostPort(address)
	if host == "" {
		return WANNetwork
	}
	lower := strings.ToLower(strings.Trim(host, "[]"))
	if strings.HasSuffix(lower, ".local") || lower == "localhost" {
		return LocalNetwork
	}
	ip := net.ParseIP(lower)
	if ip == nil {
		return WANNetwork
	}
	if ipInStringList(ip, hints.LocalContainerGatewayIPs) {
		return LocalNetwork
	}
	if ipInCIDRList(ip, hints.LocalCIDRs) {
		return LocalNetwork
	}
	if ipInPublishedPortMappings(ip, port, hints.PublishedPortMappings) {
		return LocalNetwork
	}
	if isVPNOverlayIP(ip) {
		return VPNNetwork
	}
	if isDockerBridgeIP(ip) {
		return ContainerBridgeNetwork
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return LocalNetwork
	}
	return WANNetwork
}

func endpointHost(address string) string {
	host, _ := endpointHostPort(address)
	return host
}

func endpointHostPort(address string) (string, int) {
	parsed, err := url.Parse(address)
	if err == nil && parsed.Host != "" {
		return parsed.Hostname(), parsedPort(parsed.Port())
	}
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		return host, parsedPort(port)
	}
	return address, 0
}

func parsedPort(value string) int {
	if value == "" {
		return 0
	}
	port := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		port = port*10 + int(r-'0')
	}
	return port
}

func isVPNOverlayIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 != nil {
		// Tailscale/Headscale CGNAT range. It is routable through an overlay, not a true LAN.
		return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return false
	}
	// Tailscale IPv6 unique-local prefix fd7a:115c:a1e0::/48 is an overlay path,
	// not evidence that the peer is on the same physical LAN.
	return ip16[0] == 0xfd && ip16[1] == 0x7a && ip16[2] == 0x11 && ip16[3] == 0x5c && ip16[4] == 0xa1 && ip16[5] == 0xe0
}

func isDockerBridgeIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	// Docker's default and commonly allocated user-defined bridge subnets live in
	// 172.17.0.0/16 and adjacent 172.18.0.0/16-172.31.0.0/16 ranges. Treat these
	// as container bridge/NAT by default so they are not mistaken for true LAN paths;
	// deployments with proven host-gateway/LAN reachability can promote exact IPs
	// through NetworkHints.
	return ip4[0] == 172 && ip4[1] >= 17 && ip4[1] <= 31
}

func ipInStringList(ip net.IP, values []string) bool {
	for _, value := range values {
		candidate := net.ParseIP(strings.TrimSpace(value))
		if candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

func ipInCIDRList(ip net.IP, values []string) bool {
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func ipInPublishedPortMappings(ip net.IP, port int, mappings []PublishedPortMapping) bool {
	if port <= 0 {
		return false
	}
	for _, mapping := range mappings {
		candidate := net.ParseIP(strings.TrimSpace(mapping.HostIP))
		if candidate != nil && candidate.Equal(ip) && mapping.HostPort == port {
			return true
		}
	}
	return false
}

func hasRelayCandidate(candidates []CandidateSource) bool {
	for _, candidate := range candidates {
		if candidate.Path == RelayPath || candidate.Path == MeshRelayPath {
			return true
		}
	}
	return false
}

func hasNonLocalCandidate(candidates []CandidateSource) bool {
	for _, candidate := range candidates {
		if candidate.Network != LocalNetwork {
			return true
		}
	}
	return false
}

func relayCarrierPeers(candidates []CandidateSource) map[string]bool {
	available := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		available[candidate.PeerID] = true
	}
	carriers := make(map[string]bool)
	for _, candidate := range candidates {
		if candidate.RelayViaPeerID != "" && available[candidate.RelayViaPeerID] {
			carriers[candidate.RelayViaPeerID] = true
		}
	}
	return carriers
}

func hasPeerBehindRelay(candidates []CandidateSource, relayPeerID string) bool {
	for _, candidate := range candidates {
		if candidate.RelayViaPeerID == relayPeerID {
			return true
		}
	}
	return false
}

// CooperativeFetchAction describes how a peer should obtain a missing block when
// multiple true-local peers need the same Internet-only content.
type CooperativeFetchAction string

const (
	CooperativeFetchWAN   CooperativeFetchAction = "fetch_wan"
	CooperativeFetchLocal CooperativeFetchAction = "fetch_local"
)

// CooperativeFetchPeer describes one peer participating in a same-LAN fetch plan.
type CooperativeFetchPeer struct {
	PeerID                string
	Network               NetworkKind
	NeedsBlock            bool
	CanFetchWAN           bool
	LocalReachablePeerIDs []string
}

// CooperativeBlockFetchRequest asks the planner to avoid redundant WAN fetches for one block.
type CooperativeBlockFetchRequest struct {
	BlockID string
	Peers   []CooperativeFetchPeer
}

// CooperativeFetchAssignment tells one peer whether to fetch the block from WAN or locally.
type CooperativeFetchAssignment struct {
	PeerID       string
	Action       CooperativeFetchAction
	SourcePeerID string
}

// CooperativeBlockFetchPlan is a deterministic reportable block-fetch coordination plan.
type CooperativeBlockFetchPlan struct {
	BlockID     string
	Assignments []CooperativeFetchAssignment
	Reason      SelectionReason
}

// PlanCooperativeBlockFetch assigns at most one WAN fetcher per true-local connected
// peer group when several peers need the same missing Internet-only block. Peers outside
// that true-local group keep independent WAN fetch assignments instead of assuming a
// relay/mesh redistribution path is cheap or safe for data transfer.
func PlanCooperativeBlockFetch(req CooperativeBlockFetchRequest) CooperativeBlockFetchPlan {
	needing := cooperativeNeedingPeers(req.Peers)
	assignments := make([]CooperativeFetchAssignment, 0, len(needing))
	if len(needing) == 0 {
		return CooperativeBlockFetchPlan{BlockID: req.BlockID}
	}

	groups := cooperativeLocalGroups(needing)
	for _, group := range groups {
		fetcher := chooseCooperativeFetcher(group)
		for _, peer := range group {
			assignment := CooperativeFetchAssignment{PeerID: peer.PeerID, Action: CooperativeFetchWAN}
			if fetcher.PeerID != "" && peer.PeerID != fetcher.PeerID {
				assignment.Action = CooperativeFetchLocal
				assignment.SourcePeerID = fetcher.PeerID
			}
			assignments = append(assignments, assignment)
		}
	}
	sort.SliceStable(assignments, func(i, j int) bool { return assignments[i].PeerID < assignments[j].PeerID })

	reason := ReasonOnlyAvailableSource
	for _, assignment := range assignments {
		if assignment.Action == CooperativeFetchLocal {
			reason = ReasonCooperativeLocalRedistribution
			break
		}
	}
	return CooperativeBlockFetchPlan{BlockID: req.BlockID, Assignments: assignments, Reason: reason}
}

func cooperativeNeedingPeers(peers []CooperativeFetchPeer) []CooperativeFetchPeer {
	needing := make([]CooperativeFetchPeer, 0, len(peers))
	for _, peer := range peers {
		if peer.PeerID == "" || !peer.NeedsBlock {
			continue
		}
		needing = append(needing, peer)
	}
	sort.SliceStable(needing, func(i, j int) bool { return needing[i].PeerID < needing[j].PeerID })
	return needing
}

func cooperativeLocalGroups(peers []CooperativeFetchPeer) [][]CooperativeFetchPeer {
	byID := make(map[string]CooperativeFetchPeer, len(peers))
	for _, peer := range peers {
		byID[peer.PeerID] = peer
	}
	seen := map[string]bool{}
	groups := make([][]CooperativeFetchPeer, 0, len(peers))
	for _, peer := range peers {
		if seen[peer.PeerID] {
			continue
		}
		group := []CooperativeFetchPeer{}
		queue := []CooperativeFetchPeer{peer}
		seen[peer.PeerID] = true
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			group = append(group, current)
			if current.Network != LocalNetwork {
				continue
			}
			for _, nextID := range current.LocalReachablePeerIDs {
				next, ok := byID[nextID]
				if !ok || seen[next.PeerID] || next.Network != LocalNetwork {
					continue
				}
				seen[next.PeerID] = true
				queue = append(queue, next)
			}
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].PeerID < group[j].PeerID })
		groups = append(groups, group)
	}
	return groups
}

func chooseCooperativeFetcher(group []CooperativeFetchPeer) CooperativeFetchPeer {
	for _, peer := range group {
		if peer.CanFetchWAN {
			return peer
		}
	}
	return CooperativeFetchPeer{}
}
