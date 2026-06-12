package configops

import (
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestAddPeerPersistsEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	if _, _, err := config.EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if err := AddPeer(path, "peer-b", "pipe:stdio"); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, peer := range cfg.Peers {
		if peer.ID == "peer-b" {
			found = true
			if len(peer.Endpoints) != 1 || peer.Endpoints[0].Kind != "pipe" || peer.Endpoints[0].Address != "stdio" {
				t.Fatalf("bad endpoint: %+v", peer.Endpoints)
			}
		}
	}
	if !found {
		t.Fatalf("peer not added: %+v", cfg.Peers)
	}
}

func TestAddFolderPersistsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	if _, _, err := config.EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if err := AddFolder(path, "media", "/srv/media", "sendonly"); err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, folder := range cfg.Folders {
		if folder.ID == "media" {
			found = true
			if folder.Path != "/srv/media" || folder.Mode != config.ModeSendOnly {
				t.Fatalf("bad folder: %+v", folder)
			}
		}
	}
	if !found {
		t.Fatalf("folder not added: %+v", cfg.Folders)
	}
}

func TestUpdatePeerReplacesEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	if _, _, err := config.EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if err := AddPeer(path, "peer-b", "pipe:stdio"); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if err := UpdatePeer(path, "peer-b", "manual:http://127.0.0.1:8080"); err != nil {
		t.Fatalf("UpdatePeer: %v", err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range cfg.Peers {
		if peer.ID == "peer-b" {
			if len(peer.Endpoints) != 1 || peer.Endpoints[0].Kind != "manual" || peer.Endpoints[0].Address != "http://127.0.0.1:8080" {
				t.Fatalf("endpoint not updated: %+v", peer.Endpoints)
			}
			return
		}
	}
	t.Fatalf("peer missing after update: %+v", cfg.Peers)
}

func TestUpdateDiscoveryPersistsManualOnlyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	if _, _, err := config.EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	update := config.DiscoveryConfig{Disabled: true, DHT: false, Local: false, DHTNamespace: "fse-test", DHTBootstrapPeers: []string{"/dnsaddr/bootstrap.libp2p.io"}}
	if err := UpdateDiscovery(path, update); err != nil {
		t.Fatalf("UpdateDiscovery: %v", err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Discovery.Disabled || cfg.Discovery.DHT || cfg.Discovery.Local || cfg.Discovery.DHTNamespace != "fse-test" || len(cfg.Discovery.DHTBootstrapPeers) != 1 || cfg.Discovery.DHTBootstrapPeers[0] != "/dnsaddr/bootstrap.libp2p.io" {
		t.Fatalf("discovery config not updated: %+v", cfg.Discovery)
	}
}

func TestUpdateFolderReplacesPathAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	if _, _, err := config.EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if err := AddFolder(path, "media", "/srv/media", "sendonly"); err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	if err := UpdateFolder(path, "media", "/srv/media-new", "recvonly"); err != nil {
		t.Fatalf("UpdateFolder: %v", err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range cfg.Folders {
		if folder.ID == "media" {
			if folder.Path != "/srv/media-new" || folder.Mode != config.ModeReceiveOnly {
				t.Fatalf("folder not updated: %+v", folder)
			}
			return
		}
	}
	t.Fatalf("folder missing after update: %+v", cfg.Folders)
}
