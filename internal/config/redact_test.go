package config

import (
	"strings"
	"testing"
)

func TestRedactedJSONHidesAPIKey(t *testing.T) {
	cfg := Config{NodeName: "node-a", API: APIConfig{Listen: "127.0.0.1:22420", Key: "secret-key"}, Identity: IdentityConfig{PrivateKey: "node-private-key", PublicKey: "node-public-key", EncryptionLevel: 5, Groups: []IdentityGroupConfig{{ID: "family-sync", Token: "identity-group-secret-token", Enabled: true}}}, Peers: []PeerConfig{{ID: "peer-a", APIKey: "peer-secret"}}, Folders: []FolderConfig{{ID: "docs", Path: "/tmp/docs", Mode: ModeSendReceive, BlockSize: DefaultBlockSize}}}
	data, err := RedactedJSON(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "secret-key") {
		t.Fatalf("redacted config leaked api key: %s", text)
	}
	if strings.Contains(text, "peer-secret") {
		t.Fatalf("redacted config leaked peer api key: %s", text)
	}
	if strings.Contains(text, "node-private-key") {
		t.Fatalf("redacted config leaked node private key: %s", text)
	}
	if strings.Contains(text, "identity-group-secret-token") {
		t.Fatalf("redacted config leaked identity group token: %s", text)
	}
	if !strings.Contains(text, "node-public-key") {
		t.Fatalf("redacted config should keep public key visible: %s", text)
	}
	if !strings.Contains(text, `"key": "[REDACTED]"`) {
		t.Fatalf("redacted config missing placeholder: %s", text)
	}
}
