package foldershare

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"filesyncengine/internal/config"
)

func pinnedServer(t *testing.T, handler http.Handler) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	sum := sha256.Sum256(server.Certificate().Raw)
	return server, hex.EncodeToString(sum[:])
}

func pinnedPeer(id, apiKey, address, pin string) config.PeerConfig {
	return config.PeerConfig{
		ID:                   id,
		APIKey:               apiKey,
		IdentityPublicKey:    id + "-identity",
		APICertificateSHA256: pin,
		Endpoints: []config.EndpointConfig{{
			Kind: "manual", Address: address, NetworkHint: "local", Authenticated: true,
		}},
	}
}
