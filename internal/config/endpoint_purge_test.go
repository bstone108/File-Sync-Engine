package config

import (
	"os"
	"path/filepath"
	"testing"
)

func unverifiedPeerConfig() Config {
	return Config{NodeName: "kiki", Peers: []PeerConfig{{
		ID: "neko",
		Endpoints: []EndpointConfig{
			{Kind: "manual", Address: "https://192.168.1.206:22420"},
			{Kind: "manual", Address: "https://100.64.1.8:22420"},
			{Kind: "manual", Address: "https://[fd7a:115c:a1e0::1]:22420"},
			{Kind: "manual", Address: "https://files.example:22420"},
			{Kind: "manual", Address: "https://203.0.113.9:22420"},
			{Kind: "manual", Address: "https://[2001:db8::1]:22420"},
			{Kind: "manual", Address: "https://203.0.113.10:22420", Authenticated: true},
		},
		Relays: []string{
			"/ip4/10.200.0.1/tcp/4001/p2p/12D3KooWRelay/p2p-circuit",
			"https://198.51.100.8:22420",
			"https://relay.example:22420",
		},
	}}}
}

func TestPurgeUnverifiedEndpointsDropsPublicLiterals(t *testing.T) {
	cfg := unverifiedPeerConfig()
	got, changed := PurgeUnverifiedEndpoints(cfg)
	if !changed {
		t.Fatal("unverified public endpoints were kept")
	}
	var addresses []string
	for _, endpoint := range got.Peers[0].Endpoints {
		addresses = append(addresses, endpoint.Address)
	}
	want := []string{
		"https://192.168.1.206:22420",
		"https://100.64.1.8:22420",
		"https://[fd7a:115c:a1e0::1]:22420",
		"https://files.example:22420",
		"https://203.0.113.10:22420",
	}
	if len(addresses) != len(want) {
		t.Fatalf("endpoints = %v", addresses)
	}
	for i := range want {
		if addresses[i] != want[i] {
			t.Fatalf("endpoints = %v", addresses)
		}
	}
	if len(got.Peers[0].Relays) != 2 || got.Peers[0].Relays[0] != cfg.Peers[0].Relays[0] || got.Peers[0].Relays[1] != "https://relay.example:22420" {
		t.Fatalf("relays = %v", got.Peers[0].Relays)
	}

	path := filepath.Join(t.TempDir(), "node.json")
	stored, err := PersistUnverifiedEndpointPurge(path, unverifiedPeerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Peers[0].Endpoints) != len(want) {
		t.Fatalf("persisted endpoints = %+v", stored.Peers[0].Endpoints)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Peers[0].Endpoints) != len(want) {
		t.Fatalf("loaded endpoints = %+v", loaded.Peers[0].Endpoints)
	}
	again, err := PersistUnverifiedEndpointPurge(path, loaded)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Peers[0].Endpoints) != len(want) || info.Size() == 0 {
		t.Fatalf("second purge changed the file: %+v", again.Peers[0].Endpoints)
	}
}
