package apicontrol

import (
	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/pairing"
)

// HandleIdentityPackage builds a GUI/API-safe shared identity package from the
// current config while leaving daemon API keys and private identity keys out of
// the returned pairing payload.
func HandleIdentityPackage(configPath string, req api.IdentityPackageRequest) (pairing.IdentityPackage, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return pairing.IdentityPackage{}, err
	}
	return pairing.BuildIdentityPackage(cfg, req.GroupID)
}

// HandleIdentityImport validates an imported same-identity package and prepares
// redacted dedicated peer-pair key identifiers without returning bootstrap
// proof material or other secret config fields.
func HandleIdentityImport(configPath string, req api.IdentityImportRequest) (api.IdentityImportResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.IdentityImportResponse{}, err
	}
	plan, err := pairing.PlanIdentityBootstrap(cfg, req.Package)
	if err != nil {
		return api.IdentityImportResponse{}, err
	}
	keyMaterial, err := pairing.NewPeerPairKeyMaterial(plan.LocalDiscoveryID, plan.RemoteDiscoveryID, plan.PeerPairEncryptionLevel, req.Package.DefaultPeerEncryptionLevel, req.Package.BootstrapProofKey)
	if err != nil {
		return api.IdentityImportResponse{}, err
	}
	return api.IdentityImportResponse{
		Status:                       "accepted",
		Message:                      "identity import accepted; dedicated peer-pair key negotiation prepared",
		GroupID:                      plan.GroupID,
		RemoteDiscoveryID:            plan.RemoteDiscoveryID,
		IntroductionEncryptionLevel:  plan.IntroductionEncryptionLevel,
		PeerPairEncryptionLevel:      plan.PeerPairEncryptionLevel,
		RequiresDedicatedPeerPairKey: plan.RequiresDedicatedPeerPairKey,
		UsesBootstrapKeyForTraffic:   plan.UsesBootstrapKeyForTraffic,
		PairID:                       keyMaterial.PairID,
		KeyID:                        keyMaterial.KeyID,
	}, nil
}
