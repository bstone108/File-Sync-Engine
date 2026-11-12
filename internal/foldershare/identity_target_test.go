package foldershare

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestIdentityPeerIsATargetOnlyAfterAddingTheFolder(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	body := []byte(`{
	  "groupId": "identity-a",
	  "instanceId": "11111111-1111-4111-8111-111111111111",
	  "connections": {
	    "22222222-2222-4222-8222-222222222222": {
	      "instanceId": "22222222-2222-4222-8222-222222222222",
	      "dhtPeerId": "12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU",
	      "discoveryId": "peer-public",
	      "addedFolders": ["shared"]
	    }
	  }
	}`)
	if err := os.WriteFile(filepath.Join(dir, "config-sync.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Identity: config.IdentityConfig{Groups: []config.IdentityGroupConfig{{ID: "identity-a", Enabled: true}}},
		Peers: []config.PeerConfig{{
			ID: "neko", APIKey: "secret", IdentityGroupID: "identity-a",
			DHTPeerID: "12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU",
		}},
	}
	added := peersFor(cfg, configPath, config.FolderConfig{ID: "shared", Path: "/data", Enabled: true, Mode: config.ModeSendReceive})
	if len(added) != 1 {
		t.Fatalf("added folder targets = %d", len(added))
	}
	missing := peersFor(cfg, configPath, config.FolderConfig{ID: "other", Path: "/data", Enabled: true, Mode: config.ModeSendReceive})
	if len(missing) != 0 {
		t.Fatalf("unadded folder targets = %d", len(missing))
	}
}
