package configops

import (
	"encoding/json"
	"fmt"
	"strings"

	"filesyncengine/internal/config"
	"filesyncengine/internal/peeridentity"
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
	cfg.Folders = append(cfg.Folders, config.FolderConfig{ID: id, Path: folderPath, Enabled: true, Mode: config.FolderMode(mode), BlockSize: config.DefaultBlockSize})
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

// AddFolderShared persists a folder that starts enabled and is optionally limited to peer IDs.
func AddFolderShared(path, id, folderPath, mode string, sharedWith []string) error {
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
	cfg.Folders = append(cfg.Folders, config.FolderConfig{
		ID: id, Path: folderPath, Enabled: true, Mode: config.FolderMode(mode), BlockSize: config.DefaultBlockSize,
		SharedWith: append([]string(nil), sharedWith...),
	})
	if err := cfg.Validate(); err != nil {
		return err
	}
	return save(path, cfg)
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

// UpdateFolderShared updates path, mode, and, when sharedWith is non-nil, the peer allow-list.
func UpdateFolderShared(path, id, folderPath, mode string, sharedWith *[]string) error {
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
			if sharedWith != nil {
				cfg.Folders[i].SharedWith = append([]string(nil), (*sharedWith)...)
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			return save(path, cfg)
		}
	}
	return fmt.Errorf("folder %q not found", id)
}

// PairedPeerID derives a stable peer id from the remote node name, falling back
// to a discovery-id prefix when two default nodes would otherwise collide.
func PairedPeerID(nodeName, discoveryID string) string {
	trimmed := strings.TrimSpace(nodeName)
	if trimmed == "" || trimmed == "node-CHANGE-ME" {
		prefix := sanitizePeerID(discoveryID)
		if len(prefix) > 12 {
			prefix = prefix[:12]
		}
		if prefix == "" {
			prefix = "peer"
		}
		return "peer-" + prefix
	}
	return sanitizePeerID(trimmed)
}

// UpsertPairedPeer stores the API key, certificate pin, and HTTPS endpoint learned
// during daemon-to-daemon pairing. An existing peer with the same identity is updated.
func UpsertPairedPeer(path string, peer config.PeerConfig) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	if peer.ID == "" {
		peer.ID = PairedPeerID(peer.ID, peer.IdentityPublicKey)
	}
	if peer.EncryptionLevel == 0 {
		peer.EncryptionLevel = peeridentity.DefaultEncryptionLevel
	}
	for i := range cfg.Peers {
		sameIdentity := peer.IdentityPublicKey != "" && cfg.Peers[i].IdentityPublicKey == peer.IdentityPublicKey
		sameID := cfg.Peers[i].ID == peer.ID && (cfg.Peers[i].IdentityPublicKey == "" || cfg.Peers[i].IdentityPublicKey == peer.IdentityPublicKey)
		if sameIdentity || sameID {
			cfg.Peers[i].APIKey = peer.APIKey
			cfg.Peers[i].APICertificateSHA256 = peer.APICertificateSHA256
			cfg.Peers[i].IdentityPublicKey = peer.IdentityPublicKey
			if peer.DHTPeerID != "" {
				cfg.Peers[i].DHTPeerID = peer.DHTPeerID
			}
			if len(peer.Relays) > 0 {
				cfg.Peers[i].Relays = peer.Relays
			}
			if len(peer.Endpoints) > 0 {
				cfg.Peers[i].Endpoints = peer.Endpoints
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			return save(path, cfg)
		}
	}
	for _, existing := range cfg.Peers {
		if existing.ID == peer.ID {
			peer.ID = peer.ID + "-" + sanitizePeerID(peer.IdentityPublicKey)
			if len(peer.ID) > 48 {
				peer.ID = peer.ID[:48]
			}
			break
		}
	}
	cfg.Peers = append(cfg.Peers, peer)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return save(path, cfg)
}

// MergePairedPeerLink adds newly observed addresses, relays, and the DHT
// identity without removing addresses that still work.
func MergePairedPeerLink(path, peerID string, peer config.PeerConfig) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	for i := range cfg.Peers {
		if cfg.Peers[i].ID != peerID && (peer.IdentityPublicKey == "" || cfg.Peers[i].IdentityPublicKey != peer.IdentityPublicKey) {
			continue
		}
		if peer.DHTPeerID != "" {
			cfg.Peers[i].DHTPeerID = peer.DHTPeerID
		}
		cfg.Peers[i].Relays = unionStrings(cfg.Peers[i].Relays, peer.Relays)
		seen := map[string]struct{}{}
		for _, endpoint := range cfg.Peers[i].Endpoints {
			seen[endpoint.Address] = struct{}{}
		}
		for _, endpoint := range peer.Endpoints {
			if endpoint.Address == "" {
				continue
			}
			if _, ok := seen[endpoint.Address]; ok {
				continue
			}
			seen[endpoint.Address] = struct{}{}
			cfg.Peers[i].Endpoints = append(cfg.Peers[i].Endpoints, endpoint)
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		return save(path, cfg)
	}
	return fmt.Errorf("peer %q not found", peerID)
}

func unionStrings(left, right []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string{}, left...), right...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sanitizePeerID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "peer"
	}
	return b.String()
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
