// Package advertise chooses the URL other engines use to reach this node's API.
package advertise

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"filesyncengine/internal/config"
	"filesyncengine/internal/routing"
)

// EnvVar overrides auto-detection. Set it at install or first start.
const EnvVar = "FSE_ADVERTISE_URL"

// EnvVarList is a comma- or whitespace-separated list of extra reachable API
// URLs. The first usable entry is the advertise URL when EnvVar is empty.
const EnvVarList = "FSE_ADVERTISE_URLS"

// EnvPublishedAPIPort is the host port mapped to the API listener. The web GUI
// combines it with the host name the browser used to open the console.
const EnvPublishedAPIPort = "FSE_PUBLISHED_API_PORT"

// Ensure persists the reachable API URL when it is missing, overridden, or
// still a container-internal address. An empty FSE_ADVERTISE_URL keeps a
// stored URL that peers can already reach.
func Ensure(configPath string) (config.Config, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return config.Config{}, err
	}
	before := cfg.API.AdvertiseURL
	applyErr := Apply(&cfg, os.Getenv(EnvVar))
	if cfg.API.AdvertiseURL == before {
		return cfg, applyErr
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return config.Config{}, err
	}
	if err := config.WriteFileAtomic(configPath, append(data, '\n'), 0o600); err != nil {
		return config.Config{}, err
	}
	return cfg, applyErr
}

// Apply sets cfg.API.AdvertiseURL from an explicit override, a saved
// host-reachable URL, published port mappings, or a detected address.
// A stored Docker bridge URL is cleared when nothing better is available.
func Apply(cfg *config.Config, envOverride string) error {
	resolved, err := ResolveWithHost(*cfg, envOverride, DetectHost())
	if err != nil {
		if ContainerInternalURL(cfg.API.AdvertiseURL) {
			cfg.API.AdvertiseURL = ""
		}
		return err
	}
	cfg.API.AdvertiseURL = resolved
	return nil
}

// ResolveWithHost picks the reachable API URL. An explicit override wins, then
// FSE_ADVERTISE_URLS, then a stored URL, then a published host port, then
// detectedHost. Docker bridge and other container-internal addresses are skipped.
func ResolveWithHost(cfg config.Config, envOverride, detectedHost string) (string, error) {
	for _, candidate := range candidateURLs(cfg, envOverride, detectedHost) {
		if !HostReachable(candidate) {
			continue
		}
		return Normalize(candidate, cfg)
	}
	return "", errors.New("could not detect a host-reachable address; set FSE_ADVERTISE_URL")
}

func candidateURLs(cfg config.Config, envOverride, detectedHost string) []string {
	out := []string{envOverride}
	out = append(out, ListFromEnv(os.Getenv(EnvVarList))...)
	out = append(out, cfg.API.AdvertiseURL)
	out = append(out, PublishedAPIURLs(cfg)...)
	out = append(out, detectedHost)
	return out
}

// Normalize turns a host or URL into scheme://host:port using the API listen port.
func Normalize(raw string, cfg config.Config) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("advertise URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = schemeFor(cfg) + "://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("advertise URL %q is not a valid http(s) URL", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("advertise URL must be http or https")
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = apiPort(cfg.API.Listen)
	}
	return parsed.Scheme + "://" + net.JoinHostPort(host, port), nil
}

// DetectHost returns the best host-reachable address for this machine.
// Docker bridge and container-internal addresses are never returned.
func DetectHost() string {
	return SelectDetected(observedIPs())
}

// SelectDetected prefers a LAN address, then Tailscale, then a public address.
// Container-internal addresses are dropped.
func SelectDetected(ips []string) string {
	var lan, vpn, public []string
	for _, raw := range ips {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil || !usableIP(ip) || routing.IsContainerBridgeIP(ip) {
			continue
		}
		switch {
		case isTailscaleIP(ip):
			vpn = append(vpn, ip.String())
		case ip.IsPrivate():
			lan = append(lan, ip.String())
		default:
			public = append(public, ip.String())
		}
	}
	for _, group := range [][]string{lan, vpn, public} {
		if len(group) == 0 {
			continue
		}
		sort.Strings(group)
		return group[0]
	}
	return ""
}

// ListFromEnv splits a comma- or whitespace-separated advertise URL list.
func ListFromEnv(raw string) []string {
	raw = strings.ReplaceAll(raw, ",", " ")
	return strings.Fields(raw)
}

// PublishedAPIURLs returns host-reachable URLs from published port mappings
// for this API listener. Bridge host IPs are omitted.
func PublishedAPIURLs(cfg config.Config) []string {
	listenPort := listenPortNumber(cfg.API.Listen)
	out := []string{}
	for _, mapping := range cfg.Discovery.NetworkHints.PublishedPortMappings {
		if mapping.HostPort <= 0 || strings.TrimSpace(mapping.HostIP) == "" {
			continue
		}
		if mapping.ContainerPort > 0 && listenPort > 0 && mapping.ContainerPort != listenPort {
			continue
		}
		if !HostReachable(mapping.HostIP) {
			continue
		}
		out = append(out, net.JoinHostPort(strings.TrimSpace(mapping.HostIP), strconv.Itoa(mapping.HostPort)))
	}
	return out
}

// PublishedAPIPort reads FSE_PUBLISHED_API_PORT. Zero means unset or invalid.
func PublishedAPIPort() int {
	raw := strings.TrimSpace(os.Getenv(EnvPublishedAPIPort))
	if raw == "" {
		return 0
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

// HostReachable reports whether raw is a hostname or an IP peers can be told
// to dial. Docker bridge addresses are not reachable from other hosts.
func HostReachable(raw string) bool {
	host := hostname(raw)
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	if routing.IsContainerBridgeIP(ip) {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return usableIP(ip)
}

// ContainerInternalURL reports whether raw uses a Docker bridge address.
func ContainerInternalURL(raw string) bool {
	ip := net.ParseIP(hostname(raw))
	return ip != nil && routing.IsContainerBridgeIP(ip)
}

func hostname(raw string) string {
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

func observedIPs() []string {
	ips := []string{}
	conn, err := net.Dial("udp", "192.0.2.1:80")
	if err == nil {
		if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok && udp.IP != nil {
			ips = append(ips, udp.IP.String())
		}
		conn.Close()
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || routing.ContainerInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip != nil {
				ips = append(ips, ip.String())
			}
		}
	}
	return ips
}

func addrIP(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}

func isTailscaleIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 != nil {
		return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return false
	}
	return ip16[0] == 0xfd && ip16[1] == 0x7a && ip16[2] == 0x11 && ip16[3] == 0x5c && ip16[4] == 0xa1 && ip16[5] == 0xe0
}

func listenPortNumber(listen string) int {
	port, err := strconv.Atoi(apiPort(listen))
	if err != nil {
		return 0
	}
	return port
}

func schemeFor(cfg config.Config) string {
	if cfg.API.RequiresTLS() {
		return "https"
	}
	return "http"
}

func apiPort(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "22420"
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		trimmed := strings.TrimPrefix(listen, ":")
		if trimmed != "" && trimmed != listen {
			return trimmed
		}
		return "22420"
	}
	if port == "" {
		return "22420"
	}
	return port
}

func usableIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}
