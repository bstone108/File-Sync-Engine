package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestHostnameAPIIsTriedBeforeRelay(t *testing.T) {
	peer := config.PeerConfig{
		ID:        "fse-a",
		DHTPeerID: "12D3KooWPeer",
		Relays:    []string{"/ip4/10.200.0.1/tcp/4001/p2p/12D3KooWRelay/p2p-circuit"},
		Endpoints: []config.EndpointConfig{{
			Kind: "manual", Address: "https://fse-a:22420", NetworkHint: "wan", Authenticated: true,
		}},
	}
	stages := endpointStages(peer)
	if len(stages) <= libp2pStage || len(stages[0]) != 1 || !strings.Contains(stages[0][0].Address, "fse-a") {
		t.Fatalf("hostname was not in the preferred stage: %+v", stages)
	}
	foundWAN := false
	for i, stage := range stages {
		if i <= libp2pStage {
			continue
		}
		for _, endpoint := range stage {
			if strings.Contains(endpoint.Address, "203.0.113.9") {
				foundWAN = true
			}
			if strings.Contains(endpoint.Address, "fse-a") {
				t.Fatalf("hostname fell through to stage %d", i)
			}
		}
	}
	peer.Endpoints = append(peer.Endpoints, config.EndpointConfig{
		Kind: "manual", Address: "https://203.0.113.9:22420", NetworkHint: "wan",
	})
	stages = endpointStages(peer)
	for _, stage := range stages {
		for _, endpoint := range stage {
			if strings.Contains(endpoint.Address, "203.0.113.9") {
				t.Fatal("unauthenticated public IP was dialed")
			}
		}
	}
	peer.Endpoints[1].Authenticated = true
	stages = endpointStages(peer)
	for _, endpoint := range stages[0] {
		if strings.Contains(endpoint.Address, "203.0.113.9") {
			t.Fatal("public IP was preferred ahead of libp2p")
		}
	}
	for i, stage := range stages {
		if i <= libp2pStage {
			continue
		}
		for _, endpoint := range stage {
			if strings.Contains(endpoint.Address, "203.0.113.9") {
				foundWAN = true
			}
		}
	}
	if !foundWAN {
		t.Fatalf("authenticated public IP was dropped: %+v", stages)
	}
}

func TestDialFailsOverWhenFirstAddressDies(t *testing.T) {
	live := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sync-journal" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[{"path":"notes.txt"}]}`))
	}))
	defer live.Close()
	sum := sha256.Sum256(live.Certificate().Raw)
	peer := config.PeerConfig{
		ID:                   "peer-a",
		APIKey:               "secret",
		IdentityPublicKey:    "ours",
		APICertificateSHA256: hex.EncodeToString(sum[:]),
		Endpoints: []config.EndpointConfig{
			{Kind: "manual", Address: "https://127.0.0.1:1", NetworkHint: "local", Authenticated: true},
			{Kind: "manual", Address: live.URL, NetworkHint: "wan", Authenticated: true},
		},
	}
	var used string
	body := ""
	got, err := Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/sync-journal", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		buf := make([]byte, 128)
		n, _ := resp.Body.Read(buf)
		body = string(buf[:n])
		used = base
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != live.URL || used != live.URL {
		t.Fatalf("used %q, dial returned %q", used, got)
	}
	if !strings.Contains(body, "notes.txt") {
		t.Fatalf("body = %s", body)
	}
}
