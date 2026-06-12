package apicontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
)

func TestHandleAPITrustCommandPinsActiveCertificateFingerprint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		NodeName: "node-a",
		API: config.APIConfig{
			Listen: "0.0.0.0:22420",
			Key:    "secret-key",
			Encryption: config.APIEncryptionConfig{
				Mode: config.APIEncryptionAuto,
			},
		},
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		t.Fatalf("ensure API TLS assets: %v", err)
	}
	certPEM, err := os.ReadFile(cfg.API.Encryption.CertFile)
	if err != nil {
		t.Fatalf("read generated certificate: %v", err)
	}
	fingerprint, err := certificateFingerprintSHA256ForTest(certPEM)
	if err != nil {
		t.Fatalf("fingerprint generated certificate: %v", err)
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	response, err := HandleAPITrustCommand(configPath, api.APITrustCommandRequest{Action: "pin-active-certificate"})
	if err != nil {
		t.Fatalf("HandleAPITrustCommand: %v", err)
	}
	if response.Action != "pin-active-certificate" || response.Status != "accepted" || response.CertificateSHA256 != fingerprint || !response.TrustedCertificateConfigured || !response.TrustedCertificateMatches {
		t.Fatalf("unexpected trust command response: %+v", response)
	}
	updated, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	if updated.API.Encryption.TrustedCertificateSHA256 != fingerprint {
		t.Fatalf("active API certificate fingerprint was not pinned: %+v", updated.API.Encryption)
	}
	if updated.API.Key != "secret-key" {
		t.Fatalf("API key was not preserved")
	}
}

func certificateFingerprintSHA256ForTest(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("test certificate did not contain a PEM certificate")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}
