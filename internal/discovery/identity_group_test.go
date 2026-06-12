package discovery

import (
	"testing"

	"filesyncengine/internal/config"
)

func TestPlanIdentityGroupExchangeLearnsDisabledFolderAdvertisements(t *testing.T) {
	local := IdentityGroupState{
		GroupID:       "family-sync",
		KnownPeers:    []Peer{{ID: "node-b", Addresses: []string{"tcp://10.0.0.2:22420"}}},
		SharedFolders: []FolderAdvertisement{{ID: "docs", Label: "Documents"}},
	}
	remote := IdentityGroupState{
		GroupID: "family-sync",
		KnownPeers: []Peer{
			{ID: "node-b", Addresses: []string{"tcp://10.0.0.2:22420"}},
			{ID: "node-c", Addresses: []string{"tcp://10.0.0.3:22420"}},
		},
		SharedFolders: []FolderAdvertisement{
			{ID: "docs", Label: "Documents"},
			{ID: "photos", Label: "Photo Library"},
		},
	}

	plan := PlanIdentityGroupExchange("node-a", local, "node-c", remote)

	if got := len(plan.ConnectPeers); got != 1 {
		t.Fatalf("ConnectPeers len = %d, want 1: %+v", got, plan.ConnectPeers)
	}
	if plan.ConnectPeers[0].ID != "node-c" || plan.ConnectPeers[0].Addresses[0] != "tcp://10.0.0.3:22420" {
		t.Fatalf("unexpected connect peer: %+v", plan.ConnectPeers[0])
	}
	if got := len(plan.LearnedFolders); got != 1 {
		t.Fatalf("LearnedFolders len = %d, want 1: %+v", got, plan.LearnedFolders)
	}
	learned := plan.LearnedFolders[0]
	if learned.ID != "photos" || learned.Enabled || learned.Path != "" || learned.Label != "Photo Library" {
		t.Fatalf("learned folder should be disabled with no path and remote label: %+v", learned)
	}
	if got := len(plan.ShareFolders); got != 1 || plan.ShareFolders[0].ID != "docs" {
		t.Fatalf("share folders not preserved: %+v", plan.ShareFolders)
	}
}

func TestPlanIdentityGroupExchangeIgnoresDifferentOrEmptyGroups(t *testing.T) {
	local := IdentityGroupState{GroupID: "family-sync", SharedFolders: []FolderAdvertisement{{ID: "docs"}}}
	remote := IdentityGroupState{GroupID: "other", KnownPeers: []Peer{{ID: "node-c", Addresses: []string{"tcp://10.0.0.3:22420"}}}, SharedFolders: []FolderAdvertisement{{ID: "photos"}}}

	plan := PlanIdentityGroupExchange("node-a", local, "node-c", remote)
	if len(plan.ConnectPeers) != 0 || len(plan.LearnedFolders) != 0 || len(plan.ShareFolders) != 0 {
		t.Fatalf("different groups must not exchange: %+v", plan)
	}

	local.GroupID = ""
	remote.GroupID = ""
	plan = PlanIdentityGroupExchange("node-a", local, "node-c", remote)
	if len(plan.ConnectPeers) != 0 || len(plan.LearnedFolders) != 0 || len(plan.ShareFolders) != 0 {
		t.Fatalf("empty groups must not exchange: %+v", plan)
	}
}

func TestIdentityGroupStatesFromConfigAdvertisesManualFoldersWithoutChangingOrigin(t *testing.T) {
	cfg := config.Config{
		Identity: config.IdentityConfig{Groups: []config.IdentityGroupConfig{
			{ID: "family-sync", Token: "identity-token", Enabled: true},
			{ID: "disabled-sync", Token: "disabled-token", Enabled: false},
		}},
		Peers: []config.PeerConfig{
			{ID: "manual-peer", Addresses: []string{"tcp://10.0.0.2:22420"}},
			{ID: "identity-peer", IdentityPublicKey: "pub-identity", Addresses: []string{"tcp://10.0.0.3:22420"}},
		},
		Folders: []config.FolderConfig{
			{ID: "manual-docs", Path: "/shares/docs", Enabled: true, IdentityGroup: "family-sync"},
			{ID: "learned-photos", Enabled: false, AdvertisedBy: "identity-peer", IdentityGroup: "family-sync"},
			{ID: "disabled-group-folder", Path: "/shares/disabled", Enabled: true, IdentityGroup: "disabled-sync"},
			{ID: "ordinary", Path: "/shares/ordinary", Enabled: true},
		},
	}

	groups := IdentityGroupStatesFromConfig(cfg)

	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want only enabled family-sync group", groups)
	}
	group := groups[0]
	if group.GroupID != "family-sync" {
		t.Fatalf("GroupID = %q, want family-sync", group.GroupID)
	}
	if len(group.KnownPeers) != 1 || group.KnownPeers[0].ID != "identity-peer" {
		t.Fatalf("KnownPeers = %+v, want only identity-derived peers", group.KnownPeers)
	}
	if len(group.SharedFolders) != 1 || group.SharedFolders[0].ID != "manual-docs" {
		t.Fatalf("SharedFolders = %+v, want enabled manually-originated folder advertisement", group.SharedFolders)
	}
	if cfg.Folders[0].AdvertisedBy != "" || !cfg.Folders[0].Enabled {
		t.Fatalf("manual folder origin should remain enabled/manual in config: %+v", cfg.Folders[0])
	}
}
