package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureFileCreatesSkeletonWithGeneratedAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	cfg, created, err := EnsureFile(path)
	if err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}
	if !created {
		t.Fatalf("expected skeleton to be created")
	}
	if cfg.API.Key == "" {
		t.Fatalf("API key not generated")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "// folders define synchronized libraries") || !strings.Contains(text, cfg.API.Key) {
		t.Fatalf("skeleton lacks comments or persisted key:\n%s", text)
	}
	if !strings.Contains(text, `"permissions"`) || !strings.Contains(text, `"mode": "ignore"`) {
		t.Fatalf("skeleton lacks explicit permission policy:\n%s", text)
	}
	if !strings.Contains(text, `"dhtNamespace": "filesyncengine/v1"`) || !strings.Contains(text, `"dhtBootstrapPeers"`) || !strings.Contains(text, `"dht": true`) {
		t.Fatalf("skeleton lacks public DHT defaults:\n%s", text)
	}
	if strings.Contains(text, `"dht": false`) {
		t.Fatalf("fresh install must enable DHT:\n%s", text)
	}
	if !cfg.Discovery.DHT {
		t.Fatal("fresh install config has DHT disabled")
	}
	if strings.Contains(text, "peer-example") || strings.Contains(text, "192.0.2.10") || strings.Contains(text, `"./docs"`) {
		t.Fatalf("fresh install must not ship example peers or folders:\n%s", text)
	}
	if len(cfg.Peers) != 0 || len(cfg.Folders) != 0 {
		t.Fatalf("fresh install peers=%d folders=%d", len(cfg.Peers), len(cfg.Folders))
	}
}

func TestEnsureAPIKeyPersistsKeyWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	if err := os.WriteFile(path, []byte(`{"nodeName":"node-a","api":{"listen":"127.0.0.1:0"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, changed, err := EnsureAPIKey(path)
	if err != nil {
		t.Fatalf("EnsureAPIKey: %v", err)
	}
	if !changed || cfg.API.Key == "" {
		t.Fatalf("expected generated key, changed=%v cfg=%+v", changed, cfg.API)
	}
	reloaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.API.Key != cfg.API.Key {
		t.Fatalf("key not persisted")
	}
}

func TestLoadFileAcceptsCommentedJSONSkeleton(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	_, _, err := EnsureFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile should accept jsonc skeleton: %v", err)
	}
}
