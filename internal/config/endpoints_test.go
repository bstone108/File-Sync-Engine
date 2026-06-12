package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAcceptsPeerEndpoints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"peers":[{"id":"node-b","endpoints":[{"kind":"pipe","address":"stdio"},{"kind":"relay","address":"relay://example/node-b"}]}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Peers[0].Endpoints) != 2 || cfg.Peers[0].Endpoints[0].Kind != "pipe" {
		t.Fatalf("endpoints not loaded: %+v", cfg.Peers[0].Endpoints)
	}
}

func TestLoadConfigRejectsInvalidPeerEndpointKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{"nodeName":"node-a","peers":[{"id":"node-b","endpoints":[{"kind":"bogus","address":"x"}]}]}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid endpoint kind error")
	}
}
