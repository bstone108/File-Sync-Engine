package advertise

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestResolveWithHostPrefersEnvironmentOverride(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420", AdvertiseURL: "https://saved.example:22420"}}
	got, err := ResolveWithHost(cfg, "192.0.2.20", "10.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.0.2.20:22420" {
		t.Fatalf("override = %q", got)
	}
}

func TestResolveWithHostKeepsSavedURL(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "127.0.0.1:22420", AdvertiseURL: "http://127.0.0.1:22420"}}
	got, err := ResolveWithHost(cfg, "", "192.0.2.20")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:22420" {
		t.Fatalf("saved = %q", got)
	}
}

func TestResolveWithHostUsesDetectedAddress(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22421"}}
	got, err := ResolveWithHost(cfg, "  ", "192.0.2.30")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.0.2.30:22421" {
		t.Fatalf("detected = %q", got)
	}
}

func TestResolveWithHostRequiresAnAddress(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420"}}
	if _, err := ResolveWithHost(cfg, "", " "); err == nil || !strings.Contains(err.Error(), "FSE_ADVERTISE_URL") {
		t.Fatalf("expected advertise URL guidance, got %v", err)
	}
}

func TestEnsurePersistsDetectedURLWithoutReplacingOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{"nodeName":"node-a","api":{"listen":"0.0.0.0:22420","key":"secret"},"peers":[],"folders":[]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, "https://files.example:22420")
	cfg, err := Ensure(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.API.AdvertiseURL != "https://files.example:22420" {
		t.Fatalf("persisted = %q", cfg.API.AdvertiseURL)
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.AdvertiseURL != cfg.API.AdvertiseURL || loaded.API.Key != "secret" {
		t.Fatalf("rewritten config = %+v", loaded.API)
	}
	if strings.Contains(string(mustRead(t, path)), "peer-example") || strings.Contains(string(mustRead(t, path)), `"./docs"`) {
		t.Fatalf("ensure reintroduced example peers or folders:\n%s", mustRead(t, path))
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
