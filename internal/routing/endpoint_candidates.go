package routing

import "sort"

// EndpointSource describes where a candidate endpoint came from so API/status callers can
// distinguish user-configured addresses from container sidecar discoveries and relay/rendezvous
// control-plane hints.
type EndpointSource string

const (
	EndpointSourceManual     EndpointSource = "manual"
	EndpointSourceSidecar    EndpointSource = "sidecar"
	EndpointSourceRendezvous EndpointSource = "rendezvous"
)

// EndpointObservation is one observed endpoint for a peer from config, discovery, sidecars, or
// helper peers. Reachable should reflect the caller's latest connectivity probe when available.
type EndpointObservation struct {
	PeerID      string
	Address     string
	Reachable   bool
	Path        PathKind
	NetworkHint string
}

// EndpointCandidateRequest combines direct observations with peer-assisted rendezvous helpers.
type EndpointCandidateRequest struct {
	LocalPeerID      string
	TargetPeerID     string
	ManualEndpoints  []EndpointObservation
	SidecarEndpoints []EndpointObservation
	AssistPeers      []NATAssistPeer
	NetworkHints     NetworkHints
}

// EndpointCandidate is a stable, status-friendly candidate connection path for a peer. Rendezvous
// candidates are control-plane-only: they help peers exchange endpoint hints or attempt NAT punching
// and must not be treated as selected file/block transfer routes.
type EndpointCandidate struct {
	PeerID           string
	Address          string
	Path             PathKind
	Network          NetworkKind
	Reachable        bool
	Source           EndpointSource
	ViaPeerID        string
	ControlPlaneOnly bool
}

// DiagnosticCode is a stable API/GUI-facing reason for a connection-path warning.
type DiagnosticCode string

const (
	// DiagnosticContainerBridgeIsolated means a peer has only Docker bridge/NAT-looking direct
	// addresses that are not reachable/promoted to true local, so control-plane relay/rendezvous may
	// be the only visible path until the deployment adds explicit container networking hints.
	DiagnosticContainerBridgeIsolated DiagnosticCode = "container_bridge_isolated"
)

// EndpointDiagnostic explains why a peer's candidate paths may be degraded and what deployment
// settings can improve it. The fields are intentionally compact and stable for API/status display.
type EndpointDiagnostic struct {
	PeerID    string         `json:"peerId"`
	Code      DiagnosticCode `json:"code"`
	Address   string         `json:"address,omitempty"`
	RoutePath PathKind       `json:"routePath,omitempty"`
	Network   NetworkKind    `json:"network,omitempty"`
	Guidance  string         `json:"guidance"`
}

// DiagnoseEndpointCandidates returns status-friendly diagnostics for degraded connection paths.
func DiagnoseEndpointCandidates(candidates []EndpointCandidate) []EndpointDiagnostic {
	byPeer := map[string][]EndpointCandidate{}
	peerOrder := make([]string, 0)
	for _, candidate := range candidates {
		if candidate.PeerID == "" {
			continue
		}
		if _, ok := byPeer[candidate.PeerID]; !ok {
			peerOrder = append(peerOrder, candidate.PeerID)
		}
		byPeer[candidate.PeerID] = append(byPeer[candidate.PeerID], candidate)
	}
	diagnostics := make([]EndpointDiagnostic, 0)
	for _, peerID := range peerOrder {
		peerCandidates := byPeer[peerID]
		if hasReachableLocalDataCandidate(peerCandidates) {
			continue
		}
		container, ok := firstContainerBridgeCandidate(peerCandidates)
		if !ok {
			continue
		}
		routePath := container.Path
		if relay, ok := firstReachableControlPlaneCandidate(peerCandidates); ok {
			routePath = relay.Path
		}
		diagnostics = append(diagnostics, EndpointDiagnostic{
			PeerID:    peerID,
			Code:      DiagnosticContainerBridgeIsolated,
			Address:   container.Address,
			RoutePath: routePath,
			Network:   ContainerBridgeNetwork,
			Guidance:  "container bridge endpoint is not reachable as true LAN; publish the daemon port and add a published-port mapping through discovery.networkHints.publishedPortMappings, localContainerGatewayIPs, or localCIDRs, or run a trusted sidecar/helper so direct local paths can be discovered before using relay/mesh",
		})
	}
	return diagnostics
}

// BuildEndpointCandidates merges manual endpoints, sidecar/helper observations, and peer-assisted
// rendezvous hints into a deterministic list. Reachable direct/sidecar candidates sort first, then
// control-plane rendezvous helpers, then unreachable candidates retained for diagnostics.
func BuildEndpointCandidates(req EndpointCandidateRequest) []EndpointCandidate {
	candidates := make([]EndpointCandidate, 0, len(req.ManualEndpoints)+len(req.SidecarEndpoints)+1)
	for _, endpoint := range req.ManualEndpoints {
		candidate, ok := endpointCandidateFromObservation(endpoint, EndpointSourceManual, req.NetworkHints)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	for _, endpoint := range req.SidecarEndpoints {
		candidate, ok := endpointCandidateFromObservation(endpoint, EndpointSourceSidecar, req.NetworkHints)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	if plan, ok := PlanNATPunch(NATPunchRequest{
		LocalPeerID:           req.LocalPeerID,
		TargetPeerID:          req.TargetPeerID,
		TargetDirectReachable: false,
		AssistPeers:           req.AssistPeers,
	}); ok {
		candidates = append(candidates, EndpointCandidate{
			PeerID:           req.TargetPeerID,
			Address:          plan.AssistEndpoint,
			Path:             MeshRelayPath,
			Network:          ClassifyEndpointNetworkWithHints(plan.AssistEndpoint, req.NetworkHints),
			Reachable:        true,
			Source:           EndpointSourceRendezvous,
			ViaPeerID:        plan.AssistPeerID,
			ControlPlaneOnly: true,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if endpointCandidateRank(left) != endpointCandidateRank(right) {
			return endpointCandidateRank(left) < endpointCandidateRank(right)
		}
		if pathRank(left.Path) != pathRank(right.Path) {
			return pathRank(left.Path) < pathRank(right.Path)
		}
		if networkRank(left.Network) != networkRank(right.Network) {
			return networkRank(left.Network) < networkRank(right.Network)
		}
		if left.PeerID != right.PeerID {
			return left.PeerID < right.PeerID
		}
		if left.Address != right.Address {
			return left.Address < right.Address
		}
		return left.Source < right.Source
	})
	return candidates
}

func endpointCandidateFromObservation(endpoint EndpointObservation, source EndpointSource, hints NetworkHints) (EndpointCandidate, bool) {
	if endpoint.PeerID == "" || endpoint.Address == "" {
		return EndpointCandidate{}, false
	}
	path := endpoint.Path
	if path == "" {
		path = DirectPath
	}
	network := ClassifyEndpointNetworkWithHints(endpoint.Address, hints)
	if endpoint.NetworkHint != "" {
		network = NetworkKind(endpoint.NetworkHint)
	}
	return EndpointCandidate{
		PeerID:    endpoint.PeerID,
		Address:   endpoint.Address,
		Path:      path,
		Network:   network,
		Reachable: endpoint.Reachable,
		Source:    source,
	}, true
}

func hasReachableDirectTarget(candidates []EndpointCandidate, targetPeerID string) bool {
	for _, candidate := range candidates {
		if candidate.PeerID == targetPeerID && candidate.Reachable && candidate.Path == DirectPath && !candidate.ControlPlaneOnly {
			return true
		}
	}
	return false
}

func hasReachableLocalDataCandidate(candidates []EndpointCandidate) bool {
	for _, candidate := range candidates {
		if candidate.Reachable && !candidate.ControlPlaneOnly && candidate.Network == LocalNetwork {
			return true
		}
	}
	return false
}

func firstContainerBridgeCandidate(candidates []EndpointCandidate) (EndpointCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.Network == ContainerBridgeNetwork {
			return candidate, true
		}
	}
	return EndpointCandidate{}, false
}

func firstReachableControlPlaneCandidate(candidates []EndpointCandidate) (EndpointCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.Reachable && candidate.ControlPlaneOnly {
			return candidate, true
		}
	}
	return EndpointCandidate{}, false
}

func endpointCandidateRank(candidate EndpointCandidate) int {
	if candidate.Reachable && !candidate.ControlPlaneOnly {
		return 0
	}
	if candidate.Reachable && candidate.ControlPlaneOnly {
		return 1
	}
	return 2
}
