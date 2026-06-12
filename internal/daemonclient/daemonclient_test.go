package daemonclient

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestRequestOptionsUseHTTPSTrustedConfiguredCertificate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
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

	url, client, err := RequestOptions(cfg, http.MethodGet, "/v1/status", nil)
	if err != nil {
		t.Fatalf("RequestOptions: %v", err)
	}
	if url != "https://0.0.0.0:22420/v1/status" {
		t.Fatalf("url = %q, want HTTPS daemon URL", url)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatalf("HTTPS API client did not configure a trusted certificate pool: %#v", client.Transport)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("HTTPS API client must trust configured certs, not disable verification")
	}
	if got := len(transport.TLSClientConfig.RootCAs.Subjects()); got == 0 {
		t.Fatalf("HTTPS API client root pool has no configured certificate subjects")
	}
}

func TestRequestReturnsBodyAndSendsAPIKey(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-FSE-API-Key")
		if r.URL.Path != "/v1/status" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	defer server.Close()

	cfg := config.Config{API: config.APIConfig{Listen: strings.TrimPrefix(server.URL, "http://"), Key: "secret-key"}}
	body, err := Request(cfg, http.MethodGet, "/v1/status", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if string(body) != `{"status":"running"}` {
		t.Fatalf("body = %s", string(body))
	}
	if gotKey != "secret-key" {
		t.Fatalf("API key header = %q", gotKey)
	}
}

func TestRequestReportsNonOKStatusWithResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key"))
	}))
	defer server.Close()

	cfg := config.Config{API: config.APIConfig{Listen: strings.TrimPrefix(server.URL, "http://"), Key: "secret-key"}}
	_, err := Request(cfg, http.MethodPost, "/v1/stop", nil)
	if err == nil || !strings.Contains(err.Error(), "stop failed: 401 Unauthorized: bad key") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestTLSClientConfigPinsConfiguredCertificateFingerprint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
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
	fingerprint, err := CertificateFingerprintFromFile(cfg.API.Encryption.CertFile)
	if err != nil {
		t.Fatalf("fingerprint generated certificate: %v", err)
	}
	cfg.API.Encryption.TrustedCertificateSHA256 = fingerprint

	tlsConfig, err := TLSClientConfig(cfg)
	if err != nil {
		t.Fatalf("TLSClientConfig: %v", err)
	}
	if tlsConfig.VerifyPeerCertificate == nil {
		t.Fatalf("expected API TLS client to pin configured certificate fingerprint")
	}
	if err := tlsConfig.VerifyPeerCertificate([][]byte{[]byte("not the certificate")}, nil); err == nil || !strings.Contains(err.Error(), "api TLS certificate fingerprint mismatch") {
		t.Fatalf("wrong certificate was not rejected with fingerprint mismatch: %v", err)
	}
}
