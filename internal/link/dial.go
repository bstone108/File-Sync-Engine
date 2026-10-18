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
		address, ok := discovery.DialableHTTPAddress(endpoint.Address)
		if !ok || !keepDialEndpoint(endpoint, peer.DHTPeerID) {
			continue
		}
		endpoint.Address = address
		endpoints = append(endpoints, endpoint)
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

// HasAPIEndpoint reports whether the peer has an HTTP API address or a
// libp2p stream path. Relay-only peers are dialable.
func HasAPIEndpoint(peer config.PeerConfig) bool {
	return len(OrderedEndpoints(peer)) > 0 || len(libp2pTargets(peer)) > 0
}

// Dial tries saved addresses first, then peer-exchange and same-identity
// connection details, then LAN discoveries, then DHT addresses. Each stage
// races its addresses and keeps a relay only until a direct path wins.
// An HTTP response stops the search because that address is alive.
func Dial(ctx context.Context, peer config.PeerConfig, do func(context.Context, string, *http.Client) error) (string, error) {
	if base, client, ok := cachedDial(peer.ID); ok {
		err := do(ctx, base, client)
		if err == nil {
			discovery.NotePeerReachable(peer.ID)
			nat.DefaultPaths.Touch(peer.ID)
			if peer.DHTPeerID != "" {
				nat.DefaultPaths.Touch(peer.DHTPeerID)
			}
			return base, nil
		}
		forgetDial(peer.ID)
		if !failoverError(err) {
			return "", err
		}
	}
	stages := endpointStages(peer)
	var last error
	sawLimited := false
	tried := 0
	for i, stage := range stages {
		if i == libp2pStage {
			used, err := dialLibp2p(ctx, peer, do)
			if err == nil {
				return used, nil
			}
			var answered answeredError
			if errors.As(err, &answered) {
				return "", answered.err
			}
			if !errors.Is(err, errNoStream) && err != nil {
				if errors.Is(err, discovery.ErrLimitedRelay) {
					sawLimited = true
				}
				last = err
				tried++
			}
			continue
		}
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
	if sawLimited || errors.Is(last, discovery.ErrLimitedRelay) {
		nat.DefaultPaths.ObserveLimited(peer.ID, "")
		if peer.DHTPeerID != "" {
			nat.DefaultPaths.ObserveLimited(peer.DHTPeerID, "")
		}
	} else {
		nat.DefaultPaths.Forget(peer.ID)
		if peer.DHTPeerID != "" {
			nat.DefaultPaths.Forget(peer.DHTPeerID)
		}
	}
	if last == nil {
		last = errors.New("peer has no reachable endpoint")
	}
	return "", last
}

// libp2pStage is tried after saved LAN/Tailscale addresses and learned LAN
// addresses, and before an unverified public API URL.
const libp2pStage = 3

func endpointStages(peer config.PeerConfig) [][]config.EndpointConfig {
	var preferred, fallback []config.EndpointConfig
	for _, endpoint := range OrderedEndpoints(peer) {
		if endpointRank(endpoint) <= 1 {
			preferred = append(preferred, endpoint)
		} else {
			fallback = append(fallback, endpoint)
		}
	}
	return [][]config.EndpointConfig{
		preferred,
		nil,
		nil,
		nil, // libp2pStage dials relay, hole-punched, and direct streams
		fallback,
		nil, // DHT addresses are one libp2p dial, never an HTTP endpoint
	}
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
		endpoint := byAddress[address]
		if !keepDialEndpoint(endpoint, peer.DHTPeerID) {
			return &url.Error{Op: "dial", URL: address, Err: errors.New("refusing to send application data before the peer is authenticated")}
		}
		if err := refuseUnencrypted(address); err != nil {
			return err
		}
		client, err := verifyPinnedPeerCertificateClient(address, peer.APICertificateSHA256)
		if err != nil {
			return &url.Error{Op: "dial", URL: address, Err: err}
		}
		err = do(ctx, strings.TrimRight(address, "/"), client)
		if err == nil {
			rememberDial(peer.ID, strings.TrimRight(address, "/"), client)
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
	return postJSON(ctx, peer, path, payload, false)
}

// PostSecret posts a body that contains an API key or pairing claim.
// Plain HTTP is skipped. HTTPS checks the certificate pin during the handshake,
// and a libp2p stream is opened only after the peer id is authenticated.
func PostSecret(ctx context.Context, peer config.PeerConfig, path string, payload []byte) ([]byte, string, error) {
	return postJSON(ctx, peer, path, payload, true)
}

func postJSON(ctx context.Context, peer config.PeerConfig, path string, payload []byte, secret bool) ([]byte, string, error) {
	var body []byte
	used, err := Dial(ctx, peer, func(ctx context.Context, base string, client *http.Client) error {
		if !encryptedAppTransport(base) {
			message := "refusing to send an unencrypted payload"
			if secret {
				message = "refusing to send a secret before the peer identity is verified"
			}
			return &url.Error{Op: "post", URL: base, Err: errors.New(message)}
		}
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

func secretTransportAllowed(base string) bool {
	return encryptedAppTransport(base)
}

// encryptedAppTransport is an HTTPS connection pinned to the paired peer, or a
// libp2p stream whose handshake already authenticated that peer. Plain HTTP is
// not a path for application data.
func encryptedAppTransport(base string) bool {
	if base == discovery.StreamBaseURL {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(base)), "https://")
}

// refuseUnencrypted stops a dial before any application bytes are written.
func refuseUnencrypted(address string) error {
	if encryptedAppTransport(address) {
		return nil
	}
	return &url.Error{Op: "dial", URL: address, Err: errors.New("refusing to send an unencrypted payload")}
}

func endpointRank(endpoint config.EndpointConfig) int {
	if endpoint.Kind == "relay" {
		return 4
	}
	// A peer-supplied hostname (Docker DNS, a LAN name) is a path that peer
	// named. Try it before a public relay. A literal public IP stays later so
	// an unforwarded address does not block hole-punch and relay.
	if endpointHostIsName(endpoint.Address) {
		return 0
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

func endpointHostIsName(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return net.ParseIP(parsed.Hostname()) == nil
}

func isAPIAddress(address string) bool {
	_, ok := discovery.DialableHTTPAddress(address)
	return ok
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

const (
	dialTimeout   = 8 * time.Second
	headerTimeout = 15 * time.Second
)

// IdleReadTimeout is how long a transfer waits for the next bytes.
// There is no total timeout. A large file keeps moving as long as bytes arrive.
var IdleReadTimeout = 30 * time.Second

// verifyPinnedPeerCertificateClient trusts only the pinned leaf certificate.
// Go requires InsecureSkipVerify so the self-signed daemon certificate can reach
// VerifyPeerCertificate, which checks the SHA-256 fingerprint.
// The client has a dial timeout and a header timeout, then an idle timeout on
// each read. It does not cap the total transfer.
func verifyPinnedPeerCertificateClient(endpoint, pin string) (*http.Client, error) {
	transport := transferTransport(nil)
	if !strings.HasPrefix(endpoint, "https://") {
		return &http.Client{Transport: transport}, nil
	}
	pin = strings.ToLower(strings.TrimSpace(pin))
	if len(pin) != 64 {
		return nil, errors.New("https peer is missing an API certificate fingerprint")
	}
	transport.TLSClientConfig = &tls.Config{
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
	}
	return &http.Client{Transport: transport}, nil
}

func transferTransport(base *http.Transport) *http.Transport {
	if base == nil {
		base = &http.Transport{}
	}
	base.Proxy = http.ProxyFromEnvironment
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
		conn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &idleConn{Conn: conn, idle: IdleReadTimeout}, nil
	}
	base.ForceAttemptHTTP2 = false
	base.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	base.MaxIdleConns = 32
	base.IdleConnTimeout = 90 * time.Second
	base.TLSHandshakeTimeout = headerTimeout
	base.ResponseHeaderTimeout = headerTimeout
	base.ExpectContinueTimeout = time.Second
	return base
}

// idleConn fails a read or write that goes quiet, without a total deadline.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(p []byte) (int, error) {
	if c.idle > 0 {
		_ = c.Conn.SetReadDeadline(time.Now().Add(c.idle))
	}
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	if c.idle > 0 {
		_ = c.Conn.SetWriteDeadline(time.Now().Add(c.idle))
	}
	return c.Conn.Write(p)
}
