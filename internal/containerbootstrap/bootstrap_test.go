package containerbootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestApplyFirstRunDefaultsUsesContainerRuntimeValues(t *testing.T) {
	cfg := config.Config{
		NodeName:  "node-1",
		Listen:    []string{"tcp://0.0.0.0:22000"},
		API:       config.APIConfig{Listen: "127.0.0.1:22420", Key: "test-api-key"},
		Logging:   config.LoggingConfig{Level: config.LogLevelInfo, Output: "stderr"},
		WebGUI:    config.WebGUIConfig{Enabled: false, InstallDir: "./web/current", Listen: "127.0.0.1:8385"},
		Identity:  config.IdentityConfig{PrivateKey: "private", PublicKey: "public", EncryptionLevel: 4},
		Discovery: config.DiscoveryConfig{DHT: false, Local: true, DHTNamespace: "filesyncengine/v1"},
		Metadata:  config.MetadataConfig{Backend: config.MetadataBackendJSON},
	}

	updated := ApplyFirstRunDefaults(cfg, RuntimeDefaults{
		APIListen:         "0.0.0.0:22420",
		SyncListen:        "tcp://0.0.0.0:22001",
		LogLevel:          "warn",
		LogOutput:         "/config/logs/custom.jsonl",
		DiscoveryLocal:    false,
		DiscoveryDHT:      true,
		WebGUIEnabled:     true,
		WebGUIPackage:     "/opt/fse/web/default.zip",
		WebGUIInstallDir:  "/config/web/current",
		WebGUIListen:      "0.0.0.0:8385",
		WebGUITLSEnabled:  true,
		WebGUIHTTPSListen: "0.0.0.0:8943",
		WebGUIChecksum:    "abc123",
	})

	if updated.API.Listen != "0.0.0.0:22420" || updated.Listen[0] != "tcp://0.0.0.0:22001" {
		t.Fatalf("container listeners not applied: %+v", updated)
	}
	if updated.Logging.Level != config.LogLevelWarn || updated.Logging.Output != "/config/logs/custom.jsonl" {
		t.Fatalf("container logging not applied: %+v", updated.Logging)
	}
	if updated.Metadata.Backend != config.MetadataBackendBadger || updated.Metadata.Path != "/config/metadata" || !updated.Metadata.PerFolder {
		t.Fatalf("container metadata defaults not applied: %+v", updated.Metadata)
	}
	if updated.Discovery.Local || !updated.Discovery.DHT {
		t.Fatalf("container discovery defaults not applied: %+v", updated.Discovery)
	}
	if !updated.WebGUI.Enabled || updated.WebGUI.PackagePath != "/opt/fse/web/default.zip" || updated.WebGUI.HTTPSListen != "0.0.0.0:8943" || updated.WebGUI.ChecksumSHA256 != "abc123" {
		t.Fatalf("container web GUI defaults not applied: %+v", updated.WebGUI)
	}
}

func TestDefaultsFromEnvironmentKeepsCoreContainerHeadless(t *testing.T) {
	t.Setenv("FSE_WEB_GUI_ENABLED", "")
	t.Setenv("FSE_WEB_GUI_PACKAGE", "")
	t.Setenv("FSE_WEB_GUI_LISTEN", "")
	t.Setenv("FSE_WEB_GUI_TLS_ENABLED", "")
	t.Setenv("FSE_WEB_GUI_HTTPS_LISTEN", "")
	t.Setenv("FSE_WEB_GUI_CHECKSUM", "")

	defaults := DefaultsFromEnvironment()
	if defaults.WebGUIEnabled || defaults.WebGUIPackage != "" || defaults.WebGUIListen != "" || defaults.WebGUITLSEnabled || defaults.WebGUIHTTPSListen != "" || defaults.WebGUIChecksum != "" {
		t.Fatalf("core container web GUI defaults must be disabled and package-free: %+v", defaults)
	}
}

func TestExportIdentityPackageWritesAtomicallyAndPreservesExistingUnlessForced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	cfg := config.Config{
		NodeName: "node-1",
		Identity: config.IdentityConfig{
			PrivateKey: "private-secret",
			PublicKey:  "public-key",
			Groups:     []config.IdentityGroupConfig{{ID: "family", Token: "token-secret", Enabled: true}},
		},
	}

	written, err := ExportIdentityPackage(cfg, path, false)
	if err != nil || !written {
		t.Fatalf("ExportIdentityPackage first write = %v, %v", written, err)
	}
	mode := statMode(t, path)
	if mode != 0o600 {
		t.Fatalf("identity export mode = %#o, want 0600", mode)
	}
	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("identity export is not JSON: %v", err)
	}
	if got["nodeName"] != "node-1" || got["identityPrivateKey"] != "private-secret" || got["identityPublicKey"] != "public-key" {
		t.Fatalf("identity export missing expected fields: %s", data)
	}

	if err := os.WriteFile(path, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	written, err = ExportIdentityPackage(cfg, path, false)
	if err != nil || written {
		t.Fatalf("ExportIdentityPackage without force = %v, %v", written, err)
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "keep me\n" {
		t.Fatalf("identity export overwrote existing file without force: %q", kept)
	}
	written, err = ExportIdentityPackage(cfg, path, true)
	if err != nil || !written {
		t.Fatalf("ExportIdentityPackage force = %v, %v", written, err)
	}
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
