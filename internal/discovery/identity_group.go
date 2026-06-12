package discovery

import (
	"sort"

	"filesyncengine/internal/config"
)

// FolderAdvertisement is a shared-folder offer for an explicit identity group.
// It intentionally carries no local destination path; newly learned folders must
// be disabled until the user assigns a local path and enables them.
type FolderAdvertisement struct {
	ID        string
	Label     string
	Snapshots []SnapshotMarker
}

type SnapshotMarker struct {
	ID                    string
	FolderID              string
	Cursor                uint64
	StateHash             string
	CreatedAt             string
	Description           string
	Pinned                bool
	Deprecated            bool
	ArchiveFullyProtected bool
	DBCheckpointAvailable bool
}

type IdentityGroupState struct {
	GroupID       string
	KnownPeers    []Peer
	SharedFolders []FolderAdvertisement
}

type LearnedFolder struct {
	ID           string
	Label        string
	Path         string
	Enabled      bool
	AdvertisedBy string
	GroupID      string
}

type IdentityGroupPlan struct {
	ConnectPeers   []Peer
	ShareFolders   []FolderAdvertisement
	LearnedFolders []LearnedFolder
}

// IdentityGroupStatesFromConfig returns the identity-mesh advertisements that a
// configured node can safely share. Only enabled identity groups participate.
// Enabled local folders tagged with an identity group are advertised even when
// their original peer/folder relationship was created manually; this helper does
// not mark them as identity-derived or mutate the source config. Disabled
// no-path folders learned from the identity mesh are intentionally not
// re-advertised as local shares.
func IdentityGroupStatesFromConfig(cfg config.Config) []IdentityGroupState {
	enabledGroups := map[string]struct{}{}
	for _, group := range cfg.Identity.Groups {
		if group.ID == "" || !group.Enabled {
			continue
		}
		enabledGroups[group.ID] = struct{}{}
	}
	groupStates := map[string]*IdentityGroupState{}
	for groupID := range enabledGroups {
		groupStates[groupID] = &IdentityGroupState{GroupID: groupID}
	}
	for _, peer := range cfg.Peers {
		if peer.ID == "" || peer.IdentityPublicKey == "" {
			continue
		}
		for groupID := range enabledGroups {
			groupStates[groupID].KnownPeers = append(groupStates[groupID].KnownPeers, Peer{ID: peer.ID, Addresses: append([]string(nil), peer.Addresses...)})
		}
	}
	for _, folder := range cfg.Folders {
		if folder.ID == "" || folder.IdentityGroup == "" || !folder.Enabled || folder.Path == "" {
			continue
		}
		group, ok := groupStates[folder.IdentityGroup]
		if !ok {
			continue
		}
		group.SharedFolders = append(group.SharedFolders, FolderAdvertisement{ID: folder.ID})
	}
	ids := make([]string, 0, len(groupStates))
	for id := range groupStates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]IdentityGroupState, 0, len(ids))
	for _, id := range ids {
		group := groupStates[id]
		group.KnownPeers = peersFromAddressMap(peerAddressMap(group.KnownPeers))
		group.SharedFolders = normalizeFolderAdvertisements(group.SharedFolders)
		if len(group.KnownPeers) == 0 && len(group.SharedFolders) == 0 {
			continue
		}
		out = append(out, *group)
	}
	return out
}

// PlanIdentityGroupExchange plans the non-destructive work after two peers prove
// membership in the same explicit identity group. The plan is deterministic and
// never chooses local folder paths or enables learned folders automatically.
func PlanIdentityGroupExchange(selfID string, local IdentityGroupState, remoteID string, remote IdentityGroupState) IdentityGroupPlan {
	if local.GroupID == "" || remote.GroupID == "" || local.GroupID != remote.GroupID {
		return IdentityGroupPlan{}
	}

	plan := IdentityGroupPlan{
		ConnectPeers: filterUnknownIdentityPeers(selfID, peerAddressMap(local.KnownPeers), remoteID, remote.KnownPeers),
		ShareFolders: normalizeFolderAdvertisements(local.SharedFolders),
	}
	localFolders := folderAdvertisementSet(local.SharedFolders)
	for _, folder := range normalizeFolderAdvertisements(remote.SharedFolders) {
		if _, exists := localFolders[folder.ID]; exists {
			continue
		}
		plan.LearnedFolders = append(plan.LearnedFolders, LearnedFolder{
			ID:           folder.ID,
			Label:        folder.Label,
			Path:         "",
			Enabled:      false,
			AdvertisedBy: remoteID,
			GroupID:      local.GroupID,
		})
	}
	return plan
}

func filterUnknownIdentityPeers(selfID string, local map[string]map[string]struct{}, remoteID string, remote []Peer) []Peer {
	remoteMap := peerAddressMap(remote)
	delete(remoteMap, selfID)
	for id := range local {
		delete(remoteMap, id)
	}
	if remoteID != "" {
		if peerAddresses, ok := remoteMap[remoteID]; ok && len(peerAddresses) == 0 {
			delete(remoteMap, remoteID)
		}
	}
	return peersFromAddressMap(remoteMap)
}

func normalizeFolderAdvertisements(folders []FolderAdvertisement) []FolderAdvertisement {
	byID := map[string]FolderAdvertisement{}
	for _, folder := range folders {
		if folder.ID == "" {
			continue
		}
		if _, exists := byID[folder.ID]; !exists {
			folder.Snapshots = normalizeSnapshotMarkers(folder.Snapshots)
			byID[folder.ID] = folder
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]FolderAdvertisement, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func normalizeSnapshotMarkers(markers []SnapshotMarker) []SnapshotMarker {
	byID := map[string]SnapshotMarker{}
	for _, marker := range markers {
		if marker.ID == "" || marker.FolderID == "" {
			continue
		}
		if _, exists := byID[marker.ID]; !exists {
			byID[marker.ID] = marker
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]SnapshotMarker, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func folderAdvertisementSet(folders []FolderAdvertisement) map[string]struct{} {
	set := map[string]struct{}{}
	for _, folder := range normalizeFolderAdvertisements(folders) {
		set[folder.ID] = struct{}{}
	}
	return set
}
