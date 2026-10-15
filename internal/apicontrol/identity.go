package apicontrol

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configops"
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
	pkg, err := pairing.BuildIdentityPackage(cfg, req.GroupID)
	if err != nil {
		return pairing.IdentityPackage{}, err
	}
	if strings.TrimSpace(req.ReachableAPIURL) != "" {
		pkg.ReachableAPIURL = strings.TrimSpace(req.ReachableAPIURL)
		fingerprint, err := localAPICertificateFingerprint(configPath, cfg)
		if err != nil {
			return pairing.IdentityPackage{}, err
		}
		pkg.APICertificateSHA256 = fingerprint
	}
	return pkg, nil
}

// HandleIdentityImport validates an imported same-identity package and prepares
// redacted dedicated peer-pair key identifiers without returning bootstrap
// proof material or other secret config fields.
func HandleIdentityImport(configPath string, req api.IdentityImportRequest) (api.IdentityImportResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.IdentityImportResponse{}, err
	}
	if strings.TrimSpace(req.Package.ReachableAPIURL) != "" {
		if strings.TrimSpace(req.LocalReachableAPIURL) == "" {
			return api.IdentityImportResponse{}, fmt.Errorf("local reachable API URL is required to pair with a remote engine")
		}
		if err := adoptIdentityGroup(&cfg, req.Package); err != nil {
			return api.IdentityImportResponse{}, err
		}
		if err := writeConfig(configPath, cfg); err != nil {
			return api.IdentityImportResponse{}, err
		}
	}
	plan, err := pairing.PlanIdentityBootstrap(cfg, req.Package)
	if err != nil {
		return api.IdentityImportResponse{}, err
	}
	if strings.TrimSpace(req.Package.ReachableAPIURL) != "" {
		if err := exchangeAndPersistPeer(context.Background(), configPath, cfg, req); err != nil {
			return api.IdentityImportResponse{}, err
		}
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

func adoptIdentityGroup(cfg *config.Config, pkg pairing.IdentityPackage) error {
	for i, group := range cfg.Identity.Groups {
		if group.ID != pkg.GroupID {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(group.Token), []byte(pkg.BootstrapProofKey)) != 1 {
			return fmt.Errorf("identity bootstrap proof does not match group %q", pkg.GroupID)
		}
		cfg.Identity.Groups[i].Enabled = true
		return nil
	}
	cfg.Identity.Groups = append(cfg.Identity.Groups, config.IdentityGroupConfig{ID: pkg.GroupID, Token: pkg.BootstrapProofKey, Enabled: true})
	return cfg.Validate()
}

func exchangeAndPersistPeer(ctx context.Context, configPath string, cfg config.Config, req api.IdentityImportRequest) error {
	fingerprint, err := localAPICertificateFingerprint(configPath, cfg)
	if err != nil {
		return err
	}
	accepted, err := pairing.ExchangePair(ctx, req.Package.ReachableAPIURL, req.Package.APICertificateSHA256, pairing.PairOffer{
		GroupID:              req.Package.GroupID,
		BootstrapProofKey:    req.Package.BootstrapProofKey,
		DiscoveryID:          cfg.Identity.PublicKey,
		NodeName:             cfg.NodeName,
		ReachableAPIURL:      strings.TrimSpace(req.LocalReachableAPIURL),
		APICertificateSHA256: fingerprint,
		APIKey:               cfg.API.Key,
	})
	if err != nil {
		return err
	}
	return configops.UpsertPairedPeer(configPath, config.PeerConfig{
		ID:                   configops.PairedPeerID(accepted.NodeName, accepted.DiscoveryID),
		APIKey:               accepted.APIKey,
		APICertificateSHA256: accepted.APICertificateSHA256,
		IdentityPublicKey:    accepted.DiscoveryID,
		Endpoints:            []config.EndpointConfig{{Kind: "manual", Address: strings.TrimSpace(req.Package.ReachableAPIURL)}},
	})
}

// HandlePairExchange checks the bootstrap proof and stores the caller as a peer.
func HandlePairExchange(configPath string, req api.PairExchangeRequest) (api.PairExchangeResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.PairExchangeResponse{}, err
	}
	token, ok := pairing.GroupToken(cfg, req.GroupID)
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(req.BootstrapProofKey)) != 1 {
		return api.PairExchangeResponse{}, fmt.Errorf("identity bootstrap proof does not match group %q", req.GroupID)
	}
	if req.DiscoveryID == "" || req.DiscoveryID == cfg.Identity.PublicKey {
		return api.PairExchangeResponse{}, fmt.Errorf("pair exchange discovery id is invalid")
	}
	if req.APIKey == "" || req.ReachableAPIURL == "" {
		return api.PairExchangeResponse{}, fmt.Errorf("pair exchange is missing the caller API address or key")
	}
	if err := configops.UpsertPairedPeer(configPath, config.PeerConfig{
		ID:                   configops.PairedPeerID(req.NodeName, req.DiscoveryID),
		APIKey:               req.APIKey,
		APICertificateSHA256: req.APICertificateSHA256,
		IdentityPublicKey:    req.DiscoveryID,
		Endpoints:            []config.EndpointConfig{{Kind: "manual", Address: strings.TrimSpace(req.ReachableAPIURL)}},
	}); err != nil {
		return api.PairExchangeResponse{}, err
	}
	fingerprint, err := localAPICertificateFingerprint(configPath, cfg)
	if err != nil {
		return api.PairExchangeResponse{}, err
	}
	return api.PairExchangeResponse{
		DiscoveryID:          cfg.Identity.PublicKey,
		NodeName:             cfg.NodeName,
		APIKey:               cfg.API.Key,
		APICertificateSHA256: fingerprint,
	}, nil
}

func localAPICertificateFingerprint(configPath string, cfg config.Config) (string, error) {
	certFile := cfg.API.Encryption.CertFile
	if certFile == "" {
		certFile = filepath.Join(filepath.Dir(configPath), "fse-api-auto.crt")
	} else if !filepath.IsAbs(certFile) {
		certFile = filepath.Join(filepath.Dir(configPath), certFile)
	}
	pemBytes, err := os.ReadFile(certFile)
	if err != nil {
		return "", fmt.Errorf("read API certificate: %w", err)
	}
	return CertificateFingerprintSHA256(pemBytes)
}
