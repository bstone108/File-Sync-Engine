package link

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/nat"
	"filesyncengine/internal/routing"
)

// OrderedEndpoints sorts API addresses so LAN is tried before Tailscale,
// public, and relays. Unreachable earlier addresses are kept so failover can
// move on when the first one dies.
func OrderedEndpoints(peer config.PeerConfig) []config.EndpointConfig {
	endpoints := make([]config.EndpointConfig, 0, len(peer.Endpoints)+len(peer.Relays))
	for _, endpoint := range peer.Endpoints {
		if !isAPIAddress(endpoint.Address) {
			continue
		}
		endpoints = append(endpoints, endpoint)
	}
	seen := map[string]struct{}{}
	for _, endpoint := range endpoints {
		seen[endpoint.Address] = struct{}{}
	}
	for _, relay := range peer.Relays {
		if !isAPIAddress(relay) {
			continue
		}
		if _, ok := seen[relay]; ok {
			continue
		}
		endpoints = append(endpoints, config.EndpointConfig{Kind: "relay", Address: relay, NetworkHint: string(routing.ClassifyEndpointNetwork(relay))})
	}
	sort.SliceStable(endpoints, func(i, j int) bool {
		left, right := endpointRank(endpoints[i]), endpointRank(endpoints[j])
		if left != right {
			return left < right
		}
		return endpoints[i].Address < endpoints[j].Address
	})
	return endpoints
}

// HasAPIEndpoint reports whether the peer has at least one HTTP API address.
func HasAPIEndpoint(peer config.PeerConfig) bool {
	return len(OrderedEndpoints(peer)) > 0
}

// Dial tries saved addresses first, then peer-exchange and same-identity
// connection details, then LAN discoveries, then DHT addresses. Each stage
// races its addresses and keeps a relay only until a direct path wins.
// An HTTP response stops the search because that address is alive.
func Dial(ctx context.Context, peer config.PeerConfig, do func(context.Context, string, *http.Client) error) (string, error) {
	stages := endpointStages(peer)
	var last error
	tried := 0
	for _, stage := range stages {
		if len(stage) == 0 {
			continue
		}
		tried += len(stage)
		used, path, err := dialStage(ctx, peer, stage, do)
		if err == nil {
			discovery.NotePeerReachable(peer.ID)
			nat.DefaultPaths.Observe(peer.ID, used, path)
			if peer.DHTPeerID != "" {
				nat.DefaultPaths.Observe(peer.DHTPeerID, used, path)
			}
			return used, nil
		}
		var answered answeredError
		if errors.As(err, &answered) {
			return "", answered.err
		}
		last = err
	}
	if tried == 0 {
		return "", errors.New("peer has no http endpoint")
	}
	discovery.NotePeerUnreachable(peer.ID)
	if last == nil {
		last = errors.New("peer has no reachable endpoint")
	}
	return "", last
}

func endpointStages(peer config.PeerConfig) [][]config.EndpointConfig {
	exchanged, local, dht := discovery.AddressesFor(peer.ID)
	return [][]config.EndpointConfig{
		OrderedEndpoints(peer),
		httpEndpoints(exchanged, "manual"),
		httpEndpoints(local, "manual"),
		httpEndpoints(dht, "relay"),
	}
}

func httpEndpoints(addresses []string, kind string) []config.EndpointConfig {
	out := make([]config.EndpointConfig, 0, len(addresses))
	seen := map[string]struct{}{}
	for _, address := range addresses {
		if !isAPIAddress(address) {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, config.EndpointConfig{Kind: kind, Address: address, NetworkHint: string(routing.ClassifyEndpointNetwork(address))})
	}
	return out
}

func dialStage(ctx context.Context, peer config.PeerConfig, endpoints []config.EndpointConfig, do func(context.Context, string, *http.Client) error) (string, nat.Path, error) {
	candidates := make([]nat.Candidate, 0, len(endpoints))
	byAddress := map[string]config.EndpointConfig{}
	for _, endpoint := range endpoints {
		path := nat.Classify(endpoint.Address)
		if endpoint.Kind == "relay" {
			path = nat.PathRelay
		}
		candidates = append(candidates, nat.Candidate{Address: endpoint.Address, Path: path})
		byAddress[endpoint.Address] = endpoint
	}
	return nat.Race(ctx, candidates, func(ctx context.Context, address string) error {
		client, err := verifyPinnedPeerCertificateClient(address, peer.APICertificateSHA256)
		if err != nil {
			return err
		}
		err = do(ctx, strings.TrimRight(address, "/"), client)
		if err == nil {
			return nil
		}
		if !failoverError(err) {
			return answeredError{err: err}
		}
		return err
	}, 250*time.Millisecond)
}

type answeredError struct{ err error }

func (e answeredError) Error() string { return e.err.Error() }

func (e answeredError) Unwrap() error { return nat.ErrAnswered }

// PostJSON dials the peer and posts JSON, failing over when an earlier address dies.
func PostJSON(ctx context.Context, peer config.PeerConfig, path string, payload []byte) ([]byte, string, error) {
	var body []byte
	used, err := Dial(ctx, peer, func(ctx context.Context, base string, client *http.Client) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if peer.APIKey != "" {
			req.Header.Set("X-FSE-API-Key", peer.APIKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		read, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			message := strings.TrimSpace(string(read))
			if message == "" {
				message = resp.Status
			}
			return fmt.Errorf("peer %s: %s", path, message)
		}
		body = read
		return nil
	})
	return body, used, err
}

func endpointRank(endpoint config.EndpointConfig) int {
	if endpoint.Kind == "relay" {
		return 4
	}
	hint := endpoint.NetworkHint
	if hint == "" {
		hint = string(routing.ClassifyEndpointNetwork(endpoint.Address))
	}
	switch hint {
	case string(routing.LocalNetwork):
		return 0
	case string(routing.VPNNetwork):
		return 1
	case string(routing.WANNetwork), "":
		return 2
	case string(routing.ContainerBridgeNetwork):
		return 3
	default:
		return 2
	}
}

func isAPIAddress(address string) bool {
	return strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://")
}

func failoverError(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// verifyPinnedPeerCertificateClient trusts only the pinned leaf certificate.
// Go requires InsecureSkipVerify so the self-signed daemon certificate can reach
// VerifyPeerCertificate, which checks the SHA-256 fingerprint.
func verifyPinnedPeerCertificateClient(endpoint, pin string) (*http.Client, error) {
	if !strings.HasPrefix(endpoint, "https://") {
		return &http.Client{Timeout: 8 * time.Second}, nil
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
	return &http.Client{Timeout: 8 * time.Second, Transport: transport}, nil
}
