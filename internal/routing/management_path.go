package routing

import (
	"sort"
	"time"
)

// ManagementCommandPathRequest asks for a control-plane route for status,
// configuration, pairing, identity negotiation, or other management traffic.
// Unlike file/block transfer source selection, reachable relay/mesh candidates
// are valid here because they carry small authenticated commands rather than
// bulk sync data.
type ManagementCommandPathRequest struct {
	Purpose    string
	Candidates []EndpointCandidate
}

// ManagementCommandPath is the selected API/GUI-facing management route.
type ManagementCommandPath struct {
	EndpointCandidate
	Purpose              string
	AllowedForManagement bool
	RelayedMayBeSlow     bool
	UserMessage          string
}

// PlanManagementCommandPath chooses a reachable route for management commands.
// It prefers direct routes, but deliberately allows control-plane-only relay or
// mesh paths so offline/unreachable identity-linked nodes can still be inspected
// or queued for configuration through trusted peers. Bulk data transfer remains
// governed by ChooseTransferSource, which rejects control-plane-only candidates.
func PlanManagementCommandPath(req ManagementCommandPathRequest) (ManagementCommandPath, bool) {
	matches := managementCandidates(req.Candidates, "")
	if len(matches) == 0 {
		return ManagementCommandPath{}, false
	}

	sortManagementCandidates(matches)

	choice := ManagementCommandPath{
		EndpointCandidate:    matches[0],
		Purpose:              req.Purpose,
		AllowedForManagement: true,
		UserMessage:          "management command can use a direct encrypted control path",
	}
	if choice.Path == RelayPath || choice.Path == MeshRelayPath || choice.ControlPlaneOnly {
		choice.RelayedMayBeSlow = true
		choice.UserMessage = "management command is relayed through the trusted control mesh and may take longer while messages hop between peers"
	}
	return choice, true
}

// MeshSettingsRetryRequest plans one scheduler tick for pending remote settings
// changes. It is intentionally non-mutating: callers persist attempt counts,
// next-attempt times, and terminal statuses after attempting the returned
// deliveries through ExchangeMeshSettings or another authenticated mesh path.
type MeshSettingsRetryRequest struct {
	Now         time.Time
	MaxAttempts int
	Changes     []MeshSettingsRetryChange
	Candidates  []EndpointCandidate
}

// MeshSettingsRetryChange is the scheduler-visible subset of a durable pending
// settings change. Status values such as acked/failed/applied are terminal and
// are not retried; queued/delivered/pending changes remain eligible when due.
type MeshSettingsRetryChange struct {
	ID            string
	TargetNodeID  string
	Status        string
	Attempts      int
	NextAttemptAt time.Time
}

type MeshSettingsRetryPlan struct {
	Deliveries []MeshSettingsRetryDelivery
	Skipped    []MeshSettingsRetrySkip
}

type MeshSettingsRetryDelivery struct {
	EndpointCandidate
	ChangeID         string
	TargetNodeID     string
	Attempt          int
	RelayedMayBeSlow bool
	Reason           string
}

type MeshSettingsRetrySkip struct {
	ChangeID     string
	TargetNodeID string
	Reason       string
}

// PlanMeshSettingsRetryDelivery selects reachable direct or relay/mesh
// management paths for pending settings changes so a recurring scheduler can
// keep them moving through identity peers until each change is acknowledged or
// durably failed. It never selects bulk-transfer-only routing; relayed paths are
// explicitly marked slow for GUI/API status.
func PlanMeshSettingsRetryDelivery(req MeshSettingsRetryRequest) MeshSettingsRetryPlan {
	plan := MeshSettingsRetryPlan{}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	changes := append([]MeshSettingsRetryChange(nil), req.Changes...)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].TargetNodeID != changes[j].TargetNodeID {
			return changes[i].TargetNodeID < changes[j].TargetNodeID
		}
		return changes[i].ID < changes[j].ID
	})
	for _, change := range changes {
		if !meshSettingsRetryableStatus(change.Status) {
			plan.Skipped = append(plan.Skipped, MeshSettingsRetrySkip{ChangeID: change.ID, TargetNodeID: change.TargetNodeID, Reason: "terminal_status"})
			continue
		}
		if !change.NextAttemptAt.IsZero() && change.NextAttemptAt.After(now) {
			plan.Skipped = append(plan.Skipped, MeshSettingsRetrySkip{ChangeID: change.ID, TargetNodeID: change.TargetNodeID, Reason: "not_due"})
			continue
		}
		if req.MaxAttempts > 0 && change.Attempts >= req.MaxAttempts {
			plan.Skipped = append(plan.Skipped, MeshSettingsRetrySkip{ChangeID: change.ID, TargetNodeID: change.TargetNodeID, Reason: "max_attempts_reached"})
			continue
		}
		choice, ok := PlanManagementCommandPath(ManagementCommandPathRequest{Purpose: "mesh_settings_retry", Candidates: managementCandidates(req.Candidates, change.TargetNodeID)})
		if !ok {
			plan.Skipped = append(plan.Skipped, MeshSettingsRetrySkip{ChangeID: change.ID, TargetNodeID: change.TargetNodeID, Reason: "no_reachable_management_path"})
			continue
		}
		plan.Deliveries = append(plan.Deliveries, MeshSettingsRetryDelivery{ChangeID: change.ID, TargetNodeID: change.TargetNodeID, Attempt: change.Attempts + 1, EndpointCandidate: choice.EndpointCandidate, RelayedMayBeSlow: choice.RelayedMayBeSlow, Reason: choice.UserMessage})
	}
	return plan
}

func meshSettingsRetryableStatus(status string) bool {
	switch status {
	case "", "queued", "pending", "delivered":
		return true
	default:
		return false
	}
}

func managementCandidates(candidates []EndpointCandidate, peerID string) []EndpointCandidate {
	matches := make([]EndpointCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Reachable {
			continue
		}
		if peerID != "" && candidate.PeerID != peerID {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func sortManagementCandidates(matches []EndpointCandidate) {
	sort.SliceStable(matches, func(i, j int) bool {
		left, right := matches[i], matches[j]
		if managementPathRank(left) != managementPathRank(right) {
			return managementPathRank(left) < managementPathRank(right)
		}
		if networkRank(left.Network) != networkRank(right.Network) {
			return networkRank(left.Network) < networkRank(right.Network)
		}
		if left.PeerID != right.PeerID {
			return left.PeerID < right.PeerID
		}
		return left.Address < right.Address
	})
}

func managementPathRank(candidate EndpointCandidate) int {
	if candidate.Path == DirectPath && !candidate.ControlPlaneOnly {
		return 0
	}
	if candidate.Path == RelayPath || candidate.Path == MeshRelayPath || candidate.ControlPlaneOnly {
		return 1
	}
	return 2
}
