package link

import (
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/config"
	"filesyncengine/internal/routing"
)

// Address is one way to reach an engine API. Kind and NetworkHint use the
// config endpoint vocabulary so a profile can be stored on a peer as-is.
type Address struct {
	Kind        string `json:"kind"`
	Address     string `json:"address"`
	NetworkHint string `json:"networkHint,omitempty"`
}

// Profile is everything a peer needs to reconnect after the first link.
// It carries addresses, the DHT identity, relays, and the API certificate pin.
// It does not carry API keys or private identity keys.
type Profile struct {
	DiscoveryID       string    `json:"discoveryId,omitempty"`
	NodeName          string    `json:"nodeName,omitempty"`
	DHTPeerID         string    `json:"dhtPeerId,omitempty"`
	CertificateSHA256 string    `json:"certificateSha256,omitempty"`
	Addresses         []Address `json:"addresses,omitempty"`
	Relays            []string  `json:"relays,omitempty"`
}

// Extras are observations supplied by the caller. Empty IPs means the
// profile only includes the configured advertise URL and ExtraURLs.
// Container-internal addresses are never included.
type Extras struct {
	IPs               []string
	ExtraURLs         []string
	Relays            []string
	DHTPeerID         string
	CertificateSHA256 string
}

// LocalIPs returns non-loopback unicast addresses on up interfaces.
func LocalIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || routing.ContainerInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip == nil || !usableLinkIP(ip) {
				continue
			}
			text := ip.String()
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			out = append(out, text)
		}
	}
	sort.Strings(out)
	return out
}

// Gather builds a reconnect profile from the engine config and observed addresses.
func Gather(cfg config.Config, extras Extras) Profile {
	scheme, port := endpointSchemePort(cfg)
	seen := map[string]struct{}{}
	addresses := []Address{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		normalized := raw
		if !strings.Contains(raw, "://") {
			normalized = scheme + "://" + net.JoinHostPort(raw, port)
		}
		if !keepReachableAddress(normalized) || skipTailscaleAddress(normalized) || skipUnverifiedPublic(normalized) || gatewayAddress(normalized) {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		hint := string(routing.ClassifyEndpointNetwork(normalized))
		kind := "manual"
		if hint == string(routing.VPNNetwork) {
			kind = "vpn"
		}
		addresses = append(addresses, Address{Kind: kind, Address: normalized, NetworkHint: hint})
	}
	add(cfg.API.AdvertiseURL)
	for _, extra := range extras.ExtraURLs {
		add(extra)
	}
	for _, ip := range extras.IPs {
		add(ip)
	}
	relays := []string{}
	for _, relay := range normalizeList(extras.Relays) {
		if !keepReachableAddress(relay) || skipTailscaleAddress(relay) {
			continue
		}
		relays = append(relays, relay)
		// A relay multiaddr or a public relay IP is not this peer's API.
		if strings.HasPrefix(relay, "/") || skipUnverifiedPublic(relay) {
			continue
		}
		if _, ok := seen[relay]; ok {
			continue
		}
		seen[relay] = struct{}{}
		addresses = append(addresses, Address{Kind: "relay", Address: relay, NetworkHint: string(routing.ClassifyEndpointNetwork(relay))})
	}
	sort.SliceStable(addresses, func(i, j int) bool {
		if addresses[i].Address != addresses[j].Address {
			return addresses[i].Address < addresses[j].Address
		}
		return addresses[i].Kind < addresses[j].Kind
	})
	return Profile{
		DiscoveryID:       cfg.Identity.PublicKey,
		NodeName:          cfg.NodeName,
		DHTPeerID:         strings.TrimSpace(extras.DHTPeerID),
		CertificateSHA256: strings.ToLower(strings.TrimSpace(extras.CertificateSHA256)),
		Addresses:         addresses,
		Relays:            relays,
	}
}

// Endpoints converts a profile into config endpoints, always keeping primary
// when it is not already present.
func (p Profile) Endpoints(primary string) []config.EndpointConfig {
	seen := map[string]struct{}{}
	out := []config.EndpointConfig{}
	add := func(kind, address, hint string) {
		address = strings.TrimSpace(address)
		if address == "" || !keepReachableAddress(address) || skipTailscaleAddress(address) {
			return
		}
		if _, ok := seen[address]; ok {
			return
		}
		seen[address] = struct{}{}
		if kind == "" {
			kind = "manual"
		}
		if hint == "" {
			hint = string(routing.ClassifyEndpointNetwork(address))
		}
		if kind == "manual" && hint == string(routing.VPNNetwork) {
			kind = "vpn"
		}
		out = append(out, config.EndpointConfig{Kind: kind, Address: address, NetworkHint: hint, Authenticated: true})
	}
	add("manual", primary, "")
	for _, address := range p.Addresses {
		if address.Kind == "relay" {
			continue
		}
		add(address.Kind, address.Address, address.NetworkHint)
	}
	return out
}

// MergeProfiles keeps every address, relay, and identity from both observations.
func MergeProfiles(current, observed Profile) Profile {
	if observed.DiscoveryID != "" {
		current.DiscoveryID = observed.DiscoveryID
	}
	if observed.NodeName != "" {
		current.NodeName = observed.NodeName
	}
	if observed.DHTPeerID != "" {
		current.DHTPeerID = observed.DHTPeerID
	}
	if observed.CertificateSHA256 != "" {
		current.CertificateSHA256 = strings.ToLower(observed.CertificateSHA256)
	}
	seen := map[string]struct{}{}
	merged := []Address{}
	for _, address := range append(append([]Address{}, current.Addresses...), observed.Addresses...) {
		if address.Address == "" || !keepReachableAddress(address.Address) || skipTailscaleAddress(address.Address) {
			continue
		}
		if _, ok := seen[address.Address]; ok {
			continue
		}
		seen[address.Address] = struct{}{}
		merged = append(merged, address)
	}
	current.Addresses = merged
	current.Relays = unionStrings(current.Relays, observed.Relays)
	return current
}

func endpointSchemePort(cfg config.Config) (string, string) {
	if raw := strings.TrimSpace(cfg.API.AdvertiseURL); raw != "" {
		parsed, err := url.Parse(raw)
		if err == nil && parsed.Scheme != "" && parsed.Port() != "" {
			return parsed.Scheme, parsed.Port()
		}
	}
	scheme := "http"
	if cfg.API.RequiresTLS() {
		scheme = "https"
	}
	port := "22420"
	if _, parsedPort, err := net.SplitHostPort(cfg.API.Listen); err == nil && parsedPort != "" {
		port = parsedPort
	}
	return scheme, port
}

func normalizeList(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func unionStrings(left, right []string) []string {
	return normalizeList(append(append([]string{}, left...), right...))
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

func usableLinkIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || routing.IsContainerBridgeIP(ip) {
		return false
	}
	return true
}

func keepReachableAddress(address string) bool {
	return routing.ClassifyEndpointNetwork(address) != routing.ContainerBridgeNetwork
}

func skipTailscaleAddress(address string) bool {
	return advertise.ExcludeTailscale() && advertise.IsTailscaleHost(address)
}

// skipUnverifiedPublic drops a literal public IP that has not answered a
// dial-back. Hostnames stay. Relay and observed addresses are not endpoints
// until the peer proves it serves the API there.
func skipUnverifiedPublic(address string) bool {
	host := endpointHost(address)
	ip := net.ParseIP(host)
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || advertise.IsTailscaleHost(host) {
		return false
	}
	if !ip.IsGlobalUnicast() {
		return false
	}
	port := 22420
	if parsed, err := url.Parse(address); err == nil && parsed.Port() != "" {
		if n, convErr := strconv.Atoi(parsed.Port()); convErr == nil {
			port = n
		}
	}
	return !advertise.PublicDialbackOK(host, port)
}

func gatewayAddress(address string) bool {
	ip := net.ParseIP(endpointHost(address))
	return ip != nil && advertise.IsDefaultGateway(ip)
}

func endpointHost(address string) string {
	address = strings.TrimSpace(address)
	if strings.Contains(address, "://") {
		parsed, err := url.Parse(address)
		if err != nil {
			return ""
		}
		return parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return strings.Trim(address, "[]")
}
