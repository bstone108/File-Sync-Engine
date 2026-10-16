package apicontrol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"filesyncengine/internal/advertise"
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
	if err := recordOwnSettings(configPath, cfg); err != nil {
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
	if err := recordOwnSettings(configPath, cfg); err != nil {
		return api.ConfigUpdateResponse{}, err
	}
	return api.ConfigUpdateResponse{Status: "accepted", Message: configUpdateAcceptedMessage}, nil
}

func loadPatchedConfig(configPath string, req api.ConfigUpdateRequest) (config.Config, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return config.Config{}, err
	}
	if len(req.Raw) > 0 {
		if err := applyRawConfigPatch(&cfg, req.Raw); err != nil {
			return config.Config{}, err
		}
	} else if err := applyStructConfigPatch(&cfg, req); err != nil {
		return config.Config{}, err
	}
	if strings.TrimSpace(cfg.Logging.Output) == "" {
		cfg.Logging.Output = config.DefaultLogOutput()
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func applyStructConfigPatch(cfg *config.Config, req api.ConfigUpdateRequest) error {
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
		if req.API.AdvertiseURL != nil {
			if err := setAdvertiseURL(cfg, *req.API.AdvertiseURL); err != nil {
				return err
			}
		}
		if req.API.Encryption != nil {
			cfg.API.Encryption = *req.API.Encryption
		}
	}
	if req.Logging != nil {
		if req.Logging.Level != "" {
			cfg.Logging.Level = req.Logging.Level
		}
		if strings.TrimSpace(req.Logging.Output) != "" {
			cfg.Logging.Output = req.Logging.Output
		}
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
	return nil
}

func applyRawConfigPatch(cfg *config.Config, raw []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("invalid config update request")
	}
	if value, ok := root["nodeName"]; ok {
		if err := json.Unmarshal(value, &cfg.NodeName); err != nil {
			return err
		}
	}
	if value, ok := root["listen"]; ok {
		if err := json.Unmarshal(value, &cfg.Listen); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		key string
		dst any
	}{
		{"api", &cfg.API},
		{"logging", &cfg.Logging},
		{"transfer", &cfg.Transfer},
		{"backup", &cfg.Backup},
		{"discovery", &cfg.Discovery},
		{"metadata", &cfg.Metadata},
		{"maintenance", &cfg.Maintenance},
	} {
		value, ok := root[field.key]
		if !ok {
			continue
		}
		if err := mergeJSON(field.dst, value); err != nil {
			return err
		}
	}
	if _, ok := root["api"]; ok {
		return setAdvertiseURL(cfg, cfg.API.AdvertiseURL)
	}
	return nil
}

func setAdvertiseURL(cfg *config.Config, raw string) error {
	cfg.API.AdvertiseURL = strings.TrimSpace(raw)
	if cfg.API.AdvertiseURL == "" {
		return nil
	}
	normalized, err := advertise.Normalize(cfg.API.AdvertiseURL, *cfg)
	if err != nil {
		return err
	}
	if advertise.ContainerInternalURL(normalized) {
		return fmt.Errorf("advertise URL %s is a container-internal address; set %s to a host-reachable URL", normalized, advertise.EnvVar)
	}
	cfg.API.AdvertiseURL = normalized
	return nil
}

func mergeJSON(dst any, patch json.RawMessage) error {
	base, err := json.Marshal(dst)
	if err != nil {
		return err
	}
	var baseMap map[string]any
	if len(base) > 0 && string(base) != "null" {
		if err := json.Unmarshal(base, &baseMap); err != nil {
			return err
		}
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patch, &patchMap); err != nil {
		return err
	}
	encoded, err := json.Marshal(mergeMaps(baseMap, patchMap))
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, dst)
}

func mergeMaps(base, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range patch {
		child, ok := value.(map[string]any)
		if !ok {
			base[key] = value
			continue
		}
		existing, _ := base[key].(map[string]any)
		base[key] = mergeMaps(existing, child)
	}
	return base
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
		if req.API.AdvertiseURL != nil {
			settings["api.advertiseURL"] = cfg.API.AdvertiseURL
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
