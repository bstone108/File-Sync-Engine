package apicontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestHandleAPITrustStatusReportsPinnedCertificateMatch(t *testing.T) {
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
	fingerprint, err := CertificateFingerprintSHA256(certPEM)
	if err != nil {
		t.Fatalf("fingerprint generated certificate: %v", err)
	}
	cfg.API.Encryption.TrustedCertificateSHA256 = fingerprint
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	response, err := HandleAPITrustStatus(configPath)
	if err != nil {
		t.Fatalf("HandleAPITrustStatus: %v", err)
	}
	if response.Mode != string(config.APIEncryptionAuto) || !response.TLSEnabled || !response.TLSRequired {
		t.Fatalf("unexpected trust transport status: %+v", response)
	}
	if response.CertificateSHA256 != fingerprint || response.TrustedCertificateSHA256 != fingerprint || !response.TrustedCertificateConfigured || !response.TrustedCertificateMatches {
		t.Fatalf("unexpected certificate pinning status: %+v", response)
	}
}
