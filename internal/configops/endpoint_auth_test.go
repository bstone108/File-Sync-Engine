package configops

import (
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestUpsertDropsEndpointsThatAreNotThePeer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := config.Config{
		NodeName: "neko",
		API:      config.APIConfig{Listen: "127.0.0.1:22420", Key: "local"},
		Identity: config.IdentityConfig{PublicKey: "local-public", PrivateKey: "local-private", EncryptionLevel: 4},
	}
	if err := save(path, cfg); err != nil {
		t.Fatal(err)
	}
	const peerID = "12D3KooWNeKoPeer"
	err := UpsertPairedPeer(path, config.PeerConfig{
		ID:                "neko-peer",
		APIKey:            "remote",
		IdentityPublicKey: "remote-public",
		DHTPeerID:         peerID,
		Endpoints: []config.EndpointConfig{
			{Kind: "manual", Address: "https://10.1.1.5:22420", Authenticated: true},
			{Kind: "manual", Address: "http://3.133.83.75:4001"},
			{Kind: "manual", Address: "/ip4/65.109.35.170/tcp/4001/p2p/12D3KooWOther"},
			{Kind: "manual", Address: "/ip4/10.2.3.24/tcp/4001/p2p/12D3KooWDHT"},
		},
		Relays: []string{
			"/ip4/10.1.1.1/tcp/4001/p2p/12D3KooWRelay/p2p-circuit",
			"/ip4/80.255.2.15/tcp/4001/p2p/12D3KooWOther/p2p-circuit/p2p/12D3KooWOther",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Peers) != 1 {
		t.Fatalf("peers = %+v", loaded.Peers)
	}
	peer := loaded.Peers[0]
	if len(peer.Endpoints) != 1 || peer.Endpoints[0].Address != "https://10.1.1.5:22420" {
		t.Fatalf("endpoints = %+v", peer.Endpoints)
	}
	if len(peer.Relays) != 1 || peer.Relays[0] != "/ip4/10.1.1.1/tcp/4001/p2p/12D3KooWRelay/p2p-circuit/p2p/"+peerID {
		t.Fatalf("relays = %+v", peer.Relays)
	}
}
