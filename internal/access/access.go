package access

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"filesyncengine/internal/config"
)

// Role is who is calling an engine.
type Role string

const (
	// RoleOwner is the local GUI or another process that holds this engine's API key.
	RoleOwner Role = "owner"
	// RoleIdentity is a peer in the same enabled identity group. It may manage
	// the replicated identity config. It does not adopt another engine's config
	// as its own.
	RoleIdentity Role = "identity"
	// RoleShare is a computer paired only for a folder. It may sync that folder
	// and nothing else.
	RoleShare Role = "share"
)

// RoleFor reports how peer relates to the enabled identity groups on this engine.
// A peer with no matching group is share-only, even when this engine has no group.
func RoleFor(enabledGroupIDs []string, peerGroupID string) Role {
	peerGroupID = strings.TrimSpace(peerGroupID)
	if peerGroupID == "" {
		return RoleShare
	}
	for _, id := range enabledGroupIDs {
		if strings.TrimSpace(id) == peerGroupID {
			return RoleIdentity
		}
	}
	return RoleShare
}

// EnabledGroupIDs lists identity groups this engine will manage.
func EnabledGroupIDs(cfg config.Config) []string {
	var ids []string
	for _, group := range cfg.Identity.Groups {
		if group.Enabled && strings.TrimSpace(group.ID) != "" {
			ids = append(ids, group.ID)
		}
	}
	return ids
}

// MayManage is true for the local owner and for identity peers.
// A share-only peer cannot change config, folders, or options.
func MayManage(role Role) bool {
	return role == RoleOwner || role == RoleIdentity
}

// MaySeeConfig is the same boundary as management. Share-only peers do not
// receive this engine's config or its unshared folders.
func MaySeeConfig(role Role) bool {
	return MayManage(role)
}

// MaySyncFolder reports whether role may read or write one folder.
// Identity peers receive every folder of the identity. A share-only peer,
// including one paired before any identity group exists, receives a folder
// only when sharedWith names that peer.
func MaySyncFolder(role Role, sharedWith []string, peerID string) bool {
	if role == RoleOwner || role == RoleIdentity {
		return true
	}
	for _, id := range sharedWith {
		if strings.TrimSpace(id) != "" && id == peerID {
			return true
		}
	}
	return false
}

// GrantsIdentityTrust is false when the introducer is only a folder share.
// A peer learned from that introducer stays share-only until it proves it is
// in this identity itself.
func GrantsIdentityTrust(introducer Role) bool {
	return introducer == RoleOwner || introducer == RoleIdentity
}

// NewPeerSecret is the credential a share-only peer uses instead of this
// engine's API key.
func NewPeerSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
