package config

import (
	"encoding/json"
	"os"
	"strings"
)

const shippedDHTBootstrapPeer = "/dnsaddr/bootstrap.libp2p.io"

// EnableShippedDefaultDHT turns public DHT discovery on for installs that
// still have the historical default (dht off, local on, public bootstrap).
// FSE_DISCOVERY_DHT, when set, is the operator's choice and is applied as-is.
func EnableShippedDefaultDHT(cfg Config) (Config, bool) {
	if value, ok := os.LookupEnv("FSE_DISCOVERY_DHT"); ok && strings.TrimSpace(value) != "" {
		want := strings.EqualFold(strings.TrimSpace(value), "true")
		if cfg.Discovery.DHT == want {
			return cfg, false
		}
		cfg.Discovery.DHT = want
		return cfg, true
	}
	if cfg.Discovery.DHT || cfg.Discovery.Disabled || !cfg.Discovery.Local {
		return cfg, false
	}
	if !shippedBootstrap(cfg.Discovery.DHTBootstrapPeers) {
		return cfg, false
	}
	cfg.Discovery.DHT = true
	return cfg, true
}

// PersistShippedDefaultDHT stores the default DHT switch when it changes.
func PersistShippedDefaultDHT(path string, cfg Config) (Config, error) {
	updated, changed := EnableShippedDefaultDHT(cfg)
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

func shippedBootstrap(peers []string) bool {
	if len(peers) == 0 {
		return true
	}
	return len(peers) == 1 && peers[0] == shippedDHTBootstrapPeer
}
