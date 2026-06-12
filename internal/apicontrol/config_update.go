package apicontrol

import (
	"encoding/json"
	"fmt"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

const configUpdateAcceptedMessage = "config update accepted; daemon hot reload will adopt the config change"

// HandleConfigUpdate applies non-secret config patch fields through the normal
// atomic config writer while preserving secret-bearing fields already on disk.
func HandleConfigUpdate(configPath string, req api.ConfigUpdateRequest) (api.ConfigUpdateResponse, error) {
	cfg, err := loadPatchedConfig(configPath, req)
	if err != nil {
		return api.ConfigUpdateResponse{}, err
	}
	if err := writePatchedConfig(configPath, cfg); err != nil {
		return api.ConfigUpdateResponse{}, err
	}
	return api.ConfigUpdateResponse{Status: "accepted", Message: configUpdateAcceptedMessage}, nil
}

// HandleConfigUpdateWithStore mirrors accepted local non-secret config patches
// into the local node settings document so identity-mesh clients can inspect
// and eventually queue changes without using the config file as cross-node
// source of truth.
func HandleConfigUpdateWithStore(configPath string, store state.JSONStore, req api.ConfigUpdateRequest) (api.ConfigUpdateResponse, error) {
	cfg, err := loadPatchedConfig(configPath, req)
	if err != nil {
		return api.ConfigUpdateResponse{}, err
	}
	if err := writePatchedConfig(configPath, cfg); err != nil {
		return api.ConfigUpdateResponse{}, err
	}
	if err := saveLocalSettingsDocument(store, cfg, req); err != nil {
		return api.ConfigUpdateResponse{}, err
	}
	return api.ConfigUpdateResponse{Status: "accepted", Message: configUpdateAcceptedMessage}, nil
}

func loadPatchedConfig(configPath string, req api.ConfigUpdateRequest) (config.Config, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return config.Config{}, err
	}
	if req.NodeName != nil {
		cfg.NodeName = *req.NodeName
	}
	if req.Listen != nil {
		cfg.Listen = append([]string(nil), req.Listen...)
	}
	if req.API != nil {
		if req.API.Listen != nil {
			cfg.API.Listen = *req.API.Listen
		}
		if req.API.Encryption != nil {
			cfg.API.Encryption = *req.API.Encryption
		}
	}
	if req.Logging != nil {
		cfg.Logging = *req.Logging
	}
	if req.Transfer != nil {
		cfg.Transfer = *req.Transfer
	}
	if req.Backup != nil {
		cfg.Backup = *req.Backup
	}
	if req.Discovery != nil {
		cfg.Discovery = *req.Discovery
	}
	if req.Metadata != nil {
		cfg.Metadata = *req.Metadata
	}
	if req.Maintenance != nil {
		cfg.Maintenance = *req.Maintenance
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func writePatchedConfig(configPath string, cfg config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(configPath, append(data, '\n'), 0o600)
}

func saveLocalSettingsDocument(store state.JSONStore, cfg config.Config, req api.ConfigUpdateRequest) error {
	nodeID := cfg.NodeName
	if nodeID == "" {
		return fmt.Errorf("nodeName is required to mirror local settings document")
	}
	now := time.Now().UTC()
	doc := state.NodeSettingsDocument{
		NodeID:      nodeID,
		Revision:    uint64(now.UnixNano()),
		UpdatedAt:   now.Format(time.RFC3339Nano),
		Source:      "local-config",
		ApplyStatus: "applied",
		Settings:    localSettingsDocumentPatch(req, cfg),
	}
	return store.SaveNodeSettingsDocument(nodeID, doc)
}

func localSettingsDocumentPatch(req api.ConfigUpdateRequest, cfg config.Config) map[string]any {
	settings := map[string]any{"nodeName": cfg.NodeName}
	if req.Listen != nil {
		settings["listen"] = append([]string(nil), cfg.Listen...)
	}
	if req.API != nil {
		if req.API.Listen != nil {
			settings["api.listen"] = cfg.API.Listen
		}
		if req.API.Encryption != nil {
			settings["api.encryption"] = cfg.API.Encryption
		}
	}
	if req.Logging != nil {
		settings["logging.level"] = string(cfg.Logging.Level)
		settings["logging.output"] = cfg.Logging.Output
	}
	if req.Transfer != nil {
		settings["transfer.sendBytesPerSecond"] = float64(cfg.Transfer.SendBytesPerSecond)
		settings["transfer.receiveBytesPerSecond"] = float64(cfg.Transfer.ReceiveBytesPerSecond)
	}
	if req.Backup != nil {
		settings["backup"] = cfg.Backup
	}
	if req.Discovery != nil {
		settings["discovery"] = cfg.Discovery
	}
	if req.Metadata != nil {
		settings["metadata"] = cfg.Metadata
	}
	if req.Maintenance != nil {
		settings["maintenance"] = cfg.Maintenance
	}
	return settings
}
