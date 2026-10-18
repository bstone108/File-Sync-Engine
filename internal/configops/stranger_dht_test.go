package configops

import (
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

func TestStrangerDHTAddressIsNotPersisted(t *testing.T) {
	discovery.ResetCatalog()
	t.Cleanup(discovery.ResetCatalog)
	const (
		ours     = "12D3KooWOursPeerID"
		stranger = "http://203.0.113.8:22420"
		lan      = "https://10.1.1.5:22420"
	)
	discovery.NoteDHTPeers([]discovery.Peer{{
		ID:        "12D3KooWStranger",
		Addresses: []string{stranger, "/ip4/203.0.113.8/tcp/4001/p2p/12D3KooWStranger"},
	}})
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := save(path, config.Config{
		NodeName: "neko",
		API:      config.APIConfig{Listen: "127.0.0.1:22420", Key: "local"},
		Identity: config.IdentityConfig{PublicKey: "local-public", PrivateKey: "local-private", EncryptionLevel: 4},
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertPairedPeer(path, config.PeerConfig{
		ID:                "ours",
		APIKey:            "remote-key",
		IdentityPublicKey: "remote-public",
		DHTPeerID:         ours,
		Endpoints: []config.EndpointConfig{
			{Kind: "manual", Address: lan, Authenticated: true},
			{Kind: "manual", Address: stranger, Authenticated: true},
			{Kind: "manual", Address: "/ip4/203.0.113.8/tcp/4001/p2p/12D3KooWStranger", Authenticated: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Peers) != 1 || len(loaded.Peers[0].Endpoints) != 1 || loaded.Peers[0].Endpoints[0].Address != lan {
		t.Fatalf("persisted endpoints = %+v", loaded.Peers)
	}
	for _, endpoint := range loaded.Peers[0].Endpoints {
		if strings.Contains(endpoint.Address, "203.0.113.8") {
			t.Fatalf("stranger address was persisted: %+v", endpoint)
		}
	}
}
