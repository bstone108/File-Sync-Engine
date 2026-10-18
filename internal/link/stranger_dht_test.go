package link

import (
	"context"
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
