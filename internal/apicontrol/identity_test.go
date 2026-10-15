package apicontrol

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/pairing"
)

func TestHandleIdentityPackageBuildsPackageFromCurrentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	groupToken := strings.Repeat("z", 80)
	if err := os.WriteFile(configPath, []byte(`{
		"nodeName":"node-a",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret"},
		"identity":{"privateKey":"private-secret","publicKey":"public-discovery-key","encryptionLevel":4,"groups":[{"id":"family-sync","token":"`+groupToken+`","enabled":true}]},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	pkg, err := HandleIdentityPackage(configPath, api.IdentityPackageRequest{GroupID: "family-sync"})
	if err != nil {
		t.Fatalf("HandleIdentityPackage returned error: %v", err)
	}
	if pkg.DiscoveryID != "public-discovery-key" || pkg.GroupID != "family-sync" || pkg.BootstrapProofKey != groupToken {
		t.Fatalf("unexpected identity package: %+v", pkg)
	}
	if pkg.BootstrapEncryptionLevel != 10 || pkg.DefaultPeerEncryptionLevel != 4 {
		t.Fatalf("unexpected identity package levels: %+v", pkg)
	}
}

func TestHandleIdentityImportValidatesPackageAndCreatesRedactedPeerPairMaterial(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	groupToken := strings.Repeat("z", 80)
	if err := os.WriteFile(configPath, []byte(`{
		"nodeName":"node-b",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret"},
		"identity":{"privateKey":"private-secret","publicKey":"local-public","encryptionLevel":4,"groups":[{"id":"family-sync","token":"`+groupToken+`","enabled":true}]},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	response, err := HandleIdentityImport(configPath, api.IdentityImportRequest{Package: pairing.IdentityPackage{
		Version:                    pairing.IdentityPackageVersion,
		DiscoveryID:                "remote-public",
		GroupID:                    "family-sync",
		BootstrapProofKey:          groupToken,
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
	}})
	if err != nil {
		t.Fatalf("HandleIdentityImport returned error: %v", err)
	}
	if response.Status != "accepted" || response.GroupID != "family-sync" || response.RemoteDiscoveryID != "remote-public" {
		t.Fatalf("unexpected identity import response: %+v", response)
	}
	if response.IntroductionEncryptionLevel != 10 || response.PeerPairEncryptionLevel != 4 || !response.RequiresDedicatedPeerPairKey || response.UsesBootstrapKeyForTraffic {
		t.Fatalf("unexpected identity import levels/safety flags: %+v", response)
	}
	if response.PairID == "" || response.KeyID == "" {
		t.Fatalf("identity import should create redacted peer-pair key identifiers: %+v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	for _, leaked := range []string{groupToken, "bootstrapProofKey", "secretKey", "private-secret", "api-secret"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("identity import response leaked secret %q: %s", leaked, encoded)
		}
	}
}

func TestIdentityImportUsesEngineAdvertiseURLAndKeepsLocalPrimary(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"nodeName":"node-b",
		"api":{"listen":"0.0.0.0:22420","key":"local-api","advertiseURL":"https://node-b.example:22420"},
		"identity":{"privateKey":"local-private","publicKey":"local-public","encryptionLevel":4},
		"peers":[],
		"folders":[]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		t.Fatal(err)
	}
	var gotOffer pairing.PairOffer
	rejectExchange := false
	exchangeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchangeCalls++
		if rejectExchange {
			http.Error(w, "exchange should not run", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/pair-exchange" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotOffer); err != nil {
			t.Errorf("decode offer: %v", err)
		}
		_ = json.NewEncoder(w).Encode(pairing.PairAcceptance{DiscoveryID: "remote-public", NodeName: "node-a", APIKey: "remote-api", APICertificateSHA256: strings.Repeat("ab", 32)})
	}))
	defer server.Close()

	response, err := HandleIdentityImport(configPath, api.IdentityImportRequest{Package: pairing.IdentityPackage{
		Version:                    pairing.IdentityPackageVersion,
		NodeName:                   "node-a",
		DiscoveryID:                "remote-public",
		GroupID:                    "primary",
		BootstrapProofKey:          strings.Repeat("q", 43),
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
		ReachableAPIURL:            server.URL,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message == "already paired with this engine" {
		t.Fatal("first import should exchange, not report already paired")
	}
	if gotOffer.ReachableAPIURL != "https://node-b.example:22420" {
		t.Fatalf("exchange advertised %q", gotOffer.ReachableAPIURL)
	}
	reloaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Peers) != 1 || reloaded.Peers[0].IdentityPublicKey != "remote-public" || reloaded.Peers[0].APIKey != "remote-api" {
		t.Fatalf("peer not stored: %+v", reloaded.Peers)
	}

	rejectExchange = true
	exchangeCalls = 0
	again, err := HandleIdentityImport(configPath, api.IdentityImportRequest{Package: pairing.IdentityPackage{
		Version:                    pairing.IdentityPackageVersion,
		DiscoveryID:                "remote-public",
		GroupID:                    "primary",
		BootstrapProofKey:          strings.Repeat("q", 43),
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
		ReachableAPIURL:            "http://127.0.0.1:1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Message != "already paired with this engine" {
		t.Fatalf("second import = %+v", again)
	}
	if exchangeCalls != 0 {
		t.Fatalf("already-paired import called pair exchange %d times", exchangeCalls)
	}
}

func TestPairExchangeIsIdempotentWhenAlreadyPaired(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"nodeName":"node-a",
		"api":{"listen":"0.0.0.0:22420","key":"local-api"},
		"identity":{"privateKey":"local-private","publicKey":"local-public","encryptionLevel":4,"groups":[{"id":"primary","token":"`+strings.Repeat("z", 43)+`","enabled":true}]},
		"peers":[{"id":"node-b","apiKey":"remote-api","identityPublicKey":"remote-public","endpoints":[{"kind":"manual","address":"https://node-b.example:22420"}]}],
		"folders":[]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		t.Fatal(err)
	}
	response, err := HandlePairExchange(configPath, api.PairExchangeRequest{
		GroupID:           "primary",
		BootstrapProofKey: "not-the-current-token",
		DiscoveryID:       "remote-public",
		NodeName:          "node-b",
		ReachableAPIURL:   "https://node-b.example:22420",
		APIKey:            "remote-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.APIKey != "local-api" || response.DiscoveryID != "local-public" {
		t.Fatalf("idempotent exchange = %+v", response)
	}
	reloaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Peers) != 1 || reloaded.Peers[0].APIKey != "remote-api" {
		t.Fatalf("already-paired exchange changed peers: %+v", reloaded.Peers)
	}
}
