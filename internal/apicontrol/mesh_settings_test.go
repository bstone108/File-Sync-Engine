package apicontrol

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/state"
)

func TestHandleMeshSettingsRedactsSecretFieldsForRequestedNode(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveNodeSettingsDocument("node-b", state.NodeSettingsDocument{
		NodeID:   "node-b",
		Revision: 7,
		Settings: map[string]any{
			"logging.level": "warn",
			"apiKey":        "secret-api-key",
			"nested": map[string]any{
				"privateToken": "nested-secret",
				"theme":        "dark",
			},
		},
		Source:      "identity-mesh-cache",
		ApplyStatus: "cached-read-only",
	}); err != nil {
		t.Fatalf("save node-b settings: %v", err)
	}
	if err := store.SaveNodeSettingsDocument("node-a", state.NodeSettingsDocument{NodeID: "node-a", Revision: 1, Settings: map[string]any{"logging.level": "info"}}); err != nil {
		t.Fatalf("save node-a settings: %v", err)
	}

	response, err := HandleMeshSettings(store, api.MeshSettingsRequest{NodeID: "node-b"})
	if err != nil {
		t.Fatalf("HandleMeshSettings: %v", err)
	}
	if len(response.Documents) != 1 || response.Documents[0].NodeID != "node-b" {
		t.Fatalf("unexpected filtered documents: %+v", response.Documents)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	body := string(encoded)
	for _, leaked := range []string{"secret-api-key", "nested-secret", "apiKey", "privateToken"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("mesh settings response leaked secret-looking value %q: %s", leaked, body)
		}
	}
	for _, want := range []string{"logging.level", "warn", "theme", "dark"} {
		if !strings.Contains(body, want) {
			t.Fatalf("mesh settings response missing non-secret value %q: %s", want, body)
		}
	}
}

func TestHandleMeshSettingsCommandPersistsPendingRemoteChange(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	response, err := HandleMeshSettingsCommand(store, "node-a", api.MeshSettingsCommandRequest{
		Action:         "queue",
		TargetNodeID:   "node-b",
		OriginNodeID:   "node-a",
		IdempotencyKey: "node-a:node-b:settings-1",
		SettingsPatch: map[string]any{
			"logging.level":                  "warn",
			"transfer.receiveBytesPerSecond": float64(2048),
		},
	})
	if err != nil {
		t.Fatalf("HandleMeshSettingsCommand: %v", err)
	}
	if response.Status != "queued" || response.TargetNodeID != "node-b" || response.OriginNodeID != "node-a" || response.ChangeID == "" {
		t.Fatalf("unexpected command response: %+v", response)
	}
	changes, err := store.ListPendingSettingsChanges("node-b")
	if err != nil {
		t.Fatalf("ListPendingSettingsChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one pending settings change, got %+v", changes)
	}
	change := changes[0]
	if change.ID != response.ChangeID || change.Status != "pending" || change.IdempotencyKey != "node-a:node-b:settings-1" || change.SettingsPatch["logging.level"] != "warn" {
		t.Fatalf("unexpected persisted change: %+v", change)
	}
}

func TestHandleMeshSettingsCommandRejectsSpoofedOriginAndSecrets(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	_, err := HandleMeshSettingsCommand(store, "node-a", api.MeshSettingsCommandRequest{
		Action:         "queue",
		TargetNodeID:   "node-b",
		OriginNodeID:   "node-c",
		IdempotencyKey: "node-c:node-b:settings-1",
		SettingsPatch:  map[string]any{"logging.level": "warn"},
	})
	if err == nil || !strings.Contains(err.Error(), "originNodeId must match authenticated local node") {
		t.Fatalf("expected spoofed origin rejection, got %v", err)
	}
	_, err = HandleMeshSettingsCommand(store, "node-a", api.MeshSettingsCommandRequest{
		Action:         "queue",
		TargetNodeID:   "node-b",
		OriginNodeID:   "node-a",
		IdempotencyKey: "node-a:node-b:settings-2",
		SettingsPatch:  map[string]any{"nested": map[string]any{"privateToken": "secret"}},
	})
	if err == nil || !strings.Contains(err.Error(), "secret-bearing") {
		t.Fatalf("expected secret-bearing patch rejection, got %v", err)
	}
	changes, err := store.ListPendingSettingsChanges("node-b")
	if err != nil {
		t.Fatalf("ListPendingSettingsChanges: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("rejected mesh commands queued changes: %+v", changes)
	}
}
