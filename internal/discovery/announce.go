package discovery

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/structuredlog"
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

var (
	keyMu         sync.Mutex
	libp2pKeyFile string
)

// SetLibp2pKeyPath is the appdata file that keeps this process's libp2p peer
// id across restarts. An explicit FSE_LIBP2P_PRIVATE_KEY still wins.
func SetLibp2pKeyPath(path string) {
	keyMu.Lock()
	libp2pKeyFile = strings.TrimSpace(path)
	keyMu.Unlock()
}

func libp2pKeyPath() string {
	keyMu.Lock()
	defer keyMu.Unlock()
	return libp2pKeyFile
}

// configuredLibp2pIdentity loads FSE_LIBP2P_PRIVATE_KEY, then the appdata key
// file. A missing file is created. With neither, libp2p generates a new id.
func configuredLibp2pIdentity() (libp2p.Option, error) {
	if raw := strings.TrimSpace(os.Getenv("FSE_LIBP2P_PRIVATE_KEY")); raw != "" {
		key, err := privateKeyFromBase64(raw)
		if err != nil {
			return nil, fmt.Errorf("FSE_LIBP2P_PRIVATE_KEY is not a libp2p private key")
		}
		return libp2p.Identity(key), nil
	}
	path := libp2pKeyPath()
	if path == "" {
		return nil, nil
	}
	key, err := loadOrCreateLibp2pKey(path)
	if err != nil {
		return nil, err
	}
	return libp2p.Identity(key), nil
}

func privateKeyFromBase64(raw string) (crypto.PrivKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	return crypto.UnmarshalPrivateKey(decoded)
}

func loadOrCreateLibp2pKey(path string) (crypto.PrivKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key, keyErr := privateKeyFromBase64(string(data))
		if keyErr != nil {
			return nil, fmt.Errorf("libp2p key %s is not usable", path)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		return nil, err
	}
	encoded, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(encoded)), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// SetAnnounceAPIURL is the reachable API URL included in DHT provider records.
func SetAnnounceAPIURL(raw string) {
	announceMu.Lock()
	announceAPIURL = strings.TrimSpace(raw)
	announceMu.Unlock()
	promoteSharedRouterIfReachable()
}

// promoteSharedRouterIfReachable starts DHT server behavior only after a
// dial-back of the libp2p port succeeds. A published port or a LAN address
// is not that proof: marking the node public skips relay reservation.
// The check at router creation runs before the socket is listening, so a
// later call dials again instead of reusing that closed result.
func promoteSharedRouterIfReachable() {
	if tryPromoteSharedRouter() {
		return
	}
	schedulePromoteRetry()
}

func tryPromoteSharedRouter() bool {
	sharedMu.Lock()
	router := sharedRouter
	sharedMu.Unlock()
	if router == nil {
		return false
	}
	host, port, open := freshLibp2pDialback()
	if host == "" {
		return true
	}
	if !open {
		structuredlog.Event("info", "libp2p.dht.dialback", "published libp2p port did not accept a dial-back", map[string]any{"host": host, "port": port})
		return false
	}
	router.notePublicReachability()
	return true
}

var (
	promoteMu    sync.Mutex
	promoteTimer *time.Timer
)

func schedulePromoteRetry() {
	promoteMu.Lock()
	defer promoteMu.Unlock()
	if promoteTimer != nil {
		return
	}
	promoteTimer = time.AfterFunc(2*time.Second, func() {
		promoteMu.Lock()
		promoteTimer = nil
		promoteMu.Unlock()
		tryPromoteSharedRouter()
	})
}

func stopPromoteRetry() {
	promoteMu.Lock()
	if promoteTimer != nil {
		promoteTimer.Stop()
		promoteTimer = nil
	}
	promoteMu.Unlock()
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
	if sharedRouter != nil {
		router := sharedRouter
		sharedMu.Unlock()
		return router, nil
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
		sharedMu.Unlock()
		return nil, err
	}
	sharedRouter = router
	sharedMu.Unlock()
	// The port is listening now. A dial-back that failed during construction
	// is not the answer for the published hostname.
	promoteSharedRouterIfReachable()
	return router, nil
}

// LookupPeerAddrs asks the process DHT for a paired peer's addresses.
// An empty result means there is no router or the peer was not found.
func LookupPeerAddrs(ctx context.Context, peerID string) []string {
	sharedMu.Lock()
	router := sharedRouter
	sharedMu.Unlock()
	if router == nil {
		return nil
	}
	return router.LookupPeerAddrs(ctx, peerID)
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
// host.Addrs dials public candidates back, so the router lock is not held
// across that call.
func SharedListenAddrs() []string {
	sharedMu.Lock()
	router := sharedRouter
	sharedMu.Unlock()
	if router == nil || router.host == nil {
		return nil
	}
	return multiaddrStrings(router.host.Addrs())
}

// ResetSharedRouter closes the process router. Tests use it between cases.
func ResetSharedRouter() {
	stopPromoteRetry()
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedRouter != nil {
		_ = sharedRouter.Close()
		sharedRouter = nil
	}
}

func announcedAddrs(existing []ma.Multiaddr) []ma.Multiaddr {
	apiURL := currentAnnounceAPIURL()
	if advertise.ExcludeTailscale() && advertise.IsTailscaleHost(apiURL) {
		apiURL = ""
	}
	announced := announcedMultiaddrs(announceHosts(), apiURL, PublishedLibp2pPort(Libp2pListenPort()))
	announced = appendUniqueMultiaddr(announced, libp2pSTUNCandidate())
	var circuits []ma.Multiaddr
	for _, addr := range existing {
		if addr != nil && strings.Contains(addr.String(), "p2p-circuit") {
			circuits = append(circuits, addr)
		}
	}
	// Loopback and container bridge listen addresses are not paths another
	// LAN can dial. When nothing reachable is known, advertise only circuits.
	if len(announced) == 0 {
		return circuits
	}
	return append(announced, circuits...)
}

// libp2pSTUNCandidate is the QUIC address STUN observed for this socket.
// Symmetric mappings are not candidates: a third party would see a different port.
func libp2pSTUNCandidate() ma.Multiaddr {
	status := advertise.CurrentNATStatus()
	if !status.Advertise || status.IP == "" || status.Port < 1 || status.Port > 65535 {
		return nil
	}
	ip := net.ParseIP(status.IP)
	if ip == nil {
		return nil
	}
	var raw string
	if v4 := ip.To4(); v4 != nil {
		raw = fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", v4.String(), status.Port)
	} else {
		raw = fmt.Sprintf("/ip6/%s/udp/%d/quic-v1", ip.String(), status.Port)
	}
	addr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return nil
	}
	return addr
}

func appendUniqueMultiaddr(base []ma.Multiaddr, extra ma.Multiaddr) []ma.Multiaddr {
	if extra == nil {
		return base
	}
	needle := extra.String()
	for _, addr := range base {
		if addr != nil && addr.String() == needle {
			return base
		}
	}
	return append(base, extra)
}

func verifiedPublicLibp2p() bool {
	_, _, open := freshLibp2pDialbackCached()
	return open
}

func libp2pDialbackTarget() (string, int, bool) {
	port := PublishedLibp2pPort(Libp2pListenPort())
	host := advertiseHost(currentAnnounceAPIURL())
	if host == "" || !advertise.NeedsPublicDialback(host) || !advertise.DialbackProvesRemoteReachability(host) {
		return "", port, false
	}
	return host, port, true
}

// freshLibp2pDialbackCached reports the advertise host when a dial-back can
// prove another network reached this process. The boolean is a cached result
// and may be the pre-listen failure.
func freshLibp2pDialbackCached() (string, int, bool) {
	host, port, ok := libp2pDialbackTarget()
	if !ok {
		return "", port, false
	}
	return host, port, advertise.PublicDialbackOK(host, port)
}

// freshLibp2pDialback dials the advertise host again. Router construction
// checks reachability before the socket exists, and that closed result must
// not stick. A hostname that only resolves to this machine is not proof.
func freshLibp2pDialback() (string, int, bool) {
	host, port, ok := libp2pDialbackTarget()
	if !ok {
		return "", port, false
	}
	return host, port, advertise.ForcePublicDialback(host, port)
}

func announceHosts() []string {
	port := PublishedLibp2pPort(Libp2pListenPort())
	seen := map[string]struct{}{}
	var hosts []string
	add := func(raw string) {
		host := advertiseHost(raw)
		if host == "" {
			return
		}
		if advertise.ExcludeTailscale() && advertise.IsTailscaleHost(host) {
			return
		}
		if !advertise.MayAnnounceLibp2p(host, port) {
			return
		}
		if _, ok := seen[host]; ok {
			return
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	for _, ip := range advertise.AdvertisedIPs() {
		add(ip)
	}
	obs := advertise.CurrentObservations()
	if advertise.PublicDialbackOK(obs.GUIHost, 0) {
		add(obs.GUIHost)
	}
	if advertise.PublicDialbackOK(obs.ExternalIP, obs.ExternalPort) {
		add(obs.ExternalIP)
	}
	for _, ip := range obs.ObservedIPs {
		if advertise.PublicDialbackOK(ip, 0) {
			add(ip)
		}
	}
	add(currentAnnounceAPIURL())
	add(os.Getenv(advertise.EnvVar))
	for _, extra := range advertise.ListFromEnv(os.Getenv(advertise.EnvVarList)) {
		add(extra)
	}
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

// DialableHTTPAddress returns an http or https URL.
// A multiaddr that carries /http or /https is converted. Other multiaddrs are not API addresses.
func DialableHTTPAddress(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw, true
	}
	if !strings.HasPrefix(raw, "/") {
		return "", false
	}
	addr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return "", false
	}
	return apiURLFromMultiaddr(addr)
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
