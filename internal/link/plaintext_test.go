package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

// TestNoPlaintextAppPayloadLeavesOnAnyPath listens on raw TCP and on a TLS
// server with the wrong certificate. Sync, API, and pairing payloads must not
// appear on either one, including when the address came from the DHT or a relay.
func TestNoPlaintextAppPayloadLeavesOnAnyPath(t *testing.T) {
	discovery.ResetCatalog()
	t.Cleanup(discovery.ResetCatalog)

	const (
		marker = "FSE-PLAINTEXT-MARKER"
		apiKey = "FSE-API-KEY-MARKER"
		ours   = "12D3KooWOursPeerID"
	)
	payload := []byte(`{"claim":"` + marker + `","bytes":"secret-file"}`)

	var mu sync.Mutex
	var wire strings.Builder
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf, _ := io.ReadAll(io.LimitReader(conn, 1<<20))
				mu.Lock()
				wire.Write(buf)
				mu.Unlock()
			}(conn)
		}
	}()

	var tlsHits int
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tlsHits++
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		mu.Lock()
		wire.WriteString(r.Header.Get("X-FSE-API-Key"))
		wire.Write(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer tlsServer.Close()

	plain := "http://" + ln.Addr().String()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	circuit := "/ip4/127.0.0.1/tcp/" + port + "/p2p/" + ours
	discovery.NoteDHTPeers([]discovery.Peer{{
		ID:        ours,
		Addresses: []string{plain, circuit, tlsServer.URL},
	}})

	peer := config.PeerConfig{
		ID:                   "ours",
		DHTPeerID:            ours,
		APIKey:               apiKey,
		IdentityPublicKey:    "ours-identity",
		APICertificateSHA256: hex.EncodeToString(sha256.New().Sum(nil)),
		Endpoints: []config.EndpointConfig{
			{Kind: "manual", Address: plain, NetworkHint: "local", Authenticated: true},
			{Kind: "manual", Address: tlsServer.URL, NetworkHint: "wan", Authenticated: true},
		},
		Relays: []string{plain, circuit},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, _, _ = PostJSON(ctx, peer, "/v1/sync-journal", payload)
	_, _, _ = PostSecret(ctx, peer, "/v1/pair-code/claim", payload)
	_, _ = Dial(ctx, peer, func(ctx context.Context, base string, client *http.Client) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/folder-block", strings.NewReader(string(payload)))
		if err != nil {
			return err
		}
		req.Header.Set("X-FSE-API-Key", apiKey)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	})

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := wire.String()
		mu.Unlock()
		if strings.Contains(got, marker) || strings.Contains(got, apiKey) {
			t.Fatalf("plaintext application payload left on the wire: %q", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	got := wire.String()
	mu.Unlock()
	if strings.Contains(got, marker) || strings.Contains(got, apiKey) || tlsHits != 0 {
		t.Fatalf("plaintext application payload left on a path: hits=%d wire=%q", tlsHits, got)
	}
}
