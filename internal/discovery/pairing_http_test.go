package discovery

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func TestFetchPairingUsesAdvertisedAPIWhenStreamDialFails(t *testing.T) {
	namespace := "filesyncengine/pair/v1/http-fallback"
	payload := []byte("sealed-record")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pair-code/record" || r.URL.Query().Get("namespace") != namespace {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	apiAddr := apiMultiaddr(server.URL)
	if apiAddr == nil {
		t.Fatalf("api multiaddr for %s", server.URL)
	}
	dead, err := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/1")
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	looker, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "looker"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = looker.Close() })
	got, err := looker.FetchPairing(ctx, peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{dead, apiAddr}}, namespace)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %q", got)
	}
	if _, err := looker.FetchPairing(ctx, peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{dead}}, namespace); err == nil {
		t.Fatal("dead dial without an API URL succeeded")
	}
}
