package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/peeridentity"
)

const IdentityPackageVersion = "fse-identity-package-v1"
const IdentityBootstrapEncryptionLevel = 10
const DefaultPeerPairEncryptionLevel = 4
const DefaultPeerPairRotationInterval = 90 * 24 * time.Hour

// IdentityPackage is the authenticated export/import payload GUI clients can
// copy, save, or encode visually for same-identity pairing. It intentionally
// contains the same-identity bootstrap proof key, but not daemon API keys or the
// node's private signing key.
type IdentityPackage struct {
	Version                    string    `json:"version"`
	CreatedAt                  time.Time `json:"createdAt"`
	NodeName                   string    `json:"nodeName"`
	DiscoveryID                string    `json:"discoveryId"`
	GroupID                    string    `json:"groupId"`
	BootstrapProofKey          string    `json:"bootstrapProofKey"`
	BootstrapEncryptionLevel   int       `json:"bootstrapEncryptionLevel"`
	DefaultPeerEncryptionLevel int       `json:"defaultPeerEncryptionLevel"`
}

func BuildIdentityPackage(cfg config.Config, groupID string) (IdentityPackage, error) {
	if cfg.Identity.PublicKey == "" {
		return IdentityPackage{}, fmt.Errorf("identity public key is required to build an identity package")
	}
	group, ok := findEnabledIdentityGroup(cfg.Identity.Groups, groupID)
	if !ok {
		return IdentityPackage{}, fmt.Errorf("enabled identity group %q not found", groupID)
	}
	peerLevel := cfg.Identity.EncryptionLevel
	if peerLevel == 0 {
		peerLevel = DefaultPeerPairEncryptionLevel
	}
	return IdentityPackage{
		Version:                    IdentityPackageVersion,
		CreatedAt:                  time.Now().UTC(),
		NodeName:                   cfg.NodeName,
		DiscoveryID:                cfg.Identity.PublicKey,
		GroupID:                    group.ID,
		BootstrapProofKey:          group.Token,
		BootstrapEncryptionLevel:   IdentityBootstrapEncryptionLevel,
		DefaultPeerEncryptionLevel: peerLevel,
	}, nil
}

func MarshalIdentityPackage(pkg IdentityPackage) ([]byte, error) {
	return json.MarshalIndent(pkg, "", "  ")
}

// IdentityBootstrapPlan is the import-side contract for using an identity
// package. The bootstrap proof key is accepted only as same-identity
// introduction/authentication material; ordinary peer traffic must switch to a
// negotiated dedicated peer-pair key before synchronization/control messages are
// authorized on behalf of the imported identity.
type IdentityBootstrapPlan struct {
	LocalDiscoveryID                            string
	RemoteDiscoveryID                           string
	GroupID                                     string
	IntroductionEncryptionLevel                 int
	IntroductionProfile                         peeridentity.EncryptionProfileSpec
	PrioritizesBootstrapSecurityOverPerformance bool
	PeerPairEncryptionLevel                     int
	RequiresDedicatedPeerPairKey                bool
	UsesBootstrapKeyForTraffic                  bool
}

// PeerPairKeyMaterial is the prototype output of a successful identity
// bootstrap/key-negotiation step. The bootstrap proof key is not reused for
// ordinary traffic; each peer pair receives fresh dedicated key material at the
// highest encryption level either side requested.
type PeerPairKeyMaterial struct {
	PairID            string
	LocalDiscoveryID  string
	RemoteDiscoveryID string
	EncryptionLevel   int
	KeyID             string
	SecretKey         string
	CreatedAt         time.Time
}

// PeerPairLevelState records the last advertised ordinary traffic encryption
// levels for a peer pair. A change on either side requires a protected rekey so
// both peers can switch to key material matching the newly negotiated highest
// level.
type PeerPairLevelState struct {
	LocalLevel  int
	RemoteLevel int
}

// PeerPairRekeyPlan describes how a peer pair should replace traffic keys. The
// new key is exchanged over the current encrypted channel, then activated before
// the previous key is revoked.
type PeerPairRekeyPlan struct {
	RotationRequired              bool
	Reason                        string
	LevelChangeDirection          string
	PreviousHighestLevel          int
	NextHighestLevel              int
	PreviousKeyID                 string
	NegotiationProtectedByKeyID   string
	ActivateAfterExchange         bool
	RevokePreviousAfterActivation bool
	PreviousLevels                PeerPairLevelState
	NextLevels                    PeerPairLevelState
	Next                          PeerPairKeyMaterial
	AuditEvents                   []PeerPairKeyAuditEvent
}

// PeerPairKeyAuditEvent is a redacted status/audit record for prototype key
// lifecycle changes. It intentionally carries key identifiers and levels only,
// never secret key material or identity bootstrap proof keys.
type PeerPairKeyAuditEvent struct {
	Event                string
	Reason               string
	Direction            string
	PreviousHighestLevel int
	NextHighestLevel     int
	PreviousKeyID        string
	NextKeyID            string
}

// BootstrapKeyLifecyclePlan keeps the shared identity bootstrap key separate
// from ordinary peer-pair traffic-key rotation. The bootstrap key is regenerated
// only through an explicit user-facing safety flow; automatic rotation applies
// to dedicated peer-pair traffic keys, not to the shared identity secret.
type BootstrapKeyLifecyclePlan struct {
	CreatedAt                     time.Time
	CheckedAt                     time.Time
	RegenerationMode              string
	AutomaticRotationAllowed      bool
	RotationRequired              bool
	RequiresUserSafetyFlow        bool
	AppliesPeerPairRotationPolicy bool
}

// PeerPairKeyState is the post-activation view of a peer pair's traffic-key
// lifecycle. Only Active is usable for new traffic; revoked key IDs are retained
// as audit/status evidence and must not be authorized again.
type PeerPairKeyState struct {
	Active              PeerPairKeyMaterial
	RevokedKeyIDs       []string
	LastChangeReason    string
	LastChangeDirection string
	AuditEvents         []PeerPairKeyAuditEvent
}

// IdentityRevocationPlan describes the conservative first step of identity
// compromise recovery. It is a non-secret, non-mutating plan: callers can send
// final revocation notices to reachable identity-derived peers, disconnect only
// identity-derived relationships, and require a fresh manually imported identity
// before trust can be re-established.
type IdentityRevocationPlan struct {
	GroupID                           string
	GlobalRevocation                  bool
	BreaksIdentityTrust               bool
	FinalRevocationPeerIDs            []string
	DisconnectPeerIDs                 []string
	DisableFolderIDs                  []string
	PreservePeerIDs                   []string
	PreserveFolderIDs                 []string
	RevokeBootstrapKeyAutomatically   bool
	RequiresNewIdentityForReconnect   bool
	RequiresManualSharingForReconnect bool
	AutoRejoinAllowed                 bool
	RevokedIdentityMaterialReusable   bool
	ReconnectAction                   string
}

func (s PeerPairKeyState) IsKeyAuthorized(keyID string) bool {
	return keyID != "" && s.Active.KeyID == keyID && !s.IsKeyRevoked(keyID)
}

func (s PeerPairKeyState) IsKeyRevoked(keyID string) bool {
	for _, revoked := range s.RevokedKeyIDs {
		if revoked == keyID {
			return true
		}
	}
	return false
}

func PlanIdentityBootstrap(cfg config.Config, pkg IdentityPackage) (IdentityBootstrapPlan, error) {
	if pkg.Version != IdentityPackageVersion {
		return IdentityBootstrapPlan{}, fmt.Errorf("unsupported identity package version %q", pkg.Version)
	}
	if cfg.Identity.PublicKey == "" {
		return IdentityBootstrapPlan{}, fmt.Errorf("local identity public key is required to import an identity package")
	}
	if pkg.DiscoveryID == "" {
		return IdentityBootstrapPlan{}, fmt.Errorf("identity package discovery ID is required")
	}
	if pkg.DiscoveryID == cfg.Identity.PublicKey {
		return IdentityBootstrapPlan{}, fmt.Errorf("identity package points at the local identity")
	}
	if IdentityPackageRevoked(cfg.Identity.Revoked, pkg) {
		return IdentityBootstrapPlan{}, fmt.Errorf("identity package for group %q and discovery ID %q was revoked", pkg.GroupID, pkg.DiscoveryID)
	}
	if pkg.BootstrapEncryptionLevel < IdentityBootstrapEncryptionLevel {
		return IdentityBootstrapPlan{}, fmt.Errorf("identity bootstrap encryption level must be %d", IdentityBootstrapEncryptionLevel)
	}
	if err := peeridentity.ValidateEncryptionLevel(pkg.BootstrapEncryptionLevel); err != nil {
		return IdentityBootstrapPlan{}, err
	}
	if err := peeridentity.ValidateEncryptionLevel(pkg.DefaultPeerEncryptionLevel); err != nil {
		return IdentityBootstrapPlan{}, err
	}
	localPeerLevel := cfg.Identity.EncryptionLevel
	if localPeerLevel == 0 {
		localPeerLevel = DefaultPeerPairEncryptionLevel
	}
	if err := peeridentity.ValidateEncryptionLevel(localPeerLevel); err != nil {
		return IdentityBootstrapPlan{}, err
	}
	group, ok := findEnabledIdentityGroup(cfg.Identity.Groups, pkg.GroupID)
	if !ok {
		return IdentityBootstrapPlan{}, fmt.Errorf("enabled identity group %q not found", pkg.GroupID)
	}
	if subtle.ConstantTimeCompare([]byte(group.Token), []byte(pkg.BootstrapProofKey)) != 1 {
		return IdentityBootstrapPlan{}, fmt.Errorf("identity bootstrap proof does not match group %q", pkg.GroupID)
	}
	profile, err := peeridentity.EncryptionProfile(IdentityBootstrapEncryptionLevel)
	if err != nil {
		return IdentityBootstrapPlan{}, err
	}
	return IdentityBootstrapPlan{
		LocalDiscoveryID:            cfg.Identity.PublicKey,
		RemoteDiscoveryID:           pkg.DiscoveryID,
		GroupID:                     pkg.GroupID,
		IntroductionEncryptionLevel: IdentityBootstrapEncryptionLevel,
		IntroductionProfile:         profile,
		PrioritizesBootstrapSecurityOverPerformance: true,
		PeerPairEncryptionLevel:                     maxLevel(localPeerLevel, pkg.DefaultPeerEncryptionLevel),
		RequiresDedicatedPeerPairKey:                true,
		UsesBootstrapKeyForTraffic:                  false,
	}, nil
}

func NewPeerPairKeyMaterial(localDiscoveryID, remoteDiscoveryID string, localLevel, remoteLevel int, bootstrapProofKey string) (PeerPairKeyMaterial, error) {
	if localDiscoveryID == "" {
		return PeerPairKeyMaterial{}, fmt.Errorf("local discovery ID is required")
	}
	if remoteDiscoveryID == "" {
		return PeerPairKeyMaterial{}, fmt.Errorf("remote discovery ID is required")
	}
	if localDiscoveryID == remoteDiscoveryID {
		return PeerPairKeyMaterial{}, fmt.Errorf("peer-pair key material requires two different identities")
	}
	if err := peeridentity.ValidateEncryptionLevel(localLevel); err != nil {
		return PeerPairKeyMaterial{}, err
	}
	if err := peeridentity.ValidateEncryptionLevel(remoteLevel); err != nil {
		return PeerPairKeyMaterial{}, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return PeerPairKeyMaterial{}, fmt.Errorf("generate peer-pair key material: %w", err)
	}
	keyIDBytes := make([]byte, 16)
	if _, err := rand.Read(keyIDBytes); err != nil {
		return PeerPairKeyMaterial{}, fmt.Errorf("generate peer-pair key id: %w", err)
	}
	secretKey := base64.RawURLEncoding.EncodeToString(secret)
	if bootstrapProofKey != "" && subtle.ConstantTimeCompare([]byte(secretKey), []byte(bootstrapProofKey)) == 1 {
		return PeerPairKeyMaterial{}, fmt.Errorf("generated peer-pair key unexpectedly matched bootstrap proof key")
	}
	pairID := stablePairID(localDiscoveryID, remoteDiscoveryID)
	level := maxLevel(localLevel, remoteLevel)
	return PeerPairKeyMaterial{
		PairID:            pairID,
		LocalDiscoveryID:  localDiscoveryID,
		RemoteDiscoveryID: remoteDiscoveryID,
		EncryptionLevel:   level,
		KeyID:             base64.RawURLEncoding.EncodeToString(keyIDBytes),
		SecretKey:         secretKey,
		CreatedAt:         time.Now().UTC(),
	}, nil
}

func PlanPeerPairRekey(current PeerPairKeyMaterial, previous, next PeerPairLevelState, bootstrapProofKey string) (PeerPairRekeyPlan, error) {
	if err := validatePeerPairRotationInputs(current, previous, next); err != nil {
		return PeerPairRekeyPlan{}, err
	}
	if previous.LocalLevel == next.LocalLevel && previous.RemoteLevel == next.RemoteLevel {
		return keepCurrentPeerPairKey(current, previous, next), nil
	}
	replacement, err := NewPeerPairKeyMaterial(current.LocalDiscoveryID, current.RemoteDiscoveryID, next.LocalLevel, next.RemoteLevel, bootstrapProofKey)
	if err != nil {
		return PeerPairRekeyPlan{}, err
	}
	return peerPairReplacementPlan(current, previous, next, replacement, "encryption-level-change"), nil
}

func PlanPeerPairScheduledRotation(current PeerPairKeyMaterial, levels PeerPairLevelState, now time.Time, bootstrapProofKey string) (PeerPairRekeyPlan, error) {
	if err := validatePeerPairRotationInputs(current, levels, levels); err != nil {
		return PeerPairRekeyPlan{}, err
	}
	if current.CreatedAt.IsZero() || now.Before(current.CreatedAt.Add(DefaultPeerPairRotationInterval)) {
		return keepCurrentPeerPairKey(current, levels, levels), nil
	}
	replacement, err := NewPeerPairKeyMaterial(current.LocalDiscoveryID, current.RemoteDiscoveryID, levels.LocalLevel, levels.RemoteLevel, bootstrapProofKey)
	if err != nil {
		return PeerPairRekeyPlan{}, err
	}
	return peerPairReplacementPlan(current, levels, levels, replacement, "scheduled-rotation"), nil
}

func PlanBootstrapKeyLifecycle(createdAt, checkedAt time.Time) BootstrapKeyLifecyclePlan {
	return BootstrapKeyLifecyclePlan{
		CreatedAt:                     createdAt,
		CheckedAt:                     checkedAt,
		RegenerationMode:              "manual-only",
		AutomaticRotationAllowed:      false,
		RotationRequired:              false,
		RequiresUserSafetyFlow:        true,
		AppliesPeerPairRotationPolicy: false,
	}
}

func PlanIdentityRevocation(cfg config.Config, groupID string, reachablePeerIDs []string) (IdentityRevocationPlan, error) {
	if groupID == "" {
		return IdentityRevocationPlan{}, fmt.Errorf("identity group ID is required for revocation")
	}
	if _, ok := findIdentityGroup(cfg.Identity.Groups, groupID); !ok {
		return IdentityRevocationPlan{}, fmt.Errorf("identity group %q not found", groupID)
	}

	identityPeers := map[string]struct{}{}
	plan := IdentityRevocationPlan{
		GroupID:                           groupID,
		GlobalRevocation:                  true,
		BreaksIdentityTrust:               true,
		RequiresNewIdentityForReconnect:   true,
		RequiresManualSharingForReconnect: true,
		AutoRejoinAllowed:                 false,
		RevokedIdentityMaterialReusable:   false,
		ReconnectAction:                   "generate-new-identity-and-manual-import",
	}
	for _, peer := range cfg.Peers {
		if peer.IdentityPublicKey != "" {
			identityPeers[peer.ID] = struct{}{}
			plan.DisconnectPeerIDs = append(plan.DisconnectPeerIDs, peer.ID)
			continue
		}
		plan.PreservePeerIDs = append(plan.PreservePeerIDs, peer.ID)
	}
	for _, folder := range cfg.Folders {
		if folder.IdentityGroup == groupID && folder.AdvertisedBy != "" && !folder.Enabled {
			plan.DisableFolderIDs = append(plan.DisableFolderIDs, folder.ID)
			continue
		}
		plan.PreserveFolderIDs = append(plan.PreserveFolderIDs, folder.ID)
	}

	seenReachable := map[string]struct{}{}
	for _, peerID := range reachablePeerIDs {
		if _, ok := identityPeers[peerID]; !ok {
			continue
		}
		if _, duplicate := seenReachable[peerID]; duplicate {
			continue
		}
		seenReachable[peerID] = struct{}{}
		plan.FinalRevocationPeerIDs = append(plan.FinalRevocationPeerIDs, peerID)
	}
	sort.Strings(plan.FinalRevocationPeerIDs)
	sort.Strings(plan.DisconnectPeerIDs)
	sort.Strings(plan.DisableFolderIDs)
	sort.Strings(plan.PreservePeerIDs)
	sort.Strings(plan.PreserveFolderIDs)
	return plan, nil
}

func RecordIdentityRevocation(groupID, discoveryID, bootstrapProofKey string, revokedAt time.Time) config.RevokedIdentityConfig {
	return config.RevokedIdentityConfig{
		GroupID:               groupID,
		DiscoveryID:           discoveryID,
		BootstrapProofKeyHash: HashIdentityBootstrapProof(bootstrapProofKey),
		RevokedAt:             revokedAt.UTC(),
	}
}

func IdentityPackageRevoked(records []config.RevokedIdentityConfig, pkg IdentityPackage) bool {
	proofHash := HashIdentityBootstrapProof(pkg.BootstrapProofKey)
	for _, record := range records {
		if record.GroupID != pkg.GroupID {
			continue
		}
		if record.DiscoveryID != "" && record.DiscoveryID != pkg.DiscoveryID {
			continue
		}
		if record.BootstrapProofKeyHash == "" || proofHash == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(record.BootstrapProofKeyHash), []byte(proofHash)) == 1 {
			return true
		}
	}
	return false
}

func HashIdentityBootstrapProof(proof string) string {
	if proof == "" {
		return ""
	}
	digest := sha256.Sum256([]byte("fse-identity-bootstrap-proof-v1\n" + proof))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func ActivatePeerPairRekey(current PeerPairKeyMaterial, plan PeerPairRekeyPlan, exchangedKeyID string) (PeerPairKeyState, error) {
	if current.KeyID == "" || current.SecretKey == "" || current.PairID == "" {
		return PeerPairKeyState{}, fmt.Errorf("current peer-pair key material is required")
	}
	if !plan.RotationRequired {
		return PeerPairKeyState{Active: current}, nil
	}
	if plan.PreviousKeyID != current.KeyID || plan.NegotiationProtectedByKeyID != current.KeyID {
		return PeerPairKeyState{}, fmt.Errorf("rekey plan does not match current peer-pair key")
	}
	if !plan.ActivateAfterExchange || !plan.RevokePreviousAfterActivation {
		return PeerPairKeyState{}, fmt.Errorf("rekey plan must activate replacement before revoking previous key")
	}
	if plan.Next.KeyID == "" || plan.Next.SecretKey == "" || plan.Next.PairID != current.PairID {
		return PeerPairKeyState{}, fmt.Errorf("replacement peer-pair key material is invalid")
	}
	if exchangedKeyID != plan.Next.KeyID {
		return PeerPairKeyState{}, fmt.Errorf("replacement peer-pair key was not exchanged")
	}
	activation := PeerPairKeyAuditEvent{
		Event:                "peer_pair_key_rekey_activated",
		Reason:               plan.Reason,
		Direction:            plan.LevelChangeDirection,
		PreviousHighestLevel: plan.PreviousHighestLevel,
		NextHighestLevel:     plan.NextHighestLevel,
		PreviousKeyID:        current.KeyID,
		NextKeyID:            plan.Next.KeyID,
	}
	auditEvents := append([]PeerPairKeyAuditEvent(nil), plan.AuditEvents...)
	auditEvents = append(auditEvents, activation)
	return PeerPairKeyState{
		Active:              plan.Next,
		RevokedKeyIDs:       []string{current.KeyID},
		LastChangeReason:    plan.Reason,
		LastChangeDirection: plan.LevelChangeDirection,
		AuditEvents:         auditEvents,
	}, nil
}

func validatePeerPairRotationInputs(current PeerPairKeyMaterial, previous, next PeerPairLevelState) error {
	if current.KeyID == "" || current.SecretKey == "" || current.PairID == "" {
		return fmt.Errorf("current peer-pair key material is required")
	}
	for _, level := range []int{previous.LocalLevel, previous.RemoteLevel, next.LocalLevel, next.RemoteLevel} {
		if err := peeridentity.ValidateEncryptionLevel(level); err != nil {
			return err
		}
	}
	return nil
}

func keepCurrentPeerPairKey(current PeerPairKeyMaterial, previous, next PeerPairLevelState) PeerPairRekeyPlan {
	return PeerPairRekeyPlan{
		RotationRequired:     false,
		LevelChangeDirection: "unchanged",
		PreviousHighestLevel: previous.HighestLevel(),
		NextHighestLevel:     next.HighestLevel(),
		PreviousKeyID:        current.KeyID,
		PreviousLevels:       previous,
		NextLevels:           next,
		Next:                 current,
	}
}

func peerPairReplacementPlan(current PeerPairKeyMaterial, previous, next PeerPairLevelState, replacement PeerPairKeyMaterial, reason string) PeerPairRekeyPlan {
	direction := peerPairLevelDirection(previous.HighestLevel(), next.HighestLevel())
	planned := PeerPairKeyAuditEvent{
		Event:                "peer_pair_key_rekey_planned",
		Reason:               reason,
		Direction:            direction,
		PreviousHighestLevel: previous.HighestLevel(),
		NextHighestLevel:     next.HighestLevel(),
		PreviousKeyID:        current.KeyID,
		NextKeyID:            replacement.KeyID,
	}
	return PeerPairRekeyPlan{
		RotationRequired:              true,
		Reason:                        reason,
		LevelChangeDirection:          direction,
		PreviousHighestLevel:          previous.HighestLevel(),
		NextHighestLevel:              next.HighestLevel(),
		PreviousKeyID:                 current.KeyID,
		NegotiationProtectedByKeyID:   current.KeyID,
		ActivateAfterExchange:         true,
		RevokePreviousAfterActivation: true,
		PreviousLevels:                previous,
		NextLevels:                    next,
		Next:                          replacement,
		AuditEvents:                   []PeerPairKeyAuditEvent{planned},
	}
}

func (s PeerPairLevelState) HighestLevel() int {
	return maxLevel(s.LocalLevel, s.RemoteLevel)
}

func peerPairLevelDirection(previous, next int) string {
	switch {
	case next > previous:
		return "upgrade"
	case next < previous:
		return "downgrade"
	default:
		return "same-level"
	}
}

func stablePairID(a, b string) string {
	ids := []string{a, b}
	sort.Strings(ids)
	digest := sha256.Sum256([]byte(fmt.Sprintf("fse-peer-pair-v1\n%s\n%s", ids[0], ids[1])))
	return base64.RawURLEncoding.EncodeToString(digest[:16])
}

func maxLevel(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func findEnabledIdentityGroup(groups []config.IdentityGroupConfig, groupID string) (config.IdentityGroupConfig, bool) {
	for _, group := range groups {
		if group.ID == groupID && group.Enabled {
			return group, true
		}
	}
	return config.IdentityGroupConfig{}, false
}

func findIdentityGroup(groups []config.IdentityGroupConfig, groupID string) (config.IdentityGroupConfig, bool) {
	for _, group := range groups {
		if group.ID == groupID {
			return group, true
		}
	}
	return config.IdentityGroupConfig{}, false
}
