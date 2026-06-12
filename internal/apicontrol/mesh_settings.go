package apicontrol

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/state"
)

// HandleMeshSettings returns redacted cached mesh settings documents for
// authenticated GUI/API callers. Secret-looking settings keys are removed from
// every returned document before the response leaves the daemon boundary.
func HandleMeshSettings(store state.JSONStore, req api.MeshSettingsRequest) (api.MeshSettingsResponse, error) {
	if req.NodeID != "" {
		doc, ok, err := store.LoadNodeSettingsDocument(req.NodeID)
		if err != nil {
			return api.MeshSettingsResponse{}, err
		}
		if !ok {
			return api.MeshSettingsResponse{Documents: []state.NodeSettingsDocument{}}, nil
		}
		return api.MeshSettingsResponse{Documents: []state.NodeSettingsDocument{redactedNodeSettingsDocument(doc)}}, nil
	}
	docs, err := store.ListNodeSettingsDocuments()
	if err != nil {
		return api.MeshSettingsResponse{}, err
	}
	redacted := make([]state.NodeSettingsDocument, 0, len(docs))
	for _, doc := range docs {
		redacted = append(redacted, redactedNodeSettingsDocument(doc))
	}
	return api.MeshSettingsResponse{Documents: redacted}, nil
}

// HandleMeshSettingsCommand queues a non-secret remote settings change after
// validating that the origin matches the authenticated local node identity.
func HandleMeshSettingsCommand(store state.JSONStore, localNodeID string, req api.MeshSettingsCommandRequest) (api.MeshSettingsCommandResponse, error) {
	if req.Action != "queue" {
		return api.MeshSettingsCommandResponse{}, fmt.Errorf("unsupported mesh settings command action %q", req.Action)
	}
	localNodeID = strings.TrimSpace(localNodeID)
	if localNodeID == "" {
		return api.MeshSettingsCommandResponse{}, fmt.Errorf("local node identity is required to authorize mesh settings changes")
	}
	if req.TargetNodeID == "" {
		return api.MeshSettingsCommandResponse{}, fmt.Errorf("targetNodeId is required")
	}
	if req.OriginNodeID == "" {
		return api.MeshSettingsCommandResponse{}, fmt.Errorf("originNodeId is required")
	}
	if req.OriginNodeID != localNodeID {
		return api.MeshSettingsCommandResponse{}, fmt.Errorf("originNodeId must match authenticated local node %q", localNodeID)
	}
	if req.IdempotencyKey == "" {
		return api.MeshSettingsCommandResponse{}, fmt.Errorf("idempotencyKey is required")
	}
	if containsSecretSettingsKey(req.SettingsPatch) {
		return api.MeshSettingsCommandResponse{}, fmt.Errorf("settingsPatch cannot contain secret-bearing fields")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	changeID := fmt.Sprintf("settings-%x", sha256.Sum256([]byte(req.TargetNodeID+"\x00"+req.OriginNodeID+"\x00"+req.IdempotencyKey)))
	changeID = changeID[:25]
	change := state.PendingSettingsChange{
		ID:             changeID,
		TargetNodeID:   req.TargetNodeID,
		OriginNodeID:   req.OriginNodeID,
		IdempotencyKey: req.IdempotencyKey,
		Revision:       uint64(time.Now().UTC().UnixNano()),
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
		SettingsPatch:  req.SettingsPatch,
	}
	if change.SettingsPatch == nil {
		change.SettingsPatch = map[string]any{}
	}
	if err := store.SavePendingSettingsChange(req.TargetNodeID, change); err != nil {
		return api.MeshSettingsCommandResponse{}, err
	}
	return api.MeshSettingsCommandResponse{
		Action:         req.Action,
		Status:         "queued",
		ChangeID:       change.ID,
		TargetNodeID:   req.TargetNodeID,
		OriginNodeID:   req.OriginNodeID,
		IdempotencyKey: req.IdempotencyKey,
		Message:        "remote settings change queued for authenticated mesh delivery",
	}, nil
}

func containsSecretSettingsKey(settings map[string]any) bool {
	for key, value := range settings {
		if isSecretSettingsKey(key) {
			return true
		}
		switch typed := value.(type) {
		case map[string]any:
			if containsSecretSettingsKey(typed) {
				return true
			}
		case []any:
			for _, item := range typed {
				if nested, ok := item.(map[string]any); ok && containsSecretSettingsKey(nested) {
					return true
				}
			}
		}
	}
	return false
}

func redactedNodeSettingsDocument(doc state.NodeSettingsDocument) state.NodeSettingsDocument {
	doc.Settings = redactSettingsMap(doc.Settings)
	return doc
}

func redactSettingsMap(settings map[string]any) map[string]any {
	if len(settings) == 0 {
		return map[string]any{}
	}
	redacted := make(map[string]any, len(settings))
	for key, value := range settings {
		if isSecretSettingsKey(key) {
			continue
		}
		redacted[key] = redactSettingsValue(value)
	}
	return redacted
}

func redactSettingsValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactSettingsMap(typed)
	case []any:
		redacted := make([]any, 0, len(typed))
		for _, item := range typed {
			redacted = append(redacted, redactSettingsValue(item))
		}
		return redacted
	default:
		return value
	}
}

func isSecretSettingsKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range []string{"secret", "password", "token", "apikey", "api_key", "api-key", "privatekey", "private_key", "private-key", "credential"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
