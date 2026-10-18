package apicontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configops"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/link"
	"filesyncengine/internal/peeridentity"
	"filesyncengine/internal/structuredlog"
)

var (
	linkService    = link.NewService(link.UnavailableDirectory{})
	detectLinkIPs  = link.LocalIPs
	claimTailscale = advertise.TailscaleHosts
	claimRelays    = discovery.HelperRelayAddrs
	redeemInflight redeemFlight
)

// SetLinkService installs the process pairing-code directory. Tests use a
// memory directory; the daemon installs the libp2p DHT adapter.
// SweepPairingCodes unpublishes redeemed and expired pairing records.
func SweepPairingCodes(ctx context.Context) {
	linkService.Sweep(ctx)
}

func SetLinkService(service *link.Service) {
	if service != nil {
		linkService = service
	}
}

// ServedPairingPayload is the sealed record for an unauthenticated API fetch.
// The redeemer still opens it with the pairing secret.
func ServedPairingPayload(namespace string) ([]byte, error) {
	payload, ok := linkService.ServedPayload(namespace)
	if !ok || len(payload) == 0 {
		return nil, errors.New("pairing code was not found")
	}
	return payload, nil
}

func gatheredProfile(cfg config.Config, certificate string) link.Profile {
	extras := append(advertise.ListFromEnv(os.Getenv(advertise.EnvVarList)), advertise.PublishedAPIURLs(cfg)...)
	extras = append(extras, advertise.ProfileURLs(cfg)...)
	ips := detectLinkIPs()
	if !advertise.ExcludeTailscale() {
		ips = append(ips, claimTailscale(context.Background())...)
	}
	relays := append(append([]string{}, linkService.Relays()...), claimRelays()...)
	return link.Gather(cfg, link.Extras{
		IPs:               ips,
		ExtraURLs:         extras,
		Relays:            relays,
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
	ttl, err := cfg.Discovery.PairingCodeLifetime()
	if err != nil {
		return api.PairCodeResponse{}, err
	}
	linkService.SetTTL(ttl)
	code, expires, err := linkService.Issue(context.Background(), peeridentity.Identity{PublicKey: cfg.Identity.PublicKey, PrivateKey: cfg.Identity.PrivateKey}, gatheredProfile(cfg, certificate))
	if err != nil {
		structuredlog.Event("error", "pairing.publish.failed", "pairing code was not published", map[string]any{"error": err.Error()})
		return api.PairCodeResponse{}, err
	}
	structuredlog.Event("info", "pairing.publish.ok", "pairing code published", map[string]any{"expiresAt": expires.UTC().Format(time.RFC3339)})
	return api.PairCodeResponse{Code: code, ExpiresAt: expires}, nil
}

// claimProfile is the reconnect profile sent with a pairing claim.
// It uses the configured advertise URL, the DHT peer id, and relays already
// in memory. Tailscale queries and host.Addrs() dial public addresses back,
// and that work has to stay off the claim round trip.
func claimProfile(cfg config.Config, certificate string) link.Profile {
	return link.Gather(cfg, link.Extras{
		Relays:            append([]string{}, linkService.Relays()...),
		DHTPeerID:         linkService.PeerID(),
		CertificateSHA256: certificate,
	})
}

// HandleRedeemPairCode looks a code up on the DHT, rejects hijacked records,
// and pairs with the authenticated peer, failing over across its addresses.
func HandleRedeemPairCode(ctx context.Context, configPath string, req api.PairCodeRedeemRequest) (api.PairCodeRedeemResponse, error) {
	code := link.NormalizeCode(req.Code)
	if code == "" {
		code = req.Code
	}
	return redeemInflight.do(code, func() (api.PairCodeRedeemResponse, error) {
		return redeemPairCode(ctx, configPath, req)
	})
}

func redeemPairCode(ctx context.Context, configPath string, req api.PairCodeRedeemRequest) (api.PairCodeRedeemResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	record, err := linkService.Lookup(ctx, req.Code)
	if err != nil {
		structuredlog.Event("warn", "pairing.lookup.failed", "pairing code lookup failed", map[string]any{"error": err.Error()})
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
		Profile:           claimProfile(cfg, certificate),
	})
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	payload, err := json.Marshal(claim)
	if err != nil {
		return api.PairCodeRedeemResponse{}, err
	}
	remoteProfile := record.Profile()
	// The claim itself is control traffic. A missed hole punch must not keep
	// the GUI on "Linking" while public relays are dialed for file bytes.
	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dialCancel()
	body, _, err := link.PostSecret(dialCtx, config.PeerConfig{
		APICertificateSHA256: record.CertificateSHA256,
		DHTPeerID:            remoteProfile.DHTPeerID,
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
	local := claimProfile(cfg, certificate)
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

// redeemFlight runs one redeem per pairing code. A GUI poll and the accept
// request can both see the same pending command. The second waits and uses
// the first result instead of burning the code twice.
type redeemFlight struct {
	mu    sync.Mutex
	calls map[string]*redeemCall
}

type redeemCall struct {
	done chan struct{}
	resp api.PairCodeRedeemResponse
	err  error
}

func (f *redeemFlight) do(code string, fn func() (api.PairCodeRedeemResponse, error)) (api.PairCodeRedeemResponse, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]*redeemCall{}
	}
	if call, ok := f.calls[code]; ok {
		f.mu.Unlock()
		<-call.done
		return call.resp, call.err
	}
	call := &redeemCall{done: make(chan struct{})}
	f.calls[code] = call
	f.mu.Unlock()
	defer func() {
		close(call.done)
		f.mu.Lock()
		delete(f.calls, code)
		f.mu.Unlock()
	}()
	call.resp, call.err = fn()
	return call.resp, call.err
}
