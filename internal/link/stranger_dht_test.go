package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

func TestStrangerDHTAddressIsNeverPersistedOrSentData(t *testing.T) {
	discovery.ResetCatalog()
	t.Cleanup(discovery.ResetCatalog)
	var hits atomic.Int32
	stranger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "stranger", http.StatusOK)
	}))
	defer stranger.Close()
	const (
		ours = "12D3KooWOursPeerID"
		lan  = "https://10.1.1.5:22420"
	)
	discovery.NoteDHTPeers([]discovery.Peer{
		{ID: "12D3KooWStranger", Addresses: []string{stranger.URL, "/ip4/203.0.113.8/tcp/4001/p2p/12D3KooWStranger"}},
		{ID: ours, Addresses: []string{stranger.URL, "/ip4/203.0.113.8/tcp/4001/p2p/12D3KooWStranger"}},
	})

	if KeepStoredEndpoint(config.EndpointConfig{Kind: "manual", Address: stranger.URL, Authenticated: true}, ours) {
		t.Fatal("stranger DHT address was accepted for storage")
	}
	peer := config.PeerConfig{
		ID:                "ours",
		DHTPeerID:         ours,
		APIKey:            "remote-key",
		IdentityPublicKey: "remote-public",
		Relays:            []string{"/ip4/127.0.0.1/tcp/1/p2p/" + ours},
		Endpoints: []config.EndpointConfig{{
			Kind: "manual", Address: lan, Authenticated: true,
		}},
	}
	for _, stage := range endpointStages(peer) {
		for _, endpoint := range stage {
			if strings.Contains(endpoint.Address, stranger.URL) || strings.Contains(endpoint.Address, "203.0.113.8") {
				t.Fatalf("stranger address was selected for dial: %+v", stage)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Dial(ctx, peer, func(_ context.Context, base string, client *http.Client) error {
		if strings.Contains(base, "203.0.113.8") || strings.Contains(base, stranger.Listener.Addr().String()) {
			t.Errorf("application data was addressed to %s", base)
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/sync-journal", nil)
		if reqErr != nil {
			return reqErr
		}
		req.Header.Set("X-FSE-API-Key", peer.APIKey)
		resp, reqErr := client.Do(req)
		if reqErr != nil {
			return reqErr
		}
		_ = resp.Body.Close()
		return nil
	})
	if err == nil {
		t.Fatal("dial succeeded without an authenticated peer address")
	}
	if hits.Load() != 0 {
		t.Fatalf("stranger received %d requests", hits.Load())
	}
}

// A signed profile can name the same URL the DHT advertised. That address is
// still the pinned HTTPS path. Skipping it sends the claim down a relay and
// the code is burned while the client is still waiting.
func TestSignedAddressStaysDialableWhenDHTAlsoAdvertisedIt(t *testing.T) {
	discovery.ResetCatalog()
	t.Cleanup(discovery.ResetCatalog)
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("X-FSE-API-Key") == "" {
			t.Error("claim was sent without the peer API key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	sum := sha256.Sum256(server.Certificate().Raw)
	pin := hex.EncodeToString(sum[:])
	const ours = "12D3KooWOursPeerID"
	discovery.NoteDHTPeers([]discovery.Peer{{
		ID:        ours,
		Addresses: []string{server.URL, "/ip4/203.0.113.8/tcp/4001/p2p/" + ours},
	}})
	if !discovery.PendingDHTAddress(server.URL) {
		t.Fatal("DHT advertisement was not pending")
	}
	endpoint := config.EndpointConfig{Kind: "manual", Address: server.URL, NetworkHint: "wan", Authenticated: true}
	if KeepStoredEndpoint(endpoint, ours) {
		t.Fatal("pending DHT address was stored before the session authenticated")
	}
	peer := config.PeerConfig{
		ID:                   "publisher",
		DHTPeerID:            ours,
		APIKey:               "publisher-key",
		APICertificateSHA256: pin,
		IdentityPublicKey:    "publisher-public",
		Endpoints:            []config.EndpointConfig{endpoint},
	}
	stages := endpointStages(peer)
	found := false
	for _, stage := range stages {
		for _, candidate := range stage {
			if candidate.Address == server.URL {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("signed HTTPS address was dropped because the DHT advertised it: %+v", stages)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	used, err := Dial(ctx, peer, func(ctx context.Context, base string, client *http.Client) error {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/pair-code/claim", strings.NewReader(`{"code":"ABCD-EFGH-IJKL-MNOP"}`))
		if reqErr != nil {
			return reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-FSE-API-Key", peer.APIKey)
		resp, reqErr := client.Do(req)
		if reqErr != nil {
			return reqErr
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status %s", resp.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if used != server.URL {
		t.Fatalf("dial used %q", used)
	}
	if hits.Load() != 1 {
		t.Fatalf("signed peer received %d requests", hits.Load())
	}
	if discovery.PendingDHTAddress(server.URL) {
		t.Fatal("authenticated session left the address pending")
	}
	if !KeepStoredEndpoint(endpoint, ours) {
		t.Fatal("authenticated session could not be stored")
	}
}
