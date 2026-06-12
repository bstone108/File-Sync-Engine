package metadatastore

import (
	"fmt"
	"path/filepath"
	"strings"

	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func Open(cfg config.Config, configPath string) (state.JSONStore, string, error) {
	backend := EffectiveBackend(cfg)
	path := ConfiguredStorePath(cfg, configPath)
	if cfg.Metadata.PerFolder {
		if backend != config.MetadataBackendBadger {
			return state.JSONStore{}, "", fmt.Errorf("metadata.perFolder requires metadata.backend to be badger")
		}
		paths := map[string]string{}
		for _, folder := range cfg.Folders {
			paths[folder.ID] = ConfiguredFolderStorePath(cfg, configPath, folder.ID)
		}
		store, err := state.NewPerFolderBadgerStore(paths)
		return store, path, err
	}
	switch backend {
	case config.MetadataBackendJSON:
		return state.NewJSONStore(path), path, nil
	case config.MetadataBackendBadger:
		store, err := state.NewBadgerStore(path)
		return store, path, err
	default:
		return state.JSONStore{}, "", fmt.Errorf("metadata backend %q is not supported", backend)
	}
}

func OpenFolder(cfg config.Config, configPath string, folderID string) (state.JSONStore, string, error) {
	if !cfg.Metadata.PerFolder {
		return Open(cfg, configPath)
	}
	if EffectiveBackend(cfg) != config.MetadataBackendBadger {
		return state.JSONStore{}, "", fmt.Errorf("metadata.perFolder requires metadata.backend to be badger")
	}
	path := ConfiguredFolderStorePath(cfg, configPath, folderID)
	store, err := state.NewBadgerStore(path)
	return store, path, err
}

type Identity struct {
	Backend config.MetadataBackend
	Path    string
}

func CurrentIdentity(cfg config.Config, configPath string) Identity {
	return Identity{Backend: EffectiveBackend(cfg), Path: ConfiguredStorePath(cfg, configPath)}
}

type ConfigLoader func(path string) (config.Config, error)

func CompactionStatePath(configPath string, load ConfigLoader) string {
	if cfg, err := load(configPath); err == nil {
		if statePath := ConfiguredStorePath(cfg, configPath); statePath != "" {
			return statePath
		}
	}
	return DefaultStatePath(configPath)
}

func ReloadNeeded(current Identity, next config.Config, configPath string) bool {
	nextIdentity := CurrentIdentity(next, configPath)
	return current.Backend != nextIdentity.Backend || current.Path != nextIdentity.Path
}

type Opener func(config.Config, string) (state.JSONStore, string, error)

func ReloadStore(current Identity, currentStore state.JSONStore, next config.Config, configPath string, open Opener) (state.JSONStore, string, Identity, bool, error) {
	if !ReloadNeeded(current, next, configPath) {
		return currentStore, "", current, false, nil
	}
	nextStore, openedPath, err := open(next, configPath)
	if err != nil {
		return currentStore, "", current, false, err
	}
	_ = currentStore.Close()
	return nextStore, openedPath, CurrentIdentity(next, configPath), true, nil
}

func EffectiveBackend(cfg config.Config) config.MetadataBackend {
	if cfg.Metadata.Backend == "" {
		return config.MetadataBackendJSON
	}
	return cfg.Metadata.Backend
}

func ConfiguredStorePath(cfg config.Config, configPath string) string {
	if cfg.Metadata.PerFolder {
		if cfg.Metadata.Path != "" {
			return resolvePathRelativeToConfig(cfg.Metadata.Path, configPath)
		}
		return configPath + ".state.badger.d"
	}
	if cfg.Metadata.Path != "" {
		return resolvePathRelativeToConfig(cfg.Metadata.Path, configPath)
	}
	if cfg.Metadata.Backend == config.MetadataBackendBadger {
		return configPath + ".state.badger"
	}
	return DefaultStatePath(configPath)
}

func ConfiguredFolderStorePath(cfg config.Config, configPath string, folderID string) string {
	if !cfg.Metadata.PerFolder {
		return ConfiguredStorePath(cfg, configPath)
	}
	return filepath.Join(ConfiguredStorePath(cfg, configPath), SanitizePathSegment(folderID)+".badger")
}

func DefaultStatePath(configPath string) string {
	return configPath + ".state.json"
}

func resolvePathRelativeToConfig(path string, configPath string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(filepath.Dir(configPath), path)
}

func SanitizePathSegment(value string) string {
	if value == "" {
		return "folder"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
		if allowed {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	if b.Len() == 0 {
		return "folder"
	}
	return b.String()
}
