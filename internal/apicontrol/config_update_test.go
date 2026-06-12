package apicontrol

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestHandleConfigUpdatePatchesOnlyNonSecretSettings(t *testing.T) {
	configPath := writeConfigUpdateConfig(t)
	listen := "127.0.0.1:9090"
	name := "node-b"

	response, err := HandleConfigUpdate(configPath, api.ConfigUpdateRequest{
		NodeName: &name,
		API:      &api.ConfigAPIUpdate{Listen: &listen},
		Logging:  &config.LoggingConfig{Level: config.LogLevelWarn, Output: "fse.log"},
		Transfer: &config.TransferConfig{SendBytesPerSecond: 1024, ReceiveBytesPerSecond: 2048},
	})
	if err != nil {
		t.Fatalf("config update: %v", err)
	}
	if response.Status != "accepted" || !strings.Contains(response.Message, "hot reload") {
		t.Fatalf("unexpected response: %+v", response)
	}

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeName != "node-b" || cfg.API.Listen != listen || cfg.Logging.Level != config.LogLevelWarn || cfg.Transfer.ReceiveBytesPerSecond != 2048 {
		t.Fatalf("non-secret settings not patched: %+v", cfg)
	}
	if cfg.API.Key != "secret-key" || cfg.Identity.PrivateKey != "identity-secret" || len(cfg.Peers) != 1 || cfg.Peers[0].APIKey != "peer-secret" {
		t.Fatalf("secret settings were not preserved: %+v", cfg)
	}
}

func TestHandleConfigUpdateWithStoreMirrorsLocalSettingsDocument(t *testing.T) {
	configPath := writeConfigUpdateConfig(t)
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	name := "node-b"
	listen := "127.0.0.1:9090"

	response, err := HandleConfigUpdateWithStore(configPath, store, api.ConfigUpdateRequest{
		NodeName: &name,
		API:      &api.ConfigAPIUpdate{Listen: &listen},
		Logging:  &config.LoggingConfig{Level: config.LogLevelWarn, Output: "fse.log"},
		Transfer: &config.TransferConfig{SendBytesPerSecond: 1024, ReceiveBytesPerSecond: 2048},
	})
	if err != nil {
		t.Fatalf("config update with store: %v", err)
	}
	if response.Status != "accepted" {
		t.Fatalf("unexpected response: %+v", response)
	}
	doc, ok, err := store.LoadNodeSettingsDocument("node-b")
	if err != nil || !ok {
		t.Fatalf("local settings document missing after config update: ok=%v err=%v", ok, err)
	}
	if doc.Source != "local-config" || doc.Settings["nodeName"] != "node-b" || doc.Settings["api.listen"] != listen || doc.Settings["logging.level"] != string(config.LogLevelWarn) || doc.Settings["transfer.receiveBytesPerSecond"] != float64(2048) {
		t.Fatalf("local settings document did not mirror non-secret patch: %+v", doc)
	}
	if _, exists := doc.Settings["api.key"]; exists {
		t.Fatalf("local settings document must not mirror API secrets: %+v", doc.Settings)
	}
	if doc.Revision == 0 || doc.UpdatedAt == "" {
		t.Fatalf("local settings document missing revision/timestamp: %+v", doc)
	}
}

func writeConfigUpdateConfig(t *testing.T) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		NodeName: "node-a",
		Listen:   []string{"tcp://127.0.0.1:22000"},
		API:      config.APIConfig{Listen: "127.0.0.1:0", Key: "secret-key"},
		Identity: config.IdentityConfig{PrivateKey: "identity-secret", PublicKey: "identity-public", EncryptionLevel: 4},
		Peers: []config.PeerConfig{{
			ID:     "peer-a",
			APIKey: "peer-secret",
			Endpoints: []config.EndpointConfig{{
				Kind:    "manual",
				Address: "http://127.0.0.1:22001",
			}},
		}},
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}
