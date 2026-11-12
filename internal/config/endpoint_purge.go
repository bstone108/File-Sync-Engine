package config

import (
	"encoding/json"
	"strings"
)

// PurgeUnverifiedEndpoints removes every peer endpoint the paired peer did
// not authenticate, and every relay that is not a circuit to that peer.
// DHT-learned, observed, and relay API URLs do not survive an upgrade.
func PurgeUnverifiedEndpoints(cfg Config) (Config, bool) {
	changed := false
	for i := range cfg.Peers {
		keptEndpoints := make([]EndpointConfig, 0, len(cfg.Peers[i].Endpoints))
		for _, endpoint := range cfg.Peers[i].Endpoints {
			if !endpoint.Authenticated {
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
			if unverifiedRelay(relay, cfg.Peers[i].DHTPeerID) {
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

// PersistUnverifiedEndpointPurge writes the config when unverified endpoints
// or relays were removed.
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

func unverifiedRelay(relay, dhtPeerID string) bool {
	relay = strings.TrimSpace(relay)
	if relay == "" || strings.Contains(relay, "://") || !strings.HasPrefix(relay, "/") {
		return true
	}
	target := multiaddrRelayTarget(relay)
	dhtPeerID = strings.TrimSpace(dhtPeerID)
	return target == "" || dhtPeerID == "" || target != dhtPeerID
}

func multiaddrRelayTarget(address string) string {
	parts := strings.Split(address, "/p2p/")
	if len(parts) < 2 {
		return ""
	}
	last := parts[len(parts)-1]
	if cut := strings.Index(last, "/"); cut >= 0 {
		last = last[:cut]
	}
	return strings.TrimSpace(last)
}
