package discovery

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"

	"filesyncengine/internal/advertise"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	// DefaultLibp2pPort is the TCP and QUIC/UDP port libp2p listens on.
	// Docker must publish it; a random port is not reachable from another host.
	DefaultLibp2pPort = 22421
	// EnvLibp2pPort overrides the in-container listen port.
	EnvLibp2pPort = "FSE_LIBP2P_PORT"
	// EnvPublishedLibp2pPort is the host port mapped to the listen port.
	// Empty means the listen port is also the published port.
	EnvPublishedLibp2pPort = "FSE_PUBLISHED_LIBP2P_PORT"
)

var (
	sharedMu     sync.Mutex
	sharedRouter *Libp2pDHTRouter

	announceMu     sync.Mutex
	announceAPIURL string
)

// configuredLibp2pIdentity loads FSE_LIBP2P_PRIVATE_KEY when a deployment or
// test needs a stable peer id. Empty means libp2p generates one at start.
func configuredLibp2pIdentity() (libp2p.Option, error) {
	raw := strings.TrimSpace(os.Getenv("FSE_LIBP2P_PRIVATE_KEY"))
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("FSE_LIBP2P_PRIVATE_KEY is not base64")
	}
	key, err := crypto.UnmarshalPrivateKey(decoded)
	if err != nil {
		return nil, fmt.Errorf("FSE_LIBP2P_PRIVATE_KEY is not a libp2p private key")
	}
	return libp2p.Identity(key), nil
}

// SetAnnounceAPIURL is the reachable API URL included in DHT provider records.
func SetAnnounceAPIURL(raw string) {
	announceMu.Lock()
	announceAPIURL = strings.TrimSpace(raw)
	announceMu.Unlock()
	promoteSharedRouterIfReachable()
}

// promoteSharedRouterIfReachable starts DHT server behavior when a reachable
// address shows up after startup. A client can look records up either way.
func promoteSharedRouterIfReachable() {
	if !publishedLibp2pPortConfigured() && len(announceHosts()) == 0 {
		return
	}
	sharedMu.Lock()
	router := sharedRouter
	sharedMu.Unlock()
	if router == nil {
		return
	}
	router.notePublicReachability()
}

func currentAnnounceAPIURL() string {
	announceMu.Lock()
	defer announceMu.Unlock()
	if announceAPIURL != "" {
		return announceAPIURL
	}
	return strings.TrimSpace(os.Getenv(advertise.EnvVar))
}

// Libp2pListenPort is the port inside this process. Invalid values use the default.
func Libp2pListenPort() int {
	return portFromEnv(EnvLibp2pPort, DefaultLibp2pPort)
}

// PublishedLibp2pPort is the port other hosts dial. It defaults to the listen port.
func PublishedLibp2pPort(listenPort int) int {
	if listenPort < 1 || listenPort > 65535 {
		listenPort = DefaultLibp2pPort
	}
	return portFromEnv(EnvPublishedLibp2pPort, listenPort)
}

func portFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return fallback
	}
	return port
}

// ProductionRouter is the one libp2p host for discovery and pairing.
// A second host would bind the same published port and fail.
func ProductionRouter(ctx context.Context, nodeID string) (*Libp2pDHTRouter, error) {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedRouter != nil {
		return sharedRouter, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{
		NodeID:     nodeID,
		AutoRelay:  true,
		ListenPort: Libp2pListenPort(),
		Announce:   true,
	})
	if err != nil {
		return nil, err
	}
	sharedRouter = router
	return router, nil
}

// SharedPeerID is the libp2p peer id of the process router, when it is up.
func SharedPeerID() string {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedRouter == nil {
		return ""
	}
	return sharedRouter.PeerID()
}

// SharedListenAddrs are the addresses the process router is advertising.
func SharedListenAddrs() []string {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedRouter == nil || sharedRouter.host == nil {
		return nil
	}
	return multiaddrStrings(sharedRouter.host.Addrs())
}

// ResetSharedRouter closes the process router. Tests use it between cases.
func ResetSharedRouter() {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedRouter != nil {
		_ = sharedRouter.Close()
		sharedRouter = nil
	}
}

func announcedAddrs(existing []ma.Multiaddr) []ma.Multiaddr {
	announced := announcedMultiaddrs(announceHosts(), currentAnnounceAPIURL(), PublishedLibp2pPort(Libp2pListenPort()))
	if len(announced) == 0 {
		return existing
	}
	for _, addr := range existing {
		if addr != nil && strings.Contains(addr.String(), "p2p-circuit") {
			announced = append(announced, addr)
		}
	}
	return announced
}

func announceHosts() []string {
	seen := map[string]struct{}{}
	var hosts []string
	add := func(raw string) {
		host := advertiseHost(raw)
		if host == "" {
			return
		}
		if !advertise.HostReachable(host) {
			return
		}
		if _, ok := seen[host]; ok {
			return
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	add(advertise.DetectHost())
	for _, ip := range advertise.TailscaleHosts(context.Background()) {
		add(ip)
	}
	add(currentAnnounceAPIURL())
	return hosts
}

func advertiseHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	return strings.Trim(raw, "[]")
}

// announcedMultiaddrs is what peers dial: the published TCP and QUIC port,
// plus the HTTPS API URL so a failed libp2p stream can fetch the pairing record.
func announcedMultiaddrs(hosts []string, apiURL string, publishedPort int) []ma.Multiaddr {
	if publishedPort < 1 || publishedPort > 65535 {
		publishedPort = DefaultLibp2pPort
	}
	var out []ma.Multiaddr
	for _, host := range hosts {
		out = append(out, hostMultiaddrs(host, publishedPort)...)
	}
	if api := apiMultiaddr(apiURL); api != nil {
		out = append(out, api)
	}
	return out
}

func hostMultiaddrs(host string, port int) []ma.Multiaddr {
	host = strings.TrimSpace(host)
	if host == "" || port < 1 {
		return nil
	}
	base := "/dns4/" + host
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			base = "/ip4/" + ip.String()
		} else {
			base = "/ip6/" + ip.String()
		}
	}
	var out []ma.Multiaddr
	for _, raw := range []string{
		fmt.Sprintf("%s/tcp/%d", base, port),
		fmt.Sprintf("%s/udp/%d/quic-v1", base, port),
	} {
		addr, err := ma.NewMultiaddr(raw)
		if err == nil {
			out = append(out, addr)
		}
	}
	return out
}

func apiMultiaddr(raw string) ma.Multiaddr {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" {
		return nil
	}
	if !advertise.HostReachable(parsed.Hostname()) {
		return nil
	}
	scheme := "https"
	if parsed.Scheme == "http" {
		scheme = "http"
	} else if parsed.Scheme != "https" && parsed.Scheme != "" {
		return nil
	}
	host := parsed.Hostname()
	base := "/dns4/" + host
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			base = "/ip4/" + ip.String()
		} else {
			base = "/ip6/" + ip.String()
		}
	}
	addr, err := ma.NewMultiaddr(fmt.Sprintf("%s/tcp/%s/%s", base, parsed.Port(), scheme))
	if err != nil {
		return nil
	}
	return addr
}

func multiaddrStrings(addrs []ma.Multiaddr) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr != nil {
			out = append(out, addr.String())
		}
	}
	return out
}

// apiURLFromMultiaddr reads an HTTP or HTTPS URL carried in a provider address.
func apiURLFromMultiaddr(addr ma.Multiaddr) (string, bool) {
	if addr == nil {
		return "", false
	}
	scheme := ""
	if _, err := addr.ValueForProtocol(ma.P_HTTPS); err == nil {
		scheme = "https"
	} else if _, err := addr.ValueForProtocol(ma.P_HTTP); err == nil {
		scheme = "http"
	} else {
		return "", false
	}
	host := ""
	for _, code := range []int{ma.P_IP4, ma.P_IP6, ma.P_DNS4, ma.P_DNS6, ma.P_DNS} {
		value, err := addr.ValueForProtocol(code)
		if err == nil && value != "" {
			host = value
			break
		}
	}
	port, err := addr.ValueForProtocol(ma.P_TCP)
	if host == "" || err != nil || port == "" {
		return "", false
	}
	return scheme + "://" + net.JoinHostPort(host, port), true
}
