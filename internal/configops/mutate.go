package configops

import (
	"encoding/json"
	"fmt"
	"strings"

	"filesyncengine/internal/config"
)

func AddPeer(path, id, endpoint string) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("peer id is required")
	}
	kind, address, err := splitEndpoint(endpoint)
	if err != nil {
		return err
	}
	for _, peer := range cfg.Peers {
		if peer.ID == id {
			return fmt.Errorf("peer %q already exists", id)
		}
	}
	cfg.Peers = append(cfg.Peers, config.PeerConfig{ID: id, Endpoints: []config.EndpointConfig{{Kind: kind, Address: address}}})
	return save(path, cfg)
}

func AddFolder(path, id, folderPath, mode string) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	if mode == "" {
		mode = string(config.ModeSendReceive)
	}
	for _, folder := range cfg.Folders {
		if folder.ID == id {
			return fmt.Errorf("folder %q already exists", id)
		}
	}
	cfg.Folders = append(cfg.Folders, config.FolderConfig{ID: id, Path: folderPath, Mode: config.FolderMode(mode), BlockSize: config.DefaultBlockSize})
	if err := cfg.Validate(); err != nil {
		return err
	}
	return save(path, cfg)
}

func UpdatePeer(path, id, endpoint string) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	kind, address, err := splitEndpoint(endpoint)
	if err != nil {
		return err
	}
	for i := range cfg.Peers {
		if cfg.Peers[i].ID == id {
			cfg.Peers[i].Endpoints = []config.EndpointConfig{{Kind: kind, Address: address}}
			if err := cfg.Validate(); err != nil {
				return err
			}
			return save(path, cfg)
		}
	}
	return fmt.Errorf("peer %q not found", id)
}

func UpdateFolder(path, id, folderPath, mode string) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	for i := range cfg.Folders {
		if cfg.Folders[i].ID == id {
			if folderPath != "" {
				cfg.Folders[i].Path = folderPath
			}
			if mode != "" {
				cfg.Folders[i].Mode = config.FolderMode(mode)
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			return save(path, cfg)
		}
	}
	return fmt.Errorf("folder %q not found", id)
}

func UpdateDiscovery(path string, discovery config.DiscoveryConfig) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	cfg.Discovery = discovery
	if err := cfg.Validate(); err != nil {
		return err
	}
	return save(path, cfg)
}

func RemovePeer(path, id string) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	for i, peer := range cfg.Peers {
		if peer.ID == id {
			cfg.Peers = append(cfg.Peers[:i], cfg.Peers[i+1:]...)
			return save(path, cfg)
		}
	}
	return fmt.Errorf("peer %q not found", id)
}

func RemoveFolder(path, id string) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	for i, folder := range cfg.Folders {
		if folder.ID == id {
			cfg.Folders = append(cfg.Folders[:i], cfg.Folders[i+1:]...)
			return save(path, cfg)
		}
	}
	return fmt.Errorf("folder %q not found", id)
}

func splitEndpoint(endpoint string) (string, string, error) {
	parts := strings.SplitN(endpoint, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("endpoint must be kind:address")
	}
	return parts[0], parts[1], nil
}

func save(path string, cfg config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, append(data, '\n'), 0o600)
}
