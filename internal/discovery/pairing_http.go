package discovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/structuredlog"
)

const pairingHTTPTimeout = 4 * time.Second

// fetchPairingHTTP reads the encrypted pairing record from an HTTPS API URL
// carried in the DHT provider addresses. The record stays sealed; the caller
// still opens it with the pairing secret.
func fetchPairingHTTP(ctx context.Context, addrs []ma.Multiaddr, namespace string) ([]byte, error) {
	if namespace == "" {
		return nil, fmt.Errorf("pairing namespace is required")
	}
	var last error
	seen := map[string]struct{}{}
	for _, addr := range addrs {
		raw, ok := apiURLFromMultiaddr(addr)
		if !ok {
			continue
		}
		if _, dup := seen[raw]; dup {
			continue
		}
		seen[raw] = struct{}{}
		payload, err := fetchPairingURL(ctx, raw, namespace)
		if err != nil {
			last = err
			structuredlog.Event("warn", "pairing.lookup.http_failed", "pairing record HTTP fetch failed", map[string]any{
				"namespace": namespace,
				"endpoint":  raw,
				"error":     err.Error(),
			})
			continue
		}
		if len(payload) == 0 {
			continue
		}
		structuredlog.Event("info", "pairing.lookup.http", "pairing record fetched from the advertised API", map[string]any{
			"namespace": namespace,
			"endpoint":  raw,
		})
		return payload, nil
	}
	if last != nil {
		return nil, last
	}
	return nil, fmt.Errorf("pairing provider record has no advertised API URL")
}

func fetchPairingURL(ctx context.Context, endpoint, namespace string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, pairingHTTPTimeout)
	defer cancel()
	target, err := url.Parse(endpoint)
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("pairing API URL is invalid")
	}
	target.Path = "/v1/pair-code/record"
	target.RawQuery = url.Values{"namespace": []string{namespace}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := pairingClientSkippingCertificateVerification().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPairingBytes+1))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		text := strings.TrimSpace(string(body))
		if text == "" {
			text = response.Status
		}
		return nil, fmt.Errorf("pairing API fetch failed: %s", text)
	}
	if len(body) > maxPairingBytes {
		return nil, fmt.Errorf("pairing record exceeds %d bytes", maxPairingBytes)
	}
	return body, nil
}

// pairingClientSkippingCertificateVerification fetches ciphertext from the
// engine's own API certificate. That certificate is self-signed, so the host
// name and CA are not checked on this call. The pairing secret authenticates
// the body; a substituted response fails the MAC before any profile is used.
func pairingClientSkippingCertificateVerification() *http.Client {
	// codeql[go/disabled-certificate-check]
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	tlsConfig.InsecureSkipVerify = true
	return &http.Client{
		Timeout: pairingHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("pairing API redirect is not followed")
		},
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}
}

func dialablePairingAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	var out []ma.Multiaddr
	for _, addr := range addrs {
		if addr == nil {
			continue
		}
		if _, ok := apiURLFromMultiaddr(addr); ok {
			continue
		}
		out = append(out, addr)
	}
	return out
}
