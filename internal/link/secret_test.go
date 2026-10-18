package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"filesyncengine/internal/config"
)

func TestPostSecretDoesNotReachAnUnpinnedHTTPHost(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	_, _, err := PostSecret(context.Background(), config.PeerConfig{
		ID:     "stranger",
		APIKey: "claim-secret",
		Endpoints: []config.EndpointConfig{{
			Kind: "manual", Address: server.URL, NetworkHint: "wan",
		}},
	}, "/v1/pair-code/claim", []byte(`{"code":"secret-claim"}`))
	if err == nil {
		t.Fatal("plain HTTP accepted a claim secret")
	}
	if hits != 0 {
		t.Fatalf("claim was posted %d times before the identity was verified", hits)
	}
}
