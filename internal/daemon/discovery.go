package daemon

import (
	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
)

// DiscoverySourcesFromConfig wires configured peer discovery sources for the
// daemon runtime. Manual/configured peers are always kept first-class; automatic
// sources are skipped when discovery.disabled is set.
func DiscoverySourcesFromConfig(cfg config.Config, dhtRouter discovery.DHTRouter) []discovery.Source {
	sources := []discovery.Source{}
	manualPeers := manualDiscoveryPeers(cfg.Peers)
	if len(manualPeers) > 0 {
		sources = append(sources, discovery.NewStaticSource(manualPeers))
	}
	if cfg.Discovery.Disabled {
		return sources
	}
	if cfg.Discovery.DHT {
		namespace := cfg.Discovery.DHTNamespace
		if namespace == "" {
			namespace = discovery.DefaultPublicDHTNamespace
		}
		sources = append(sources, discovery.NewPublicDHTSource(dhtRouter, discovery.PublicDHTOptions{
			Namespace:      namespace,
			SelfID:         cfg.NodeName,
			BootstrapPeers: cfg.Discovery.DHTBootstrapPeers,
		}))
	}
	return sources
}

func manualDiscoveryPeers(peers []config.PeerConfig) []discovery.Peer {
	out := make([]discovery.Peer, 0, len(peers))
	for _, peer := range peers {
		addresses := append([]string(nil), peer.Addresses...)
		for _, endpoint := range peer.Endpoints {
			if endpoint.Kind == "manual" && endpoint.Address != "" {
				addresses = append(addresses, endpoint.Address)
			}
		}
		if peer.ID != "" && len(addresses) > 0 {
			out = append(out, discovery.Peer{ID: peer.ID, Addresses: addresses})
		}
	}
	return out
}
