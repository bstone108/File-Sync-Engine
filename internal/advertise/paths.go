package advertise

import (
	"net"
	"os"
	"strconv"
	"strings"

	"filesyncengine/internal/config"
	"filesyncengine/internal/routing"
)

// EnvExcludeTailscale drops CGNAT and Tailscale addresses from advertisement,
// libp2p announcements, and reconnect profiles. LAN, public, and relay paths
// stay. Set it when Tailscale is installed but its ports are not reachable.
const EnvExcludeTailscale = "FSE_ADVERTISE_EXCLUDE_TAILSCALE"

// ExcludeTailscale reports whether Tailscale and CGNAT addresses are omitted.
func ExcludeTailscale() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvExcludeTailscale))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// AdvertisedIPs returns every host-reachable address this machine should tell
// peers about: LAN, public, and Tailscale unless exclusion is on.
func AdvertisedIPs() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		ip := net.ParseIP(raw)
		if ip == nil || !usableIP(ip) || routing.IsContainerBridgeIP(ip) || ip.IsLoopback() {
			return
		}
		if skipTailscale(ip.String()) {
			return
		}
		text := ip.String()
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	for _, raw := range observedIPs() {
		add(raw)
	}
	if !ExcludeTailscale() {
		for _, raw := range TailscaleHosts(nil) {
			add(raw)
		}
	}
	obs := CurrentObservations()
	add(obs.ExternalIP)
	for _, raw := range obs.ObservedIPs {
		add(raw)
	}
	return out
}

// ProfileURLs is every API URL a reconnect profile should keep.
// The primary advertise URL is included along with LAN, STUN, NAT, and extra URLs.
func ProfileURLs(cfg config.Config) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || skipTailscale(raw) {
			return
		}
		normalized, err := Normalize(raw, cfg)
		if err != nil || skipTailscale(normalized) || !HostReachable(normalized) {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	add(os.Getenv(EnvVar))
	for _, extra := range ListFromEnv(os.Getenv(EnvVarList)) {
		add(extra)
	}
	add(cfg.API.AdvertiseURL)
	for _, published := range PublishedAPIURLs(cfg) {
		add(published)
	}
	for _, observed := range observationURLs(cfg, CurrentObservations()) {
		add(observed)
	}
	scheme := schemeFor(cfg)
	port := strconv.Itoa(reachPort(cfg))
	for _, ip := range AdvertisedIPs() {
		add(scheme + "://" + net.JoinHostPort(ip, port))
	}
	return out
}
