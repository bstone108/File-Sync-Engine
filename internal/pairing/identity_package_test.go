package pairing

import (
	"os"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/config"
)

func TestBuildIdentityPackageExportsDiscoveryIDAndBootstrapProofWithoutDaemonSecrets(t *testing.T) {
	cfg := config.Config{
		NodeName: "node-a",
		API:      config.APIConfig{Key: "api-secret"},
		Identity: config.IdentityConfig{
			PrivateKey:      "private-secret",
			PublicKey:       "public-discovery-key",
			EncryptionLevel: 4,
			Groups: []config.IdentityGroupConfig{{
				ID:      "family-sync",
				Token:   strings.Repeat("a", 80),
				Enabled: true,
			}},
		},
	}

	pkg, err := BuildIdentityPackage(cfg, "family-sync")
	if err != nil {
		t.Fatalf("BuildIdentityPackage returned error: %v", err)
	}

	if pkg.Version != "fse-identity-package-v1" {
		t.Fatalf("unexpected package version %q", pkg.Version)
	}
	if pkg.NodeName != "node-a" || pkg.DiscoveryID != "public-discovery-key" {
		t.Fatalf("package should expose node discovery identity, got %+v", pkg)
	}
	if pkg.GroupID != "family-sync" || pkg.BootstrapProofKey != strings.Repeat("a", 80) {
		t.Fatalf("package should include selected same-identity bootstrap proof key, got %+v", pkg)
	}
	if pkg.BootstrapEncryptionLevel != 10 || pkg.DefaultPeerEncryptionLevel != 4 {
		t.Fatalf("unexpected encryption levels: bootstrap=%d peer=%d", pkg.BootstrapEncryptionLevel, pkg.DefaultPeerEncryptionLevel)
	}

	encoded, err := MarshalIdentityPackage(pkg)
	if err != nil {
		t.Fatalf("MarshalIdentityPackage returned error: %v", err)
	}
	body := string(encoded)
	for _, leaked := range []string{"api-secret", "private-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("identity package leaked daemon secret %q: %s", leaked, body)
		}
	}
}

func TestBuildIdentityPackageRejectsMissingOrDisabledGroups(t *testing.T) {
	cfg := config.Config{
		NodeName: "node-a",
		Identity: config.IdentityConfig{PublicKey: "public", EncryptionLevel: 4, Groups: []config.IdentityGroupConfig{{
			ID:      "disabled",
			Token:   strings.Repeat("b", 80),
			Enabled: false,
		}}},
	}

	if _, err := BuildIdentityPackage(cfg, "disabled"); err == nil || !strings.Contains(err.Error(), "enabled identity group") {
		t.Fatalf("expected disabled group rejection, got %v", err)
	}
	if _, err := BuildIdentityPackage(cfg, "missing"); err == nil || !strings.Contains(err.Error(), "enabled identity group") {
		t.Fatalf("expected missing group rejection, got %v", err)
	}
}

func TestPlanIdentityBootstrapUsesPackageOnlyForIntroductionAndRequiresDedicatedPeerKeys(t *testing.T) {
	cfg := config.Config{
		NodeName: "node-b",
		Identity: config.IdentityConfig{
			PublicKey:       "local-public-key",
			EncryptionLevel: 7,
			Groups: []config.IdentityGroupConfig{{
				ID:      "family-sync",
				Token:   strings.Repeat("c", 80),
				Enabled: true,
			}},
		},
	}
	pkg := IdentityPackage{
		Version:                    IdentityPackageVersion,
		NodeName:                   "node-a",
		DiscoveryID:                "remote-public-key",
		GroupID:                    "family-sync",
		BootstrapProofKey:          strings.Repeat("c", 80),
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
	}

	plan, err := PlanIdentityBootstrap(cfg, pkg)
	if err != nil {
		t.Fatalf("PlanIdentityBootstrap returned error: %v", err)
	}
	if plan.RemoteDiscoveryID != "remote-public-key" || plan.GroupID != "family-sync" {
		t.Fatalf("plan should target imported remote identity and group, got %+v", plan)
	}
	if plan.IntroductionEncryptionLevel != 10 {
		t.Fatalf("introduction encryption level = %d", plan.IntroductionEncryptionLevel)
	}
	if plan.PeerPairEncryptionLevel != 7 {
		t.Fatalf("peer-pair encryption level = %d", plan.PeerPairEncryptionLevel)
	}
	if plan.UsesBootstrapKeyForTraffic {
		t.Fatalf("bootstrap proof key must not become ordinary peer traffic key: %+v", plan)
	}
	if !plan.RequiresDedicatedPeerPairKey {
		t.Fatalf("bootstrap plan must require negotiated dedicated peer-pair key: %+v", plan)
	}
}

func TestPlanIdentityBootstrapUsesMaximumSecurityProfileForIntroduction(t *testing.T) {
	cfg := config.Config{Identity: config.IdentityConfig{PublicKey: "local", EncryptionLevel: 4, Groups: []config.IdentityGroupConfig{{
		ID:      "family-sync",
		Token:   strings.Repeat("m", 80),
		Enabled: true,
	}}}}
	pkg := IdentityPackage{
		Version:                    IdentityPackageVersion,
		DiscoveryID:                "remote",
		GroupID:                    "family-sync",
		BootstrapProofKey:          strings.Repeat("m", 80),
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
	}

	plan, err := PlanIdentityBootstrap(cfg, pkg)
	if err != nil {
		t.Fatalf("PlanIdentityBootstrap returned error: %v", err)
	}

	if plan.IntroductionProfile.Level != 10 || plan.IntroductionProfile.Name != "maximum-high-cpu" {
		t.Fatalf("identity introduction should use level-10 maximum profile, got %+v", plan.IntroductionProfile)
	}
	if !plan.PrioritizesBootstrapSecurityOverPerformance {
		t.Fatalf("identity introduction must prioritize maximum security over speed/resources: %+v", plan)
	}
	if plan.PeerPairEncryptionLevel != 4 {
		t.Fatalf("ordinary peer-pair level should remain independent of level-10 bootstrap, got %d", plan.PeerPairEncryptionLevel)
	}
}

func TestPlanIdentityBootstrapRejectsMismatchedBootstrapProof(t *testing.T) {
	cfg := config.Config{Identity: config.IdentityConfig{PublicKey: "local", EncryptionLevel: 4, Groups: []config.IdentityGroupConfig{{
		ID:      "family-sync",
		Token:   strings.Repeat("d", 80),
		Enabled: true,
	}}}}
	pkg := IdentityPackage{
		Version:                    IdentityPackageVersion,
		DiscoveryID:                "remote",
		GroupID:                    "family-sync",
		BootstrapProofKey:          strings.Repeat("e", 80),
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
	}

	if _, err := PlanIdentityBootstrap(cfg, pkg); err == nil || !strings.Contains(err.Error(), "bootstrap proof") {
		t.Fatalf("expected bootstrap proof mismatch rejection, got %v", err)
	}
}

func TestPeerPairKeyIDIsNotDerivedFromSecretKeyMaterial(t *testing.T) {
	source, err := os.ReadFile("identity_package.go")
	if err != nil {
		t.Fatalf("read identity package source: %v", err)
	}
	body := string(source)
	for _, forbidden := range []string{"keyIDDigest", "secretKey)))", "secretKey))"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("peer-pair key IDs must be independent random identifiers, not SHA-256 digests of secret key material; found %q", forbidden)
		}
	}
}

func TestNewPeerPairKeyMaterialUsesUniquePairKeyAndHighestLevel(t *testing.T) {
	material, err := NewPeerPairKeyMaterial("node-a-public", "node-b-public", 3, 10, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("NewPeerPairKeyMaterial returned error: %v", err)
	}
	if material.LocalDiscoveryID != "node-a-public" || material.RemoteDiscoveryID != "node-b-public" {
		t.Fatalf("material should preserve pair endpoints, got %+v", material)
	}
	if material.EncryptionLevel != 10 {
		t.Fatalf("peer-pair material should use highest configured level, got %d", material.EncryptionLevel)
	}
	if material.SecretKey == "" || material.SecretKey == "bootstrap-proof-secret" {
		t.Fatalf("peer-pair material must generate a non-empty dedicated traffic key that is not the bootstrap proof key")
	}
	if material.KeyID == "" || material.PairID == "" {
		t.Fatalf("peer-pair material should expose non-secret identifiers, got keyID=%q pairID=%q", material.KeyID, material.PairID)
	}

	reversed, err := NewPeerPairKeyMaterial("node-b-public", "node-a-public", 10, 3, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("NewPeerPairKeyMaterial reversed returned error: %v", err)
	}
	if reversed.PairID != material.PairID {
		t.Fatalf("pair ID should be stable regardless of endpoint order: %q vs %q", material.PairID, reversed.PairID)
	}
	if reversed.SecretKey == material.SecretKey {
		t.Fatalf("each negotiation must create fresh key material")
	}
}

func TestPlanPeerPairRekeyRotatesWhenAdvertisedLevelChanges(t *testing.T) {
	current, err := NewPeerPairKeyMaterial("node-a-public", "node-b-public", 4, 4, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("NewPeerPairKeyMaterial returned error: %v", err)
	}

	plan, err := PlanPeerPairRekey(current, PeerPairLevelState{LocalLevel: 4, RemoteLevel: 4}, PeerPairLevelState{LocalLevel: 7, RemoteLevel: 4}, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("PlanPeerPairRekey returned error: %v", err)
	}

	if !plan.RotationRequired {
		t.Fatalf("level change should require rekey: %+v", plan)
	}
	if plan.Reason != "encryption-level-change" {
		t.Fatalf("unexpected rekey reason %q", plan.Reason)
	}
	if plan.NegotiationProtectedByKeyID != current.KeyID || plan.PreviousKeyID != current.KeyID {
		t.Fatalf("rekey must be negotiated over the current key before revocation: %+v", plan)
	}
	if !plan.ActivateAfterExchange || !plan.RevokePreviousAfterActivation {
		t.Fatalf("new key should activate after exchange and then revoke previous key: %+v", plan)
	}
	if plan.Next.EncryptionLevel != 7 {
		t.Fatalf("new key should use current highest advertised level, got %d", plan.Next.EncryptionLevel)
	}
	if plan.Next.PairID != current.PairID {
		t.Fatalf("rekey should preserve pair ID, got %q want %q", plan.Next.PairID, current.PairID)
	}
	if plan.Next.KeyID == current.KeyID || plan.Next.SecretKey == current.SecretKey || plan.Next.SecretKey == "bootstrap-proof-secret" {
		t.Fatalf("rekey must create fresh dedicated material and never reuse old/bootstrap keys: current=%+v next=%+v", current, plan.Next)
	}

	unchanged, err := PlanPeerPairRekey(plan.Next, PeerPairLevelState{LocalLevel: 7, RemoteLevel: 4}, PeerPairLevelState{LocalLevel: 7, RemoteLevel: 4}, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("PlanPeerPairRekey unchanged returned error: %v", err)
	}
	if unchanged.RotationRequired || unchanged.Next.KeyID != plan.Next.KeyID {
		t.Fatalf("unchanged levels should keep current key: %+v", unchanged)
	}
}

func TestPlanBootstrapKeyLifecycleKeepsIdentityKeyManualOnly(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	plan := PlanBootstrapKeyLifecycle(created, created.Add(365*24*time.Hour))

	if plan.AutomaticRotationAllowed {
		t.Fatalf("shared identity bootstrap key must not be scheduled for automatic traffic-key rotation: %+v", plan)
	}
	if plan.RotationRequired {
		t.Fatalf("shared identity bootstrap key regeneration must be manual only even when old: %+v", plan)
	}
	if plan.RegenerationMode != "manual-only" || !plan.RequiresUserSafetyFlow {
		t.Fatalf("bootstrap key lifecycle should require GUI/API safety flow, got %+v", plan)
	}
	if plan.AppliesPeerPairRotationPolicy {
		t.Fatalf("bootstrap key lifecycle must stay separate from peer-pair traffic-key rotation: %+v", plan)
	}
}

func TestPlanPeerPairScheduledRotationUsesDefaultThreeMonthInterval(t *testing.T) {
	current, err := NewPeerPairKeyMaterial("node-a-public", "node-b-public", 4, 4, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("NewPeerPairKeyMaterial returned error: %v", err)
	}
	current.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tooEarly, err := PlanPeerPairScheduledRotation(current, PeerPairLevelState{LocalLevel: 4, RemoteLevel: 4}, current.CreatedAt.Add(89*24*time.Hour), "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("PlanPeerPairScheduledRotation too early returned error: %v", err)
	}
	if tooEarly.RotationRequired {
		t.Fatalf("key younger than default rotation interval should remain active: %+v", tooEarly)
	}

	plan, err := PlanPeerPairScheduledRotation(current, PeerPairLevelState{LocalLevel: 4, RemoteLevel: 4}, current.CreatedAt.Add(90*24*time.Hour), "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("PlanPeerPairScheduledRotation returned error: %v", err)
	}
	if !plan.RotationRequired {
		t.Fatalf("default three-month interval should require scheduled rotation: %+v", plan)
	}
	if plan.Reason != "scheduled-rotation" {
		t.Fatalf("unexpected rotation reason %q", plan.Reason)
	}
	if plan.PreviousKeyID != current.KeyID || plan.NegotiationProtectedByKeyID != current.KeyID {
		t.Fatalf("scheduled rotation should exchange over current key before revocation: %+v", plan)
	}
	if !plan.ActivateAfterExchange || !plan.RevokePreviousAfterActivation {
		t.Fatalf("scheduled rotation should activate replacement before revoking previous key: %+v", plan)
	}
	if plan.Next.EncryptionLevel != current.EncryptionLevel || plan.Next.PairID != current.PairID {
		t.Fatalf("scheduled rotation should preserve peer-pair identity and level: current=%+v next=%+v", current, plan.Next)
	}
	if plan.Next.KeyID == current.KeyID || plan.Next.SecretKey == current.SecretKey || plan.Next.SecretKey == "bootstrap-proof-secret" {
		t.Fatalf("scheduled rotation must create fresh dedicated material: current=%+v next=%+v", current, plan.Next)
	}
}

func TestPlanPeerPairRekeySupportsDowngradesWithAuditStatus(t *testing.T) {
	current, err := NewPeerPairKeyMaterial("node-a-public", "node-b-public", 9, 8, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("NewPeerPairKeyMaterial returned error: %v", err)
	}

	plan, err := PlanPeerPairRekey(current, PeerPairLevelState{LocalLevel: 9, RemoteLevel: 8}, PeerPairLevelState{LocalLevel: 3, RemoteLevel: 4}, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("PlanPeerPairRekey downgrade returned error: %v", err)
	}

	if !plan.RotationRequired || plan.Next.EncryptionLevel != 4 {
		t.Fatalf("downgrade should create replacement at current highest configured level 4: %+v", plan)
	}
	if plan.LevelChangeDirection != "downgrade" || plan.PreviousHighestLevel != 9 || plan.NextHighestLevel != 4 {
		t.Fatalf("downgrade audit/status fields are wrong: %+v", plan)
	}
	if len(plan.AuditEvents) != 1 || plan.AuditEvents[0].Event != "peer_pair_key_rekey_planned" || plan.AuditEvents[0].Direction != "downgrade" || plan.AuditEvents[0].PreviousKeyID != current.KeyID || plan.AuditEvents[0].NextKeyID != plan.Next.KeyID {
		t.Fatalf("downgrade plan should expose a redacted audit event with old/new key IDs: %+v", plan.AuditEvents)
	}

	activated, err := ActivatePeerPairRekey(current, plan, plan.Next.KeyID)
	if err != nil {
		t.Fatalf("ActivatePeerPairRekey downgrade returned error: %v", err)
	}
	if activated.Active.EncryptionLevel != 4 || activated.LastChangeDirection != "downgrade" || activated.LastChangeReason != "encryption-level-change" {
		t.Fatalf("activated downgrade status should expose current level and direction: %+v", activated)
	}
	if len(activated.AuditEvents) != 2 || activated.AuditEvents[1].Event != "peer_pair_key_rekey_activated" || activated.AuditEvents[1].Direction != "downgrade" {
		t.Fatalf("activated state should retain planning and activation audit events: %+v", activated.AuditEvents)
	}
}

func TestActivatePeerPairRekeyRevokesPreviousKeyAfterExchange(t *testing.T) {
	current, err := NewPeerPairKeyMaterial("node-a-public", "node-b-public", 4, 4, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("NewPeerPairKeyMaterial returned error: %v", err)
	}
	plan, err := PlanPeerPairRekey(current, PeerPairLevelState{LocalLevel: 4, RemoteLevel: 4}, PeerPairLevelState{LocalLevel: 6, RemoteLevel: 4}, "bootstrap-proof-secret")
	if err != nil {
		t.Fatalf("PlanPeerPairRekey returned error: %v", err)
	}

	activated, err := ActivatePeerPairRekey(current, plan, plan.Next.KeyID)
	if err != nil {
		t.Fatalf("ActivatePeerPairRekey returned error: %v", err)
	}

	if activated.Active.KeyID != plan.Next.KeyID {
		t.Fatalf("new key should be active after exchange, got %+v", activated.Active)
	}
	if !activated.IsKeyAuthorized(plan.Next.KeyID) {
		t.Fatalf("replacement key should be authorized after activation")
	}
	if activated.IsKeyAuthorized(current.KeyID) {
		t.Fatalf("previous key should be revoked after replacement activation")
	}
	if !activated.IsKeyRevoked(current.KeyID) {
		t.Fatalf("previous key should be recorded as revoked")
	}
}

func TestPlanIdentityBootstrapRejectsPersistentlyRevokedIdentityMaterial(t *testing.T) {
	proof := strings.Repeat("v", 80)
	cfg := config.Config{Identity: config.IdentityConfig{
		PublicKey:       "local",
		EncryptionLevel: 4,
		Groups: []config.IdentityGroupConfig{{
			ID:      "family-sync",
			Token:   proof,
			Enabled: true,
		}},
		Revoked: []config.RevokedIdentityConfig{{
			GroupID:               "family-sync",
			DiscoveryID:           "remote",
			BootstrapProofKeyHash: HashIdentityBootstrapProof(proof),
		}},
	}}
	pkg := IdentityPackage{
		Version:                    IdentityPackageVersion,
		DiscoveryID:                "remote",
		GroupID:                    "family-sync",
		BootstrapProofKey:          proof,
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
	}

	if _, err := PlanIdentityBootstrap(cfg, pkg); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked identity package to be rejected, got %v", err)
	}
}

func TestRecordIdentityRevocationStoresNonSecretPersistentBlockRecord(t *testing.T) {
	proof := strings.Repeat("s", 80)
	record := RecordIdentityRevocation("family-sync", "remote", proof, time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC))

	if record.GroupID != "family-sync" || record.DiscoveryID != "remote" {
		t.Fatalf("record should identify revoked identity material, got %+v", record)
	}
	if record.BootstrapProofKeyHash == "" || strings.Contains(record.BootstrapProofKeyHash, proof) {
		t.Fatalf("record should persist only a non-secret bootstrap proof hash, got %+v", record)
	}
	if record.RevokedAt.IsZero() {
		t.Fatalf("record should persist revocation time: %+v", record)
	}
}

func TestPlanIdentityRevocationBreaksOnlyIdentityDerivedRelationships(t *testing.T) {
	cfg := config.Config{
		Identity: config.IdentityConfig{Groups: []config.IdentityGroupConfig{{ID: "family-sync", Token: strings.Repeat("r", 80), Enabled: true}}},
		Peers: []config.PeerConfig{
			{ID: "identity-peer-a", IdentityPublicKey: "pub-a", Addresses: []string{"tcp://10.0.0.2:22420"}},
			{ID: "manual-peer", Addresses: []string{"tcp://10.0.0.3:22420"}},
		},
		Folders: []config.FolderConfig{
			{ID: "identity-docs", Enabled: false, AdvertisedBy: "identity-peer-a", IdentityGroup: "family-sync"},
			{ID: "manual-share", Enabled: true, Path: "/shares/manual", Mode: config.ModeSendReceive, BlockSize: config.DefaultBlockSize},
		},
	}

	plan, err := PlanIdentityRevocation(cfg, "family-sync", []string{"identity-peer-a", "manual-peer", "identity-peer-a"})
	if err != nil {
		t.Fatalf("PlanIdentityRevocation returned error: %v", err)
	}

	if !plan.GlobalRevocation || plan.GroupID != "family-sync" || !plan.BreaksIdentityTrust {
		t.Fatalf("revocation should globally break selected identity trust: %+v", plan)
	}
	if !sameStrings(plan.FinalRevocationPeerIDs, []string{"identity-peer-a"}) {
		t.Fatalf("revocation should send final messages only to reachable identity peers, got %+v", plan.FinalRevocationPeerIDs)
	}
	if !sameStrings(plan.DisconnectPeerIDs, []string{"identity-peer-a"}) {
		t.Fatalf("revocation should disconnect identity-derived peers only, got %+v", plan.DisconnectPeerIDs)
	}
	if !sameStrings(plan.DisableFolderIDs, []string{"identity-docs"}) {
		t.Fatalf("revocation should disable identity-derived folders only, got %+v", plan.DisableFolderIDs)
	}
	if !sameStrings(plan.PreservePeerIDs, []string{"manual-peer"}) || !sameStrings(plan.PreserveFolderIDs, []string{"manual-share"}) {
		t.Fatalf("manual relationships should be preserved: %+v", plan)
	}
	if plan.RevokeBootstrapKeyAutomatically || !plan.RequiresNewIdentityForReconnect {
		t.Fatalf("revocation must not silently rotate/repair the identity; it should require a new manual identity: %+v", plan)
	}
}

func TestPlanIdentityRevocationRequiresFreshManualReconnect(t *testing.T) {
	cfg := config.Config{
		Identity: config.IdentityConfig{Groups: []config.IdentityGroupConfig{{ID: "family-sync", Token: strings.Repeat("r", 80), Enabled: true}}},
	}

	plan, err := PlanIdentityRevocation(cfg, "family-sync", nil)
	if err != nil {
		t.Fatalf("PlanIdentityRevocation returned error: %v", err)
	}

	if !plan.RequiresNewIdentityForReconnect || !plan.RequiresManualSharingForReconnect {
		t.Fatalf("revoked identities should require a newly generated identity shared/imported manually: %+v", plan)
	}
	if plan.AutoRejoinAllowed || plan.RevokedIdentityMaterialReusable {
		t.Fatalf("revoked identity material must not silently auto-rejoin or be reused: %+v", plan)
	}
	if plan.ReconnectAction != "generate-new-identity-and-manual-import" {
		t.Fatalf("unexpected reconnect action: %+v", plan)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
