package daemonclient

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"filesyncengine/internal/config"
)

// RequestOptions returns the daemon API URL and HTTP client for a configured request.
func RequestOptions(cfg config.Config, method string, path string, body io.Reader) (string, *http.Client, error) {
	_ = method
	_ = body
	if cfg.API.Listen == "" {
		return "", nil, fmt.Errorf("api.listen is not configured")
	}
	scheme := "http"
	client := http.DefaultClient
	if cfg.API.RequiresTLS() {
		scheme = "https"
		tlsConfig, err := TLSClientConfig(cfg)
		if err != nil {
			return "", nil, err
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConfig
		client = &http.Client{Transport: transport}
	}
	return scheme + "://" + cfg.API.Listen + path, client, nil
}

// Request sends an authenticated daemon API request and returns the response body.
func Request(cfg config.Config, method string, path string, body io.Reader) ([]byte, error) {
	url, client, err := RequestOptions(cfg, method, path, body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-FSE-API-Key", cfg.API.Key)
	resp, err := client.Do(req)
	label := strings.TrimPrefix(path, "/v1/")
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", label, err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s failed: %s: %s", label, resp.Status, string(responseBody))
	}
	return responseBody, nil
}

// TLSClientConfig trusts the daemon API certificate configured for the active daemon.
func TLSClientConfig(cfg config.Config) (*tls.Config, error) {
	certFile := cfg.API.Encryption.CertFile
	if certFile == "" {
		return nil, fmt.Errorf("api TLS certFile is not configured; run API TLS bootstrap before daemon requests")
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("read api TLS certFile %q: %w", certFile, err)
	}
	if !roots.AppendCertsFromPEM(certPEM) {
		return nil, fmt.Errorf("api TLS certFile %q did not contain a PEM certificate", certFile)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	if want := cfg.API.Encryption.TrustedCertificateSHA256; want != "" {
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			_ = verifiedChains
			if len(rawCerts) == 0 {
				return fmt.Errorf("api TLS certificate fingerprint mismatch: no peer certificate")
			}
			got := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(got[:]) != want {
				return fmt.Errorf("api TLS certificate fingerprint mismatch")
			}
			return nil
		}
	}
	return tlsConfig, nil
}

// CertificateFingerprintFromFile returns the SHA-256 fingerprint for a PEM certificate file.
func CertificateFingerprintFromFile(path string) (string, error) {
	certPEM, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return CertificateFingerprintSHA256(certPEM)
}

// CertificateFingerprintSHA256 returns the SHA-256 fingerprint for the first PEM certificate.
func CertificateFingerprintSHA256(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("no certificate found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:]), nil
}
