package apicontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
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
