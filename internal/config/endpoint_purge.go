package config

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
)

// PurgeUnverifiedEndpoints removes API endpoints that were invented from a
// relay, a DHT observation, or an upgrade leftover. A literal public address
// stays only when the paired peer authenticated it. Hostnames, LAN, and
// Tailscale addresses are kept.
func PurgeUnverifiedEndpoints(cfg Config) (Config, bool) {
	changed := false
	for i := range cfg.Peers {
		keptEndpoints := make([]EndpointConfig, 0, len(cfg.Peers[i].Endpoints))
		for _, endpoint := range cfg.Peers[i].Endpoints {
			if unverifiedPublicEndpoint(endpoint) {
				changed = true
				continue
			}
			keptEndpoints = append(keptEndpoints, endpoint)
		}
		if len(keptEndpoints) != len(cfg.Peers[i].Endpoints) {
			cfg.Peers[i].Endpoints = keptEndpoints
		}
		keptRelays := make([]string, 0, len(cfg.Peers[i].Relays))
		for _, relay := range cfg.Peers[i].Relays {
			if strings.Contains(relay, "://") && literalPublicHost(relay) {
				changed = true
				continue
			}
			keptRelays = append(keptRelays, relay)
		}
		if len(keptRelays) != len(cfg.Peers[i].Relays) {
			cfg.Peers[i].Relays = keptRelays
		}
	}
	return cfg, changed
}

// PersistUnverifiedEndpointPurge writes the config when unverified public
// endpoints were removed.
func PersistUnverifiedEndpointPurge(path string, cfg Config) (Config, error) {
	updated, changed := PurgeUnverifiedEndpoints(cfg)
	if !changed {
		return cfg, nil
	}
	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return cfg, err
	}
	if err := WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return cfg, err
	}
	return updated, nil
}

func unverifiedPublicEndpoint(endpoint EndpointConfig) bool {
	if endpoint.Authenticated || !literalPublicHost(endpoint.Address) {
		return false
	}
	return true
}

func literalPublicHost(address string) bool {
	host := address
	if strings.Contains(address, "://") {
		parsed, err := url.Parse(address)
		if err != nil {
			return false
		}
		host = parsed.Hostname()
	} else if splitHost, _, err := net.SplitHostPort(address); err == nil {
		host = splitHost
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return false
	}
	if ip.IsGlobalUnicast() && isTailscale(ip) {
		return false
	}
	return ip.IsGlobalUnicast()
}

func isTailscale(ip net.IP) bool {
	ip = ip.To16()
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && v4[1]&0xc0 == 64
	}
	return len(ip) == 16 && ip[0] == 0xfd && ip[1] == 0x7a && ip[2] == 0x11 && ip[3] == 0x5c
}
