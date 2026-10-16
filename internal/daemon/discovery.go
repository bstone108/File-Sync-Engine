package daemon

import (
	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

// DiscoverySourcesFromConfig wires discovery in Brandon's order:
// saved connection details, peer exchange and same-identity connection
// details, LAN announcements, then the public DHT as a backup.
// The DHT source stays installed while discovery.dht is on, and its poll
// runs only when NeedsDHTBackup allows it. Pairing-code lookup uses the
// same router through a separate directory.
func DiscoverySourcesFromConfig(cfg config.Config, dhtRouter discovery.DHTRouter) []discovery.Source {
	sources := []discovery.Source{}
	manualPeers := manualDiscoveryPeers(cfg.Peers)
	if len(manualPeers) > 0 {
		sources = append(sources, discovery.NewStaticSource(manualPeers))
	}
	if cfg.Discovery.Disabled {
		return sources
	}
	if exchanged := discovery.ExchangedPeers(); len(exchanged) > 0 {
		sources = append(sources, discovery.NewStaticSource(exchanged))
	}
	if cfg.Discovery.Local {
		if local := discovery.LocalSource(); local != nil {
			sources = append(sources, local)
		}
	}
	if cfg.Discovery.DHT {
		namespace := cfg.Discovery.DHTNamespace
		if namespace == "" {
			namespace = discovery.DefaultPublicDHTNamespace
		}
		inner := discovery.NewPublicDHTSource(dhtRouter, discovery.PublicDHTOptions{
			Namespace:      namespace,
			SelfID:         cfg.NodeName,
			BootstrapPeers: cfg.Discovery.DHTBootstrapPeers,
		})
		peers := append([]config.PeerConfig(nil), cfg.Peers...)
		sources = append(sources, discovery.BackupDHTSource(inner, func() (bool, string) {
			return discovery.NeedsDHTBackup(peers, discovery.ExchangedPeers(), discovery.UnreachableIDs())
		}))
	}
	return sources
}

func manualDiscoveryPeers(peers []config.PeerConfig) []discovery.Peer {
	out := make([]discovery.Peer, 0, len(peers))
	for _, peer := range peers {
		addresses := append([]string(nil), peer.Addresses...)
		for _, endpoint := range peer.Endpoints {
			if endpoint.Address != "" && (endpoint.Kind == "manual" || endpoint.Kind == "vpn" || endpoint.Kind == "") {
				addresses = append(addresses, endpoint.Address)
			}
		}
		for _, relay := range peer.Relays {
			if relay != "" {
				addresses = append(addresses, relay)
			}
		}
		if peer.ID != "" && len(addresses) > 0 {
			out = append(out, discovery.Peer{ID: peer.ID, Addresses: addresses})
		}
	}
	return out
}
