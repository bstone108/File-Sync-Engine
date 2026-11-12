package config

import "testing"

func TestMigrateExplicitShareACLKeepsOnlySharesThatAlreadySynced(t *testing.T) {
	open := Config{
		Peers: []PeerConfig{
			{ID: "paired", APIKey: "secret"},
			{ID: "named", APIKey: "secret"},
			{ID: "unpaired"},
		},
		Folders: []FolderConfig{
			{ID: "everything"},
			{ID: "chosen", SharedWith: []string{"named"}},
		},
	}
	migrated, changed := MigrateExplicitShareACL(open)
	if !changed || migrated.ShareACLVersion != 1 {
		t.Fatalf("migration = %#v changed=%v", migrated.ShareACLVersion, changed)
	}
	if len(migrated.Folders[0].SharedWith) != 2 || migrated.Folders[0].SharedWith[0] != "paired" || migrated.Folders[0].SharedWith[1] != "named" {
		t.Fatalf("open folder = %#v", migrated.Folders[0].SharedWith)
	}
	if len(migrated.Folders[1].SharedWith) != 1 || migrated.Folders[1].SharedWith[0] != "named" {
		t.Fatalf("explicit folder = %#v", migrated.Folders[1].SharedWith)
	}
	migrated.Folders[0].SharedWith = nil
	again, changed := MigrateExplicitShareACL(migrated)
	if changed || len(again.Folders[0].SharedWith) != 0 {
		t.Fatal("migration ran a second time and reopened the folder")
	}

	grouped := Config{
		Identity: IdentityConfig{Groups: []IdentityGroupConfig{{ID: "identity-a", Token: "identity-a-bootstrap-token-must-be-43-characters", Enabled: true}}},
		Peers: []PeerConfig{
			{ID: "mac", APIKey: "owner", IdentityGroupID: "identity-a"},
			{ID: "guest", PeerSecret: "share"},
		},
		Folders: []FolderConfig{{ID: "family"}, {ID: "shared", SharedWith: []string{"guest"}}},
	}
	kept, _ := MigrateExplicitShareACL(grouped)
	if len(kept.Folders[0].SharedWith) != 0 {
		t.Fatalf("identity folder listed share-only peers: %#v", kept.Folders[0].SharedWith)
	}
	if len(kept.Folders[1].SharedWith) != 1 || kept.Folders[1].SharedWith[0] != "guest" {
		t.Fatalf("existing share-only folder = %#v", kept.Folders[1].SharedWith)
	}
}
