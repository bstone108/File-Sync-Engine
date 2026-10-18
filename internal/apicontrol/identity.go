package apicontrol

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filesyncengine/internal/advertise"
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
	reachable := strings.TrimSpace(req.ReachableAPIURL)
	if reachable == "" {
		reachable = strings.TrimSpace(cfg.API.AdvertiseURL)
	}
	if advertise.ContainerInternalURL(reachable) {
		reachable = ""
	}
	if reachable == "" {
		if resolved, err := advertise.ResolveWithHost(cfg, os.Getenv(advertise.EnvVar), ""); err == nil && !advertise.ContainerInternalURL(resolved) {
			reachable = resolved
		}
	}
	if advertise.ContainerInternalURL(reachable) {
		reachable = ""
	}
	if reachable != "" {
		pkg.ReachableAPIURL = reachable
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
	if _, ok := peerByDiscoveryID(cfg, req.Package.DiscoveryID); ok {
		return api.IdentityImportResponse{
			Status:            "accepted",
			Message:           "already paired with this engine",
			GroupID:           req.Package.GroupID,
			RemoteDiscoveryID: req.Package.DiscoveryID,
		}, nil
	}
	localURL := ""
	if strings.TrimSpace(req.Package.ReachableAPIURL) != "" {
		localURL, err = localReachableAPIURL(cfg, req.LocalReachableAPIURL)
		if err != nil {
			return api.IdentityImportResponse{}, err
		}
		req.LocalReachableAPIURL = localURL
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

func localReachableAPIURL(cfg config.Config, requested string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		return advertise.Normalize(requested, cfg)
	}
	resolved, err := advertise.ResolveWithHost(cfg, os.Getenv(advertise.EnvVar), advertise.DetectHost())
	if err != nil {
		return "", fmt.Errorf("this engine could not determine its reachable API URL; set %s", advertise.EnvVar)
	}
	return resolved, nil
}

func peerByDiscoveryID(cfg config.Config, discoveryID string) (config.PeerConfig, bool) {
	discoveryID = strings.TrimSpace(discoveryID)
	if discoveryID == "" {
		return config.PeerConfig{}, false
	}
	for _, peer := range cfg.Peers {
		if peer.IdentityPublicKey == discoveryID {
			return peer, true
		}
	}
	return config.PeerConfig{}, false
}

func exchangeAndPersistPeer(ctx context.Context, configPath string, cfg config.Config, req api.IdentityImportRequest) error {
	fingerprint, err := localCertificateIfNeeded(configPath, cfg)
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
		LinkProfile:          claimProfile(cfg, fingerprint),
	})
	if err != nil {
		return err
	}
	remote := accepted.LinkProfile
	if remote.CertificateSHA256 == "" {
		remote.CertificateSHA256 = accepted.APICertificateSHA256
	}
	return configops.UpsertPairedPeer(configPath, config.PeerConfig{
		ID:                   configops.PairedPeerID(accepted.NodeName, accepted.DiscoveryID),
		APIKey:               accepted.APIKey,
		APICertificateSHA256: accepted.APICertificateSHA256,
		IdentityPublicKey:    accepted.DiscoveryID,
		IdentityGroupID:      req.Package.GroupID,
		DHTPeerID:            remote.DHTPeerID,
		Relays:               remote.Relays,
		Endpoints:            remote.Endpoints(req.Package.ReachableAPIURL),
	})
}

// HandlePairExchange checks the bootstrap proof and stores the caller as a peer.
func HandlePairExchange(configPath string, req api.PairExchangeRequest) (api.PairExchangeResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.PairExchangeResponse{}, err
	}
	if req.DiscoveryID == "" || req.DiscoveryID == cfg.Identity.PublicKey {
		return api.PairExchangeResponse{}, fmt.Errorf("pair exchange discovery id is invalid")
	}
	if existing, ok := peerByDiscoveryID(cfg, req.DiscoveryID); ok && req.APIKey != "" && subtle.ConstantTimeCompare([]byte(existing.APIKey), []byte(req.APIKey)) == 1 {
		fingerprint, err := localCertificateIfNeeded(configPath, cfg)
		if err != nil {
			return api.PairExchangeResponse{}, err
		}
		return api.PairExchangeResponse{
			DiscoveryID:          cfg.Identity.PublicKey,
			NodeName:             cfg.NodeName,
			APIKey:               cfg.API.Key,
			APICertificateSHA256: fingerprint,
			LinkProfile:          claimProfile(cfg, fingerprint),
		}, nil
	}
	token, ok := pairing.GroupToken(cfg, req.GroupID)
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(req.BootstrapProofKey)) != 1 {
		return api.PairExchangeResponse{}, fmt.Errorf("identity bootstrap proof does not match group %q", req.GroupID)
	}
	if req.APIKey == "" || req.ReachableAPIURL == "" {
		return api.PairExchangeResponse{}, fmt.Errorf("pair exchange is missing the caller API address or key")
	}
	fingerprint, err := localCertificateIfNeeded(configPath, cfg)
	if err != nil {
		return api.PairExchangeResponse{}, err
	}
	remote := req.LinkProfile
	if err := configops.UpsertPairedPeer(configPath, config.PeerConfig{
		ID:                   configops.PairedPeerID(req.NodeName, req.DiscoveryID),
		APIKey:               req.APIKey,
		APICertificateSHA256: req.APICertificateSHA256,
		IdentityPublicKey:    req.DiscoveryID,
		IdentityGroupID:      req.GroupID,
		DHTPeerID:            remote.DHTPeerID,
		Relays:               remote.Relays,
		Endpoints:            remote.Endpoints(req.ReachableAPIURL),
	}); err != nil {
		return api.PairExchangeResponse{}, err
	}
	return api.PairExchangeResponse{
		DiscoveryID:          cfg.Identity.PublicKey,
		NodeName:             cfg.NodeName,
		APIKey:               cfg.API.Key,
		APICertificateSHA256: fingerprint,
		LinkProfile:          claimProfile(cfg, fingerprint),
	}, nil
}

func localCertificateIfNeeded(configPath string, cfg config.Config) (string, error) {
	if !cfg.API.RequiresTLS() && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.API.AdvertiseURL)), "https://") {
		return "", nil
	}
	return localAPICertificateFingerprint(configPath, cfg)
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
