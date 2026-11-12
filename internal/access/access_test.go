package access

import (
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/peeridentity"
)

func TestShareOnlyCannotManageOrSeeUnsharedFolders(t *testing.T) {
	groups := []string{"family"}
	identity := RoleFor(groups, "family")
	share := RoleFor(groups, "")
	other := RoleFor(groups, "strangers")
	if identity != RoleIdentity || share != RoleShare || other != RoleShare {
		t.Fatalf("roles identity=%s share=%s other=%s", identity, share, other)
	}
	if !MayManage(RoleOwner) || !MayManage(identity) || MayManage(share) {
		t.Fatal("management boundary")
	}
	if MaySeeConfig(share) || !MaySeeConfig(identity) {
		t.Fatal("config visibility")
	}
	if !MaySyncFolder(identity, nil, "a2") {
		t.Fatal("identity peers sync a folder that is not limited")
	}
	if !MaySyncFolder(identity, []string{"guest"}, "a2") {
		t.Fatal("identity peers still sync a folder that names only a share-only peer")
	}
	if MaySyncFolder(share, nil, "guest") {
		t.Fatal("share-only peer saw a folder that was not explicitly shared")
	}
	if MaySyncFolder(RoleFor(nil, ""), nil, "guest") {
		t.Fatal("a pairing-code peer saw every folder before an identity group existed")
	}
	if !MaySyncFolder(share, []string{"guest"}, "guest") {
		t.Fatal("explicit share was refused")
	}
	if MaySyncFolder(share, []string{"a2"}, "guest") {
		t.Fatal("share-only peer saw a folder shared with someone else")
	}
	if GrantsIdentityTrust(share) || !GrantsIdentityTrust(identity) {
		t.Fatal("identity trust leaked through a share-only peer")
	}
}

func TestEncryptionUsesTheHigherLevelAndIdentityStaysAtTen(t *testing.T) {
	if peeridentity.IdentityExchangeEncryptionLevel != 10 {
		t.Fatalf("identity exchange level = %d", peeridentity.IdentityExchangeEncryptionLevel)
	}
	level, err := peeridentity.NegotiateSessionLevel(4, 7)
	if err != nil || level != 7 {
		t.Fatalf("higher peer level = %d %v", level, err)
	}
	level, err = peeridentity.NegotiateSessionLevel(9, 4)
	if err != nil || level != 9 {
		t.Fatalf("higher local level = %d %v", level, err)
	}
	cfg := config.Config{Identity: config.IdentityConfig{Groups: []config.IdentityGroupConfig{{ID: "family", Enabled: true}}}}
	if len(EnabledGroupIDs(cfg)) != 1 {
		t.Fatal(EnabledGroupIDs(cfg))
	}
}
