package runtimesync

import (
	"fmt"
	"sort"
	"strings"

	"filesyncengine/internal/api"
	"filesyncengine/internal/peerpullplan"
	"filesyncengine/internal/routing"
)

// PeerPull is the daemon-runtime view of a peer pull candidate needed by
// cooperative block-fetch planning. It deliberately omits secrets and endpoint
// details so status/event planning can stay separate from transfer execution.
type PeerPull struct {
	PeerID  string
	Path    routing.PathKind
	Network routing.NetworkKind
}

// PeerPullsFromPlans projects full daemon peer-pull plans into the narrow
// runtime-sync view used for cooperative fetch diagnostics. Secrets, URLs,
// local paths, and block source details are intentionally dropped at this
// boundary.
func PeerPullsFromPlans(plans []peerpullplan.Pull) []PeerPull {
	out := make([]PeerPull, 0, len(plans))
	for _, plan := range plans {
		out = append(out, PeerPull{PeerID: plan.PeerID, Path: plan.Path, Network: plan.Network})
	}
	return out
}

// PlanCooperativeBlockFetches builds a coarse live-pass cooperative fetch plan
// for direct true-LAN peers so only one same-LAN peer fetches an Internet-only
// block and the others can reuse it locally.
func PlanCooperativeBlockFetches(folderID string, pulls []PeerPull) []routing.CooperativeBlockFetchPlan {
	peers := make([]routing.CooperativeFetchPeer, 0, len(pulls))
	localPeerIDs := []string{}
	seen := map[string]bool{}
	for _, pull := range pulls {
		if pull.PeerID == "" || seen[pull.PeerID] || pull.Network != routing.LocalNetwork || pull.Path != routing.DirectPath {
			continue
		}
		seen[pull.PeerID] = true
		localPeerIDs = append(localPeerIDs, pull.PeerID)
	}
	if len(localPeerIDs) < 2 {
		return nil
	}
	sort.Strings(localPeerIDs)
	for _, peerID := range localPeerIDs {
		peers = append(peers, routing.CooperativeFetchPeer{
			PeerID:                peerID,
			Network:               routing.LocalNetwork,
			NeedsBlock:            true,
			CanFetchWAN:           true,
			LocalReachablePeerIDs: otherLocalPeerIDs(peerID, localPeerIDs),
		})
	}
	plan := routing.PlanCooperativeBlockFetch(routing.CooperativeBlockFetchRequest{BlockID: folderID + ":live-transfer-pass", Peers: peers})
	if plan.Reason != routing.ReasonCooperativeLocalRedistribution {
		return nil
	}
	return []routing.CooperativeBlockFetchPlan{plan}
}

func otherLocalPeerIDs(peerID string, peers []string) []string {
	out := make([]string, 0, len(peers)-1)
	for _, other := range peers {
		if other != peerID {
			out = append(out, other)
		}
	}
	return out
}

// CooperativeBlockFetchPlanMessage renders a compact stable event/status string
// for GUI/API diagnostics without exposing peer secrets or endpoint details.
func CooperativeBlockFetchPlanMessage(plan routing.CooperativeBlockFetchPlan) string {
	wanFetchers := []string{}
	localReusers := []string{}
	for _, assignment := range plan.Assignments {
		switch assignment.Action {
		case routing.CooperativeFetchWAN:
			wanFetchers = append(wanFetchers, assignment.PeerID)
		case routing.CooperativeFetchLocal:
			localReusers = append(localReusers, fmt.Sprintf("%s<- %s", assignment.PeerID, assignment.SourcePeerID))
		}
	}
	return fmt.Sprintf("cooperativeBlockFetch block=%s reason=%s wanFetchers=%s localReuse=%s", plan.BlockID, plan.Reason, strings.Join(wanFetchers, ","), strings.Join(localReusers, ","))
}

// EventPublisher is the narrow daemon/API event seam needed by runtime sync
// planning. Keeping it here lets cmd/fse delegate cooperative-fetch event
// formatting without exposing transfer secrets or broader daemon state.
type EventPublisher interface {
	Publish(api.Event)
}

// PublishCooperativeBlockFetchPlans publishes compact diagnostics for any
// same-LAN cooperative fetch plans found in the current live transfer pass.
func PublishCooperativeBlockFetchPlans(publisher EventPublisher, folderID string, pulls []PeerPull) {
	for _, plan := range PlanCooperativeBlockFetches(folderID, pulls) {
		publisher.Publish(api.Event{
			Type:     "transfer.cooperative_block_fetch.planned",
			FolderID: folderID,
			Message:  CooperativeBlockFetchPlanMessage(plan),
		})
	}
}
