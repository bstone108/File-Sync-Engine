package apicontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSyncConfig(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"nodeName":"` + name + `",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret","advertiseURL":"https://` + name + `:22420"},
		"identity":{"privateKey":"private-secret","publicKey":"public-` + name + `","encryptionLevel":4},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigSyncEditReachesOfflineTargetThroughAnotherEngine(t *testing.T) {
	originPath := writeSyncConfig(t, "engine-a")
	relayPath := writeSyncConfig(t, "engine-b")
	targetPath := writeSyncConfig(t, "engine-c")

	queued, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"engine-c","path":"logging.level","value":"debug"}`))
	if err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, instance := range queued.Instances {
		if instance.InstanceID == "engine-c" {
			pending = instance.PendingChanges
			if instance.Online {
				t.Fatal("target was marked online before it published a connection")
			}
		}
	}
	if pending != 1 {
		t.Fatalf("origin pending count = %d view=%+v", pending, queued.Instances)
	}

	originView, err := HandleConfigSync(originPath)
	if err != nil {
		t.Fatal(err)
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
	relayRaw, err := json.Marshal(map[string]any{"replica": relayed.Replica})
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := HandleConfigSyncMerge(targetPath, relayRaw)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.LocalInstanceID != "engine-c" {
		t.Fatalf("target local id = %s", delivered.LocalInstanceID)
	}
	if _, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"engine-c","path":"apiKey","value":"nope"}`)); err == nil {
		t.Fatal("secret path was accepted")
	}
	_ = originView
}
