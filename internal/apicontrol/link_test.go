package apicontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/link"
	"filesyncengine/internal/pairing"
	"filesyncengine/internal/peeridentity"
)

func TestPairingCodeLookupSkipsHijackAndFailsOver(t *testing.T) {
	publisherKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	redeemerKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	attackerKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	publisherDir := t.TempDir()
	publisherPath := filepath.Join(publisherDir, "config.json")
	if err := writeIdentityConfig(publisherPath, "publisher", publisherKey, "publisher-api"); err != nil {
		t.Fatal(err)
	}
	redeemerDir := t.TempDir()
	redeemerPath := filepath.Join(redeemerDir, "config.json")
	if err := writeIdentityConfig(redeemerPath, "redeemer", redeemerKey, "redeemer-api"); err != nil {
		t.Fatal(err)
	}
	directory := link.NewMemoryDirectory()
	service := link.NewService(directory)
	service.SetPeerID("12D3KooWpublisher")
	service.SetRelays([]string{"http://relay.example:22420"})
	SetLinkService(service)
	t.Cleanup(func() { SetLinkService(link.NewService(link.UnavailableDirectory{})) })

	var claimServer *httptest.Server
	claimServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pair-code/claim" {
			http.NotFound(w, r)
			return
		}
		var claim link.ClaimRequest
		if err := json.NewDecoder(r.Body).Decode(&claim); err != nil {
			t.Errorf("decode claim: %v", err)
			http.Error(w, "bad claim", http.StatusBadRequest)
			return
		}
		response, err := HandlePairCodeClaim(publisherPath, claim)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer claimServer.Close()
	sum := sha256.Sum256(claimServer.Certificate().Raw)
	pin := hex.EncodeToString(sum[:])

	code, _, err := service.Issue(context.Background(), publisherKey, link.Profile{
		DiscoveryID:       publisherKey.PublicKey,
		NodeName:          "publisher",
		CertificateSHA256: pin,
		Addresses: []link.Address{
			{Kind: "manual", Address: "http://127.0.0.1:1", NetworkHint: "local"},
			{Kind: "vpn", Address: "http://127.0.0.1:2", NetworkHint: "vpn_overlay"},
			{Kind: "manual", Address: claimServer.URL, NetworkHint: "wan"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	attackerCode, err := link.NewCode()
	if err != nil {
		t.Fatal(err)
	}
	_, forged, err := link.Seal(attackerCode, attackerKey, link.Profile{
		DiscoveryID: attackerKey.PublicKey,
		NodeName:    "attacker",
		Addresses:   []link.Address{{Kind: "manual", Address: "http://198.51.100.9:22420", NetworkHint: "wan"}},
	}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := directory.Publish(context.Background(), link.Namespace(code), forged); err != nil {
		t.Fatal(err)
	}

	response, err := HandleRedeemPairCode(context.Background(), redeemerPath, api.PairCodeRedeemRequest{Code: code})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message != "paired by code" || response.RemoteDiscoveryID != publisherKey.PublicKey {
		t.Fatalf("redeem = %+v", response)
	}
	if response.DHTPeerID != "12D3KooWpublisher" {
		t.Fatalf("dht id = %s", response.DHTPeerID)
	}
	redeemerCfg, err := config.LoadFile(redeemerPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(redeemerCfg.Peers) != 1 || redeemerCfg.Peers[0].APIKey != "publisher-api" {
		t.Fatalf("redeemer peers = %+v", redeemerCfg.Peers)
	}
	addresses := map[string]string{}
	for _, endpoint := range redeemerCfg.Peers[0].Endpoints {
		addresses[endpoint.Address] = endpoint.NetworkHint
	}
	if addresses["http://127.0.0.1:1"] != "local" || addresses[claimServer.URL] == "" || addresses["http://127.0.0.1:2"] != "vpn_overlay" {
		t.Fatalf("stored addresses = %+v", redeemerCfg.Peers[0].Endpoints)
	}
	if len(redeemerCfg.Peers[0].Relays) != 1 || redeemerCfg.Peers[0].Relays[0] != "http://relay.example:22420" {
		t.Fatalf("relays = %+v", redeemerCfg.Peers[0].Relays)
	}
	publisherCfg, err := config.LoadFile(publisherPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(publisherCfg.Peers) != 1 || publisherCfg.Peers[0].IdentityPublicKey != redeemerKey.PublicKey || publisherCfg.Peers[0].APIKey != "redeemer-api" {
		t.Fatalf("publisher peers = %+v", publisherCfg.Peers)
	}
	if _, err := HandleRedeemPairCode(context.Background(), redeemerPath, api.PairCodeRedeemRequest{Code: code}); err == nil {
		t.Fatal("used code was redeemed again")
	}
}

func TestPairCodeClaimDoesNotWaitForTailscaleOrRelays(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	publisherKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	redeemerKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	publisherDir := t.TempDir()
	publisherPath := filepath.Join(publisherDir, "config.json")
	if err := writeIdentityConfig(publisherPath, "publisher", publisherKey, "publisher-api"); err != nil {
		t.Fatal(err)
	}
	service := link.NewService(link.NewMemoryDirectory())
	service.SetPeerID("12D3KooWpublisher")
	SetLinkService(service)
	t.Cleanup(func() { SetLinkService(link.NewService(link.UnavailableDirectory{})) })
	code, _, err := service.Issue(context.Background(), publisherKey, link.Profile{
		DiscoveryID: publisherKey.PublicKey,
		NodeName:    "publisher",
		Addresses:   []link.Address{{Kind: "manual", Address: "https://publisher.example:22420", NetworkHint: "local"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := link.SignClaim(redeemerKey, link.ClaimRequest{
		Code:     code,
		NodeName: "redeemer",
		APIKey:   "redeemer-api",
		Profile:  link.Profile{DiscoveryID: redeemerKey.PublicKey, NodeName: "redeemer", Addresses: []link.Address{{Kind: "manual", Address: "https://redeemer.example:22420"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	prevTail := claimTailscale
	prevRelays := claimRelays
	claimTailscale = func(context.Context) []string {
		<-release
		return []string{"100.64.0.8"}
	}
	claimRelays = func() []string {
		<-release
		return []string{"/ip4/203.0.113.8/tcp/4001/p2p/12D3KooWrelay"}
	}
	t.Cleanup(func() {
		claimTailscale = prevTail
		claimRelays = prevRelays
	})

	done := make(chan error, 1)
	go func() {
		response, claimErr := HandlePairCodeClaim(publisherPath, claim)
		if claimErr == nil && response.APIKey != "publisher-api" {
			claimErr = errors.New("claim response missing publisher api key")
		}
		done <- claimErr
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("claim waited for tailscale or relay addresses")
	}
}

func TestConcurrentRedeemUsesOneClaim(t *testing.T) {
	publisherKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	redeemerKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	publisherDir := t.TempDir()
	publisherPath := filepath.Join(publisherDir, "config.json")
	if err := writeIdentityConfig(publisherPath, "publisher", publisherKey, "publisher-api"); err != nil {
		t.Fatal(err)
	}
	redeemerDir := t.TempDir()
	redeemerPath := filepath.Join(redeemerDir, "config.json")
	if err := writeIdentityConfig(redeemerPath, "redeemer", redeemerKey, "redeemer-api"); err != nil {
		t.Fatal(err)
	}
	directory := link.NewMemoryDirectory()
	service := link.NewService(directory)
	service.SetPeerID("12D3KooWpublisher")
	SetLinkService(service)
	t.Cleanup(func() { SetLinkService(link.NewService(link.UnavailableDirectory{})) })

	var claims atomic.Int32
	var claimServer *httptest.Server
	claimServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims.Add(1)
		var claim link.ClaimRequest
		if err := json.NewDecoder(r.Body).Decode(&claim); err != nil {
			http.Error(w, "bad claim", http.StatusBadRequest)
			return
		}
		response, err := HandlePairCodeClaim(publisherPath, claim)
		if err != nil {
			http.Error(w, err.Error(), http.StatusGone)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer claimServer.Close()
	sum := sha256.Sum256(claimServer.Certificate().Raw)
	pin := hex.EncodeToString(sum[:])
	code, _, err := service.Issue(context.Background(), publisherKey, link.Profile{
		DiscoveryID:       publisherKey.PublicKey,
		NodeName:          "publisher",
		CertificateSHA256: pin,
		Addresses:         []link.Address{{Kind: "manual", Address: claimServer.URL, NetworkHint: "wan"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, redeemErr := HandleRedeemPairCode(context.Background(), redeemerPath, api.PairCodeRedeemRequest{Code: code})
			if redeemErr != nil {
				errs <- redeemErr
				return
			}
			if response.Message != "paired by code" {
				errs <- fmt.Errorf("redeem = %+v", response)
				return
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := claims.Load(); got != 1 {
		t.Fatalf("claim requests = %d, want 1", got)
	}
}

func TestManualLinkPersistsReconnectProfile(t *testing.T) {
	previousIPs := detectLinkIPs
	detectLinkIPs = func() []string { return []string{"10.1.1.5", "100.64.1.9", "203.0.113.20"} }
	t.Cleanup(func() { detectLinkIPs = previousIPs })
	service := link.NewService(link.NewMemoryDirectory())
	service.SetPeerID("12D3KooWimporter")
	service.SetRelays([]string{"http://relay.example:22420"})
	SetLinkService(service)
	t.Cleanup(func() { SetLinkService(link.NewService(link.UnavailableDirectory{})) })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"nodeName":"node-b",
		"api":{"listen":"127.0.0.1:22420","key":"local-api","advertiseURL":"http://127.0.0.1:22420"},
		"identity":{"privateKey":"local-private","publicKey":"local-public","encryptionLevel":4},
		"peers":[],
		"folders":[]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotOffer pairing.PairOffer
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotOffer); err != nil {
			t.Errorf("decode offer: %v", err)
		}
		_ = json.NewEncoder(w).Encode(pairing.PairAcceptance{
			DiscoveryID:          "remote-public",
			NodeName:             "node-a",
			APIKey:               "remote-api",
			APICertificateSHA256: strings.Repeat("ab", 32),
			LinkProfile: link.Profile{
				DiscoveryID:       "remote-public",
				NodeName:          "node-a",
				DHTPeerID:         "12D3KooWremote",
				CertificateSHA256: strings.Repeat("ab", 32),
				Relays:            []string{"http://remote-relay.example:22420"},
				Addresses: []link.Address{
					{Kind: "manual", Address: "http://10.9.9.9:22420", NetworkHint: "local"},
					{Kind: "vpn", Address: "http://100.64.9.9:22420", NetworkHint: "vpn_overlay"},
					{Kind: "manual", Address: "http://203.0.113.50:22420", NetworkHint: "wan"},
				},
			},
		})
	}))
	defer server.Close()

	if _, err := HandleIdentityImport(configPath, api.IdentityImportRequest{Package: pairing.IdentityPackage{
		Version:                    pairing.IdentityPackageVersion,
		NodeName:                   "node-a",
		DiscoveryID:                "remote-public",
		GroupID:                    "primary",
		BootstrapProofKey:          strings.Repeat("q", 43),
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
		ReachableAPIURL:            server.URL,
		APICertificateSHA256:       certPin(server.Certificate().Raw),
	}}); err != nil {
		t.Fatal(err)
	}
	offerAddresses := map[string]string{}
	for _, address := range gotOffer.LinkProfile.Addresses {
		offerAddresses[address.Address] = address.NetworkHint
	}
	if offerAddresses["http://10.1.1.5:22420"] != "local" || offerAddresses["http://100.64.1.9:22420"] != "vpn_overlay" {
		t.Fatalf("offer profile = %+v", gotOffer.LinkProfile)
	}
	if _, ok := offerAddresses["http://203.0.113.20:22420"]; ok {
		t.Fatalf("unverified public address was offered: %+v", gotOffer.LinkProfile)
	}
	if gotOffer.LinkProfile.DHTPeerID != "12D3KooWimporter" || len(gotOffer.LinkProfile.Relays) != 1 {
		t.Fatalf("offer identity = %+v", gotOffer.LinkProfile)
	}
	saved, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Peers) != 1 || saved.Peers[0].DHTPeerID != "12D3KooWremote" || saved.Peers[0].APICertificateSHA256 != strings.Repeat("ab", 32) {
		t.Fatalf("saved peer = %+v", saved.Peers)
	}
	savedAddresses := map[string]struct{}{}
	for _, endpoint := range saved.Peers[0].Endpoints {
		savedAddresses[endpoint.Address] = struct{}{}
	}
	for _, want := range []string{server.URL, "http://10.9.9.9:22420", "http://100.64.9.9:22420", "http://203.0.113.50:22420"} {
		if _, ok := savedAddresses[want]; !ok {
			t.Fatalf("missing %s in %+v", want, saved.Peers[0].Endpoints)
		}
	}
	if len(saved.Peers[0].Relays) != 1 || saved.Peers[0].Relays[0] != "http://remote-relay.example:22420" {
		t.Fatalf("relay was stored as an endpoint: endpoints=%+v relays=%v", saved.Peers[0].Endpoints, saved.Peers[0].Relays)
	}
}

func TestLinkRefreshMergesReplacementAddress(t *testing.T) {
	publisherKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	redeemerKey, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	publisherPath := filepath.Join(t.TempDir(), "config.json")
	redeemerPath := filepath.Join(t.TempDir(), "config.json")
	if err := writeIdentityConfig(publisherPath, "publisher", publisherKey, "publisher-api"); err != nil {
		t.Fatal(err)
	}
	if err := writeIdentityConfig(redeemerPath, "redeemer", redeemerKey, "redeemer-api"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publisherPath, []byte(mustPeerConfig(publisherKey, "publisher", "publisher-api", redeemerKey.PublicKey, "redeemer-api", "http://10.0.0.5:22420")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(redeemerPath, []byte(mustPeerConfig(redeemerKey, "redeemer", "redeemer-api", publisherKey.PublicKey, "publisher-api", "http://10.0.0.4:22420")), 0o600); err != nil {
		t.Fatal(err)
	}
	previousIPs := detectLinkIPs
	detectLinkIPs = func() []string { return []string{"10.0.0.4", "100.70.0.4"} }
	t.Cleanup(func() { detectLinkIPs = previousIPs })

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var push link.LinkPush
		if err := json.NewDecoder(r.Body).Decode(&push); err != nil {
			t.Errorf("decode push: %v", err)
			http.Error(w, "bad push", http.StatusBadRequest)
			return
		}
		profile, err := HandleLinkProfilePush(publisherPath, push)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(profile)
	}))
	defer server.Close()

	cfg, err := config.LoadFile(redeemerPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Peers[0].APICertificateSHA256 = certPin(server.Certificate().Raw)
	cfg.Peers[0].Endpoints = []config.EndpointConfig{
		{Kind: "manual", Address: "https://127.0.0.1:1", NetworkHint: "local", Authenticated: true},
		{Kind: "manual", Address: server.URL, NetworkHint: "wan", Authenticated: true},
	}
	if err := saveTestConfig(redeemerPath, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.LoadFile(redeemerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := RefreshPeerLink(context.Background(), redeemerPath, reloaded.Peers[0]); err != nil {
		t.Fatal(err)
	}
	publisherCfg, err := config.LoadFile(publisherPath)
	if err != nil {
		t.Fatal(err)
	}
	foundTailscale := false
	for _, endpoint := range publisherCfg.Peers[0].Endpoints {
		if endpoint.Address == "http://100.70.0.4:22420" && endpoint.NetworkHint == "vpn_overlay" {
			foundTailscale = true
		}
	}
	if !foundTailscale {
		t.Fatalf("publisher did not learn the new address: %+v", publisherCfg.Peers[0].Endpoints)
	}
}

func writeIdentityConfig(path, name string, identity peeridentity.Identity, apiKey string) error {
	cfg := config.Config{
		NodeName: name,
		API:      config.APIConfig{Listen: "127.0.0.1:22420", Key: apiKey, AdvertiseURL: "http://10.0.0.8:22420"},
		Identity: config.IdentityConfig{PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey, EncryptionLevel: 4},
	}
	return saveTestConfig(path, cfg)
}

func saveTestConfig(path string, cfg config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func mustPeerConfig(self peeridentity.Identity, name, apiKey, peerKey, peerAPI, peerURL string) string {
	cfg := config.Config{
		NodeName: name,
		API:      config.APIConfig{Listen: "127.0.0.1:22420", Key: apiKey, AdvertiseURL: "http://10.0.0.4:22420"},
		Identity: config.IdentityConfig{PublicKey: self.PublicKey, PrivateKey: self.PrivateKey, EncryptionLevel: 4},
		Peers: []config.PeerConfig{{
			ID:                "peer",
			APIKey:            peerAPI,
			IdentityPublicKey: peerKey,
			Endpoints:         []config.EndpointConfig{{Kind: "manual", Address: peerURL}},
		}},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return string(data)
}

func certPin(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
