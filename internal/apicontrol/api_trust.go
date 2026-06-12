package apicontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
)

// HandleAPITrustStatus reports the active authenticated API TLS trust state without exposing secrets.
func HandleAPITrustStatus(configPath string) (api.APITrustResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.APITrustResponse{}, err
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		return api.APITrustResponse{}, err
	}
	mode := cfg.API.Encryption.Mode
	if mode == "" {
		mode = config.APIEncryptionAuto
	}
	tlsRequired := cfg.API.RequiresTLS()
	trustedFingerprint := cfg.API.Encryption.TrustedCertificateSHA256
	resp := api.APITrustResponse{
		Mode:                         string(mode),
		TLSEnabled:                   tlsRequired,
		TLSRequired:                  tlsRequired,
		TrustedCertificateSHA256:     trustedFingerprint,
		TrustedCertificateConfigured: trustedFingerprint != "",
	}
	if !tlsRequired {
		resp.Message = "api tls is disabled for the active listener"
		return resp, nil
	}
	if cfg.API.Encryption.CertFile == "" {
		resp.Message = "api tls certificate file is not configured"
		return resp, nil
	}
	certPEM, err := os.ReadFile(cfg.API.Encryption.CertFile)
	if err != nil {
		return api.APITrustResponse{}, fmt.Errorf("read api TLS certFile %q: %w", cfg.API.Encryption.CertFile, err)
	}
	fingerprint, err := CertificateFingerprintSHA256(certPEM)
	if err != nil {
		return api.APITrustResponse{}, err
	}
	resp.CertificateSHA256 = fingerprint
	if trustedFingerprint == "" {
		resp.Message = "no trusted API certificate fingerprint is configured"
		return resp, nil
	}
	resp.TrustedCertificateMatches = trustedFingerprint == fingerprint
	if resp.TrustedCertificateMatches {
		resp.Message = "configured trusted certificate fingerprint matches the active API certificate"
	} else {
		resp.Message = "configured trusted certificate fingerprint does not match the active API certificate"
	}
	return resp, nil
}

// HandleAPITrustCommand handles authenticated API certificate trust mutations.
func HandleAPITrustCommand(configPath string, req api.APITrustCommandRequest) (api.APITrustCommandResponse, error) {
	if req.Action != "pin-active-certificate" {
		return api.APITrustCommandResponse{}, fmt.Errorf("unsupported api trust command action %q", req.Action)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.APITrustCommandResponse{}, err
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		return api.APITrustCommandResponse{}, err
	}
	if !cfg.API.RequiresTLS() {
		return api.APITrustCommandResponse{}, fmt.Errorf("api TLS is not enabled for the active listener")
	}
	if cfg.API.Encryption.CertFile == "" {
		return api.APITrustCommandResponse{}, fmt.Errorf("api TLS certificate file is not configured")
	}
	certPEM, err := os.ReadFile(cfg.API.Encryption.CertFile)
	if err != nil {
		return api.APITrustCommandResponse{}, fmt.Errorf("read api TLS certFile %q: %w", cfg.API.Encryption.CertFile, err)
	}
	fingerprint, err := CertificateFingerprintSHA256(certPEM)
	if err != nil {
		return api.APITrustCommandResponse{}, err
	}
	cfg.API.Encryption.TrustedCertificateSHA256 = fingerprint
	if err := writeConfig(configPath, cfg); err != nil {
		return api.APITrustCommandResponse{}, err
	}
	return api.APITrustCommandResponse{
		Action:                       req.Action,
		Status:                       "accepted",
		CertificateSHA256:            fingerprint,
		TrustedCertificateConfigured: true,
		TrustedCertificateMatches:    true,
		Message:                      "active API certificate fingerprint pinned for future HTTPS requests",
	}, nil
}

// CertificateFingerprintSHA256 returns the SHA-256 fingerprint of a PEM certificate.
func CertificateFingerprintSHA256(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("api TLS certificate did not contain a PEM certificate")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

func writeConfig(configPath string, cfg config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(configPath, append(data, '\n'), 0o600)
}
