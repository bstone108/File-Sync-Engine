package apicontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configsync"
)

func apiLogging(level config.LogLevel) api.ConfigUpdateRequest {
	return api.ConfigUpdateRequest{Logging: &config.LoggingConfig{Level: level, Output: "stderr"}}
}

func writeSyncConfig(t *testing.T, name, level string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"nodeName":"` + name + `",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret","advertiseURL":"https://` + name + `:22420"},
		"identity":{"privateKey":"private-secret","publicKey":"public-` + name + `","encryptionLevel":4},
		"logging":{"level":"` + level + `","output":"stderr"},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func syncReplica(t *testing.T, configPath string) configsync.Replica {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "config-sync.json"))
	if err != nil {
		t.Fatal(err)
	}
	var replica configsync.Replica
	if err := json.Unmarshal(raw, &replica); err != nil {
		t.Fatal(err)
	}
	return replica
}

func loggingLevel(t *testing.T, configPath string) string {
	t.Helper()
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(cfg.Logging.Level)
}

func TestConfigSyncDoesNotAdoptAnotherInstancesSettings(t *testing.T) {
	originPath := writeSyncConfig(t, "engine-a", "warn")
	relayPath := writeSyncConfig(t, "engine-b", "info")
	targetPath := writeSyncConfig(t, "engine-c", "error")

	originView, err := HandleConfigSync(originPath)
	if err != nil {
		t.Fatal(err)
	}
	relayView, err := HandleConfigSync(relayPath)
	if err != nil {
		t.Fatal(err)
	}
	targetView, err := HandleConfigSync(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := configsync.ValidateInstanceID(originView.LocalInstanceID); err != nil {
		t.Fatal(err)
	}
	if originView.LocalInstanceID == relayView.LocalInstanceID || relayView.LocalInstanceID == targetView.LocalInstanceID {
		t.Fatalf("instance ids collided: %s %s %s", originView.LocalInstanceID, relayView.LocalInstanceID, targetView.LocalInstanceID)
	}

	queued, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"`+targetView.LocalInstanceID+`","path":"logging.level","value":"debug"}`))
	if err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, instance := range queued.Instances {
		if instance.InstanceID == targetView.LocalInstanceID {
			pending = instance.PendingChanges
			if instance.Online {
				t.Fatal("target was marked online before it published a connection")
			}
			if instance.Settings["logging.level"] != nil {
				t.Fatalf("origin stored the queued value in the target record: %#v", instance.Settings)
			}
		}
		if instance.Local && instance.Settings["logging.level"] != nil {
			t.Fatalf("origin own settings changed while editing the target: %#v", instance.Settings)
		}
	}
	if pending != 1 {
		t.Fatalf("origin pending count = %d view=%+v", pending, queued.Instances)
	}
	if loggingLevel(t, originPath) != "warn" {
		t.Fatalf("origin config logging = %s", loggingLevel(t, originPath))
	}

	raw, err := os.ReadFile(filepath.Join(filepath.Dir(originPath), "config-sync.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "api-secret") || strings.Contains(string(raw), "private-secret") {
		t.Fatal("config sync database stored a secret")
	}
	relayed, err := HandleConfigSyncMerge(relayPath, json.RawMessage(`{"replica":`+string(raw)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, relayPath) != "info" {
		t.Fatalf("relay adopted the queued logging level: %s", loggingLevel(t, relayPath))
	}
	relayReplica := syncReplica(t, relayPath)
	if relayReplica.OwnRecord().Fields["logging.level"].Value != nil && relayReplica.OwnRecord().Fields["logging.level"].Value != "" {
		t.Fatalf("relay own record = %#v", relayReplica.OwnRecord())
	}
	if _, ok := relayReplica.Records[targetView.LocalInstanceID]; ok {
		t.Fatalf("relay wrote the target record before the target applied the edit: %#v", relayReplica.Records[targetView.LocalInstanceID])
	}

	relayRaw, err := json.Marshal(map[string]any{"replica": relayed.Replica})
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := HandleConfigSyncMerge(targetPath, relayRaw)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.LocalInstanceID != targetView.LocalInstanceID {
		t.Fatalf("target local id = %s", delivered.LocalInstanceID)
	}
	if loggingLevel(t, targetPath) != "debug" {
		t.Fatalf("target did not apply its own queued edit, logging = %s", loggingLevel(t, targetPath))
	}
	if loggingLevel(t, originPath) != "warn" || loggingLevel(t, relayPath) != "info" {
		t.Fatalf("another engine's config changed: origin=%s relay=%s", loggingLevel(t, originPath), loggingLevel(t, relayPath))
	}
	targetReplica := syncReplica(t, targetPath)
	if targetReplica.OwnRecord().Fields["logging.level"].Value != "debug" {
		t.Fatalf("target own record = %#v", targetReplica.OwnRecord())
	}

	published, err := json.Marshal(map[string]any{"replica": delivered.Replica})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleConfigSyncMerge(relayPath, published); err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, relayPath) != "info" {
		t.Fatalf("relay adopted the target setting after it was published: %s", loggingLevel(t, relayPath))
	}
	relayReplica = syncReplica(t, relayPath)
	if relayReplica.Records[targetView.LocalInstanceID].Fields["logging.level"].Value != "debug" {
		t.Fatalf("relay did not keep the target record for its GUI: %#v", relayReplica.Records[targetView.LocalInstanceID])
	}
	if value, ok := relayReplica.OwnRecord().Fields["logging.level"]; ok && value.Value != nil {
		t.Fatalf("relay own record changed after storing the target: %#v", relayReplica.OwnRecord())
	}

	if _, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"`+targetView.LocalInstanceID+`","path":"apiKey","value":"nope"}`)); err == nil {
		t.Fatal("secret path was accepted")
	}
	if _, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"`+targetView.LocalInstanceID+`","path":"shared/logging.level","value":"debug"}`)); err == nil {
		t.Fatal("shared path was accepted")
	}
}

func TestLocalConfigUpdatePublishesOnlyTheLocalRecord(t *testing.T) {
	originPath := writeSyncConfig(t, "engine-a", "info")
	otherPath := writeSyncConfig(t, "engine-b", "warn")
	level := config.LogLevelDebug
	if _, err := HandleConfigUpdate(originPath, apiLogging(level)); err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, originPath) != "debug" {
		t.Fatalf("origin logging = %s", loggingLevel(t, originPath))
	}
	origin := syncReplica(t, originPath)
	if origin.OwnRecord().Fields["logging.level"].Value != "debug" {
		t.Fatalf("own record was not published: %#v", origin.OwnRecord())
	}
	raw, err := json.Marshal(map[string]any{"replica": origin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleConfigSyncMerge(otherPath, raw); err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, otherPath) != "warn" {
		t.Fatalf("engine B adopted engine A's logging level: %s", loggingLevel(t, otherPath))
	}
	other := syncReplica(t, otherPath)
	if other.Records[origin.InstanceID].Fields["logging.level"].Value != "debug" {
		t.Fatalf("engine B did not store engine A's record: %#v", other.Records[origin.InstanceID])
	}
	if value := other.OwnRecord().Fields["logging.level"].Value; value != nil && value != "" {
		t.Fatalf("engine B own record = %#v", other.OwnRecord())
	}
}
