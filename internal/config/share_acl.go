package config

import (
	"encoding/json"
	"strings"
)

// MigrateExplicitShareACL keeps shares that already work under the old rule
// that an empty sharedWith included every paired peer. Identity peers still
// receive an empty list. A share-only peer is written onto a folder only when
// that folder was actually open to every paired peer, which is an empty list
// on an engine with no enabled identity group. A later empty list stays empty.
func MigrateExplicitShareACL(cfg Config) (Config, bool) {
	if cfg.ShareACLVersion >= 1 {
		return cfg, false
	}
	cfg.ShareACLVersion = 1
	if !hasEnabledIdentityGroup(cfg) {
		peers := pairedPeerIDs(cfg)
		if len(peers) > 0 {
			for i := range cfg.Folders {
				if folderShareListEmpty(cfg.Folders[i].SharedWith) {
					cfg.Folders[i].SharedWith = append([]string(nil), peers...)
				}
			}
		}
	}
	return cfg, true
}

// PersistShareACL stores the one-time share migration.
func PersistShareACL(path string, cfg Config) (Config, error) {
	updated, changed := MigrateExplicitShareACL(cfg)
	if !changed {
		return cfg, nil
	}
	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return cfg, err
	}
	if err := WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return cfg, err
	}
	return updated, nil
}

func hasEnabledIdentityGroup(cfg Config) bool {
	for _, group := range cfg.Identity.Groups {
		if group.Enabled && strings.TrimSpace(group.ID) != "" {
			return true
		}
	}
	return false
}

func pairedPeerIDs(cfg Config) []string {
	ids := make([]string, 0, len(cfg.Peers))
	seen := map[string]struct{}{}
	for _, peer := range cfg.Peers {
		id := strings.TrimSpace(peer.ID)
		if id == "" || (strings.TrimSpace(peer.APIKey) == "" && strings.TrimSpace(peer.PeerSecret) == "") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func folderShareListEmpty(shared []string) bool {
	for _, id := range shared {
		if strings.TrimSpace(id) != "" {
			return false
		}
	}
	return true
}
