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

func TestResolveWithHostSkipsDockerBridgeAddress(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:22420", AdvertiseURL: "https://172.17.0.4:22420"}}
	got, err := ResolveWithHost(cfg, "https://172.17.0.4:22420", "172.17.0.4")
	if err == nil || !strings.Contains(err.Error(), "FSE_ADVERTISE_URL") {
		t.Fatalf("bridge-only resolution = %q err=%v", got, err)
	}
	got, err = ResolveWithHost(cfg, "", "192.168.1.20")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.168.1.20:22420" {
		t.Fatalf("detected replacement = %q", got)
	}
	if ContainerInternalURL(got) || !ContainerInternalURL("https://172.17.0.4:22420") {
		t.Fatalf("container classification failed for %q", got)
	}
}

func TestResolveWithHostUsesAdvertiseURLListAndPublishedPort(t *testing.T) {
	t.Setenv(EnvVarList, "https://172.18.0.3:22420, https://100.64.1.9:22420 https://files.example:22420")
	cfg := config.Config{
		API: config.APIConfig{Listen: "0.0.0.0:22420"},
		Discovery: config.DiscoveryConfig{NetworkHints: config.NetworkHintsConfig{PublishedPortMappings: []config.PublishedPortMappingConfig{{
			HostIP: "192.168.1.20", HostPort: 22420, ContainerPort: 22420,
		}, {
			HostIP: "172.17.0.1", HostPort: 22420, ContainerPort: 22420,
		}}}},
	}
	got, err := ResolveWithHost(cfg, "", "172.17.0.4")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://100.64.1.9:22420" {
		t.Fatalf("list candidate = %q", got)
	}
	t.Setenv(EnvVarList, "")
	got, err = ResolveWithHost(cfg, "", "172.17.0.4")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.168.1.20:22420" {
		t.Fatalf("published host port = %q", got)
	}
	if SelectDetected([]string{"172.17.0.4", "100.70.0.4", "10.1.1.5", "203.0.113.8"}) != "10.1.1.5" {
		t.Fatal("LAN should win over Tailscale, public, and bridge addresses")
	}
	if SelectDetected([]string{"172.31.255.4", "100.64.0.8"}) != "100.64.0.8" {
		t.Fatal("Tailscale should win when the only other address is a bridge IP")
	}
}

func TestEnsureClearsStoredBridgeURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{"nodeName":"node-a","api":{"listen":"0.0.0.0:22420","key":"secret","advertiseURL":"https://172.17.0.4:22420"},"peers":[],"folders":[]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, "")
	t.Setenv(EnvVarList, "https://files.example:22420")
	cfg, err := Ensure(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.API.AdvertiseURL != "https://files.example:22420" {
		t.Fatalf("persisted = %q", cfg.API.AdvertiseURL)
	}
	if strings.Contains(string(mustRead(t, path)), "172.17.0.4") {
		t.Fatalf("bridge URL remained on disk:\n%s", mustRead(t, path))
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
