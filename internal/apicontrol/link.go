package apicontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configops"
	"filesyncengine/internal/link"
	"filesyncengine/internal/peeridentity"
)

var (
	linkService   = link.NewService(link.UnavailableDirectory{})
	detectLinkIPs = link.LocalIPs
)

// SetLinkService installs the process pairing-code directory. Tests use a
// memory directory; the daemon installs the libp2p DHT adapter.
func SetLinkService(service *link.Service) {
	if service != nil {
		linkService = service
	}
}

func gatheredProfile(cfg config.Config, certificate string) link.Profile {
	extras := append(advertise.ListFromEnv(os.Getenv(advertise.EnvVarList)), advertise.PublishedAPIURLs(cfg)...)
	return link.Gather(cfg, link.Extras{
		IPs:               detectLinkIPs(),
		ExtraURLs:         extras,
		Relays:            linkService.Relays(),
		DHTPeerID:         linkService.PeerID(),
		CertificateSHA256: certificate,
	})
}

// HandleIssuePairCode publishes a short-lived single-use code on the DHT.
func HandleIssuePairCode(configPath string) (api.PairCodeResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.PairCodeResponse{}, err
	}
	certificate, err := localCertificateIfNeeded(configPath, cfg)
	if err != nil {
		return api.PairCodeResponse{}, err
	}
	code, expires, err := linkService.Issue(context.Background(), peeridentity.Identity{PublicKey: cfg.Identity.PublicKey, PrivateKey: cfg.Identity.PrivateKey}, gatheredProfile(cfg, certificate))
	if err != nil {
		return api.PairCodeResponse{}, err
	}
	return api.PairCodeResponse{Code: code, ExpiresAt: expires}, nil
}

// HandleRedeemPairCode looks a code up on the DHT, rejects hijacked records,
// and pairs with the authenticated peer, failing over across its addresses.
func HandleRedeemPairCode(ctx context.Context, configPath string, req api.PairCodeRedeemRequest) (api.PairCodeRedeemResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	record, err := linkService.Lookup(ctx, req.Code)
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	certificate, err := localCertificateIfNeeded(configPath, cfg)
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	claim, err := link.SignClaim(peeridentity.Identity{PublicKey: cfg.Identity.PublicKey, PrivateKey: cfg.Identity.PrivateKey}, link.ClaimRequest{
		Code:              req.Code,
		NodeName:          cfg.NodeName,
		APIKey:            cfg.API.Key,
		CertificateSHA256: certificate,
		Profile:           gatheredProfile(cfg, certificate),
	})
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	payload, err := json.Marshal(claim)
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	remoteProfile := record.Profile()
	body, _, err := link.PostJSON(ctx, config.PeerConfig{
		APICertificateSHA256: record.CertificateSHA256,
		Relays:               record.Relays,
		Endpoints:            remoteProfile.Endpoints(""),
	}, "/v1/pair-code/claim", payload)
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	var accepted link.ClaimResponse
	if err := json.Unmarshal(body, &accepted); err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	if accepted.DiscoveryID == "" || accepted.APIKey == "" {
		return api.PairCodeRedeemResponse{}, fmt.Errorf("pairing claim response was incomplete")
	}
	if record.CertificateSHA256 != "" && accepted.CertificateSHA256 != "" && !strings.EqualFold(record.CertificateSHA256, accepted.CertificateSHA256) {
		return api.PairCodeRedeemResponse{}, fmt.Errorf("pairing claim certificate does not match the DHT record")
	}
	merged := link.MergeProfiles(remoteProfile, accepted.Profile)
	if err := configops.UpsertPairedPeer(configPath, config.PeerConfig{
		ID:                   configops.PairedPeerID(accepted.NodeName, accepted.DiscoveryID),
		APIKey:               accepted.APIKey,
		APICertificateSHA256: firstNonEmpty(accepted.CertificateSHA256, record.CertificateSHA256),
		IdentityPublicKey:    accepted.DiscoveryID,
		DHTPeerID:            merged.DHTPeerID,
		Relays:               merged.Relays,
		Endpoints:            merged.Endpoints(""),
	}); err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	return api.PairCodeRedeemResponse{
		Status:            "accepted",
		Message:           "paired by code",
		RemoteDiscoveryID: accepted.DiscoveryID,
		DHTPeerID:         merged.DHTPeerID,
		CertificateSHA256: firstNonEmpty(accepted.CertificateSHA256, record.CertificateSHA256),
		Addresses:         merged.Addresses,
		Relays:            merged.Relays,
	}, nil
}

// HandlePairCodeClaim burns the code and stores the redeemer's reconnect profile.
func HandlePairCodeClaim(configPath string, req link.ClaimRequest) (link.ClaimResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return link.ClaimResponse{}, err
	}
	if req.DiscoveryID == "" || req.DiscoveryID == cfg.Identity.PublicKey {
		return link.ClaimResponse{}, fmt.Errorf("pairing claim discovery id is invalid")
	}
	record, err := linkService.Claim(context.Background(), req)
	if err != nil {
		return link.ClaimResponse{}, err
	}
	certificate, err := localCertificateIfNeeded(configPath, cfg)
	if err != nil {
		return link.ClaimResponse{}, err
	}
	if err := configops.UpsertPairedPeer(configPath, config.PeerConfig{
		ID:                   configops.PairedPeerID(req.NodeName, req.DiscoveryID),
		APIKey:               req.APIKey,
		APICertificateSHA256: req.CertificateSHA256,
		IdentityPublicKey:    req.DiscoveryID,
		DHTPeerID:            req.Profile.DHTPeerID,
		Relays:               req.Profile.Relays,
		Endpoints:            req.Profile.Endpoints(""),
	}); err != nil {
		return link.ClaimResponse{}, err
	}
	local := gatheredProfile(cfg, certificate)
	return link.ClaimResponse{
		DiscoveryID:       cfg.Identity.PublicKey,
		NodeName:          cfg.NodeName,
		APIKey:            cfg.API.Key,
		CertificateSHA256: firstNonEmpty(record.CertificateSHA256, certificate),
		Profile:           link.MergeProfiles(record.Profile(), local),
	}, nil
}

// HandleLinkProfile returns this engine's current reconnect profile.
func HandleLinkProfile(configPath string) (link.Profile, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return link.Profile{}, err
	}
	certificate, err := localCertificateIfNeeded(configPath, cfg)
	if err != nil {
		return link.Profile{}, err
	}
	return gatheredProfile(cfg, certificate), nil
}

// HandleLinkProfilePush merges a signed address refresh from an already paired peer.
func HandleLinkProfilePush(configPath string, push link.LinkPush) (link.Profile, error) {
	if err := link.VerifyPush(push); err != nil {
		return link.Profile{}, err
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return link.Profile{}, err
	}
	existing, ok := peerByDiscoveryID(cfg, push.DiscoveryID)
	if !ok {
		return link.Profile{}, fmt.Errorf("link refresh peer is not paired")
	}
	if err := configops.MergePairedPeerLink(configPath, existing.ID, config.PeerConfig{
		ID:                existing.ID,
		IdentityPublicKey: push.DiscoveryID,
		DHTPeerID:         push.Profile.DHTPeerID,
		Relays:            push.Profile.Relays,
		Endpoints:         push.Profile.Endpoints(""),
	}); err != nil {
		return link.Profile{}, err
	}
	return HandleLinkProfile(configPath)
}

// RefreshPeerLink asks a paired peer for its current addresses and stores them.
func RefreshPeerLink(ctx context.Context, configPath string, peer config.PeerConfig) error {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}
	certificate, err := localCertificateIfNeeded(configPath, cfg)
	if err != nil {
		return err
	}
	push, err := link.SignPush(peeridentity.Identity{PublicKey: cfg.Identity.PublicKey, PrivateKey: cfg.Identity.PrivateKey}, link.LinkPush{
		Profile: gatheredProfile(cfg, certificate),
	})
	if err != nil {
		return err
	}
	payload, err := json.Marshal(push)
	if err != nil {
		return err
	}
	body, _, err := link.PostJSON(ctx, peer, "/v1/link-profile", payload)
	if err != nil {
		return err
	}
	var remote link.Profile
	if err := json.Unmarshal(body, &remote); err != nil {
		return err
	}
	return configops.MergePairedPeerLink(configPath, peer.ID, config.PeerConfig{
		ID:                peer.ID,
		IdentityPublicKey: remote.DiscoveryID,
		DHTPeerID:         remote.DHTPeerID,
		Relays:            remote.Relays,
		Endpoints:         remote.Endpoints(""),
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
