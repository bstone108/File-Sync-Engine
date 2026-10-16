package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnableShippedDefaultDHTTurnsHistoricalDefaultOn(t *testing.T) {
	t.Setenv("FSE_DISCOVERY_DHT", "")
	os.Unsetenv("FSE_DISCOVERY_DHT")
	cfg := Config{Discovery: DiscoveryConfig{DHT: false, Local: true, DHTBootstrapPeers: []string{shippedDHTBootstrapPeer}}}
	updated, changed := EnableShippedDefaultDHT(cfg)
	if !changed || !updated.Discovery.DHT {
		t.Fatalf("historical default was not enabled: changed=%v dht=%v", changed, updated.Discovery.DHT)
	}
}

func TestEnableShippedDefaultDHTHonorsExplicitEnvironment(t *testing.T) {
	t.Setenv("FSE_DISCOVERY_DHT", "false")
	cfg := Config{Discovery: DiscoveryConfig{DHT: false, Local: true, DHTBootstrapPeers: []string{shippedDHTBootstrapPeer}}}
	updated, changed := EnableShippedDefaultDHT(cfg)
	if changed || updated.Discovery.DHT {
		t.Fatalf("explicit false was overridden: changed=%v dht=%v", changed, updated.Discovery.DHT)
	}

	t.Setenv("FSE_DISCOVERY_DHT", "true")
	updated, changed = EnableShippedDefaultDHT(cfg)
	if !changed || !updated.Discovery.DHT {
		t.Fatalf("explicit true was not applied: changed=%v dht=%v", changed, updated.Discovery.DHT)
	}
}

func TestEnableShippedDefaultDHTLeavesCustomDiscoveryAlone(t *testing.T) {
	os.Unsetenv("FSE_DISCOVERY_DHT")
	for _, cfg := range []Config{
		{Discovery: DiscoveryConfig{DHT: true, Local: true}},
		{Discovery: DiscoveryConfig{Disabled: true, Local: true}},
		{Discovery: DiscoveryConfig{Local: false}},
		{Discovery: DiscoveryConfig{Local: true, DHTBootstrapPeers: []string{"/ip4/203.0.113.5/tcp/4001"}}},
	} {
		updated, changed := EnableShippedDefaultDHT(cfg)
		if changed || updated.Discovery.DHT != cfg.Discovery.DHT {
			t.Fatalf("custom discovery changed: before=%+v after=%+v changed=%v", cfg.Discovery, updated.Discovery, changed)
		}
	}
}

func TestPersistShippedDefaultDHTWritesTheSwitch(t *testing.T) {
	os.Unsetenv("FSE_DISCOVERY_DHT")
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Config{
		NodeName:  "node-a",
		Discovery: DiscoveryConfig{DHT: false, Local: true, DHTBootstrapPeers: []string{shippedDHTBootstrapPeer}},
	}
	updated, err := PersistShippedDefaultDHT(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Discovery.DHT {
		t.Fatal("dht was not enabled")
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Discovery.DHT || !loaded.Discovery.Local {
		t.Fatalf("persisted discovery = %+v", loaded.Discovery)
	}
}
