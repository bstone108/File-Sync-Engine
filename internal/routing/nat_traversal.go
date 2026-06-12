package routing

import "sort"

// NATAssistReason is a stable user/API-facing explanation for direct-session negotiation plans.
type NATAssistReason string

const (
	// NATReasonPeerAssistedDirect means a reachable public peer should be used only as
	// a control-plane helper to exchange endpoint hints and attempt a direct session.
	NATReasonPeerAssistedDirect NATAssistReason = "peer_assisted_direct"
)

// NATAssistPeer describes a reachable peer that may help two peers negotiate a direct path.
// The helper is for connection setup/control-plane hints only; it is not selected as a data relay.
type NATAssistPeer struct {
	PeerID          string
	PublicEndpoint  string
	Reachable       bool
	CanReachPeerIDs []string
}

// NATPunchRequest asks for a peer-assisted direct-connection setup plan.
type NATPunchRequest struct {
	LocalPeerID           string
	TargetPeerID          string
	TargetDirectReachable bool
	AssistPeers           []NATAssistPeer
}

// NATPunchPlan names the helper peer and endpoints to use for direct-session negotiation.
type NATPunchPlan struct {
	TargetPeerID   string
	AssistPeerID   string
	AssistEndpoint string
	Reason         NATAssistReason
}

// ControlPlaneLink is one reachable control-plane hop between peers. Relay and mesh
// links are allowed here only to exchange hints/handshake material for a future direct
// encrypted session; they are not a data-transfer source decision.
type ControlPlaneLink struct {
	FromPeerID string
	ToPeerID   string
	Reachable  bool
	Path       PathKind
}

// ControlPlaneNegotiationRequest asks for a stable peer path that can carry
// direct-session negotiation messages when the target is not directly reachable yet.
type ControlPlaneNegotiationRequest struct {
	LocalPeerID  string
	TargetPeerID string
	Links        []ControlPlaneLink
}

// ControlPlaneNegotiationPlan describes a relay/mesh-assisted control-plane path for
// endpoint hint exchange and hole-punch setup. ControlPlaneOnly is deliberately explicit
// so callers do not mistake this for permission to move file/block data over the path.
type ControlPlaneNegotiationPlan struct {
	PeerPath         []string
	ControlPlaneOnly bool
	Reason           NATAssistReason
}

// PlanNATPunch chooses a stable reachable helper for peer-assisted direct-session negotiation.
// It deliberately returns no plan when the target is already directly reachable or when no
// helper can reach both the local and target peer; data-transfer source selection remains a
// separate decision after a direct encrypted session is established.
func PlanNATPunch(req NATPunchRequest) (NATPunchPlan, bool) {
	if req.TargetDirectReachable || req.LocalPeerID == "" || req.TargetPeerID == "" || req.LocalPeerID == req.TargetPeerID {
		return NATPunchPlan{}, false
	}

	candidates := make([]NATAssistPeer, 0, len(req.AssistPeers))
	for _, peer := range req.AssistPeers {
		if !peer.Reachable || peer.PeerID == "" || peer.PeerID == req.LocalPeerID || peer.PeerID == req.TargetPeerID {
			continue
		}
		if !peerCanReach(peer, req.LocalPeerID) || !peerCanReach(peer, req.TargetPeerID) {
			continue
		}
		candidates = append(candidates, peer)
	}
	if len(candidates) == 0 {
		return NATPunchPlan{}, false
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.PublicEndpoint != right.PublicEndpoint {
			return left.PublicEndpoint < right.PublicEndpoint
		}
		return left.PeerID < right.PeerID
	})
	chosen := candidates[0]
	return NATPunchPlan{
		TargetPeerID:   req.TargetPeerID,
		AssistPeerID:   chosen.PeerID,
		AssistEndpoint: chosen.PublicEndpoint,
		Reason:         NATReasonPeerAssistedDirect,
	}, true
}

// PlanControlPlaneNegotiationPath finds a stable reachable peer path for relay/mesh-assisted
// control messages that help two peers exchange endpoint candidates, perform hole punching,
// and establish a direct encrypted session. The returned path is control-plane-only; callers
// must still use ChooseTransferSource for actual file/block transfer decisions.
func PlanControlPlaneNegotiationPath(req ControlPlaneNegotiationRequest) (ControlPlaneNegotiationPlan, bool) {
	if req.LocalPeerID == "" || req.TargetPeerID == "" || req.LocalPeerID == req.TargetPeerID {
		return ControlPlaneNegotiationPlan{}, false
	}

	links := make([]ControlPlaneLink, 0, len(req.Links))
	for _, link := range req.Links {
		if !link.Reachable || link.FromPeerID == "" || link.ToPeerID == "" || link.FromPeerID == link.ToPeerID {
			continue
		}
		links = append(links, link)
	}
	sort.SliceStable(links, func(i, j int) bool {
		left, right := links[i], links[j]
		if left.FromPeerID != right.FromPeerID {
			return left.FromPeerID < right.FromPeerID
		}
		if left.ToPeerID != right.ToPeerID {
			return left.ToPeerID < right.ToPeerID
		}
		return pathRank(left.Path) < pathRank(right.Path)
	})

	queue := [][]string{{req.LocalPeerID}}
	seen := map[string]bool{req.LocalPeerID: true}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		current := path[len(path)-1]
		if current == req.TargetPeerID {
			return ControlPlaneNegotiationPlan{PeerPath: path, ControlPlaneOnly: true, Reason: NATReasonPeerAssistedDirect}, true
		}
		for _, link := range links {
			if link.FromPeerID != current || seen[link.ToPeerID] {
				continue
			}
			nextPath := append(append([]string(nil), path...), link.ToPeerID)
			seen[link.ToPeerID] = true
			queue = append(queue, nextPath)
		}
	}
	return ControlPlaneNegotiationPlan{}, false
}

func peerCanReach(peer NATAssistPeer, peerID string) bool {
	for _, candidate := range peer.CanReachPeerIDs {
		if candidate == peerID {
			return true
		}
	}
	return false
}
