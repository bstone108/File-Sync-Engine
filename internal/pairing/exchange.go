package pairing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PairOffer is the importer's introduction to the node that issued a pairing package.
type PairOffer struct {
	GroupID              string `json:"groupId"`
	BootstrapProofKey    string `json:"bootstrapProofKey"`
	DiscoveryID          string `json:"discoveryId"`
	NodeName             string `json:"nodeName"`
	ReachableAPIURL      string `json:"reachableAPIURL"`
	APICertificateSHA256 string `json:"apiCertificateSha256"`
	APIKey               string `json:"apiKey"`
}

// PairAcceptance is the issuing node's API credential, returned only to the paired daemon.
type PairAcceptance struct {
	DiscoveryID          string `json:"discoveryId"`
	NodeName             string `json:"nodeName"`
	APIKey               string `json:"apiKey"`
	APICertificateSHA256 string `json:"apiCertificateSha256"`
}

// ExchangePair posts a bootstrap-proof offer to the remote daemon and returns its API credential.
func ExchangePair(ctx context.Context, remoteAPIURL, remoteCertSHA256 string, offer PairOffer) (PairAcceptance, error) {
	remoteAPIURL = strings.TrimRight(strings.TrimSpace(remoteAPIURL), "/")
	if !strings.HasPrefix(remoteAPIURL, "https://") && !strings.HasPrefix(remoteAPIURL, "http://") {
		return PairAcceptance{}, errors.New("reachable API URL must be http or https")
	}
	body, err := json.Marshal(offer)
	if err != nil {
		return PairAcceptance{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, remoteAPIURL+"/v1/pair-exchange", bytes.NewReader(body))
	if err != nil {
		return PairAcceptance{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client, err := pinnedClient(remoteAPIURL, remoteCertSHA256)
	if err != nil {
		return PairAcceptance{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return PairAcceptance{}, fmt.Errorf("pair exchange: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return PairAcceptance{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return PairAcceptance{}, fmt.Errorf("pair exchange: %s", resp.Status)
	}
	var accepted PairAcceptance
	if err := json.Unmarshal(payload, &accepted); err != nil {
		return PairAcceptance{}, err
	}
	if accepted.APIKey == "" || accepted.DiscoveryID == "" {
		return PairAcceptance{}, errors.New("pair exchange response was incomplete")
	}
	return accepted, nil
}

func pinnedClient(endpoint, pin string) (*http.Client, error) {
	if !strings.HasPrefix(endpoint, "https://") {
		return &http.Client{Timeout: 30 * time.Second}, nil
	}
	pin = strings.ToLower(strings.TrimSpace(pin))
	if len(pin) != 64 {
		return nil, errors.New("https peer is missing an API certificate fingerprint")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("peer presented no certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) != pin {
				return errors.New("peer API certificate fingerprint mismatch")
			}
			return nil
		},
	}}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport}, nil
}
