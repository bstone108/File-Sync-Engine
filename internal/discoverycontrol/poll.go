package discoverycontrol

import (
	"strings"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/daemon"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/routing"
)

// BuildRuntimeSources assembles daemon discovery sources from config and injects
// the live public-DHT router only when automatic DHT discovery is enabled.
func BuildRuntimeSources(cfg config.Config, newDHTRouter func(config.Config) discovery.DHTRouter) []discovery.Source {
	var dhtRouter discovery.DHTRouter
	if cfg.Discovery.DHT && !cfg.Discovery.Disabled {
		dhtRouter = newDHTRouter(cfg)
	}
	return daemon.DiscoverySourcesFromConfig(cfg, dhtRouter)
}

type PollState interface {
	CurrentState() api.State
	UpdateState(api.State)
	Publish(api.Event)
}

// ProcessPoll collects source peers, publishes discovery events, merges newly
// discovered peers into API state, and returns live sidecar/helper observations
// for later runtime routing decisions.
func ProcessPoll(state PollState, sources []discovery.Source, networkHints routing.NetworkHints) []routing.EndpointObservation {
	if len(sources) == 0 {
		return nil
	}
	discovered, sourceEvents := PollSources(sources)
	for _, event := range sourceEvents {
		state.Publish(event)
	}
	updated, peerEvents := MergeDiscoveredPeers(state.CurrentState(), discovered)
	state.UpdateState(updated)
	for _, event := range peerEvents {
		state.Publish(event)
	}
	return EndpointObservationsFromDiscoveredPeers(discovered, networkHints)
}

// PollSources collects peers from each discovery source without letting one
// failed source abort later sources. Peers are deduplicated by stable peer ID in
// source order and source errors are returned as API events for daemon status.
func PollSources(sources []discovery.Source) ([]discovery.Peer, []api.Event) {
	peers := []discovery.Peer{}
	events := []api.Event{}
	seen := map[string]struct{}{}
	for _, source := range sources {
		discovered, err := source.Peers()
		if err != nil {
			events = append(events, api.Event{Type: "discovery.error", Message: err.Error()})
			continue
		}
		for _, peer := range discovered {
			if peer.ID == "" {
				continue
			}
			if _, exists := seen[peer.ID]; exists {
				continue
			}
			seen[peer.ID] = struct{}{}
			peers = append(peers, peer)
		}
	}
	return peers, events
}

// MergeDiscoveredPeers appends newly discovered peers to API state while
// preserving configured/manual peers and their endpoints.
func MergeDiscoveredPeers(state api.State, discovered []discovery.Peer) (api.State, []api.Event) {
	seen := map[string]struct{}{}
	for _, peer := range state.PeersState {
		if peer.ID != "" {
			seen[peer.ID] = struct{}{}
		}
	}
	events := []api.Event{}
	for _, peer := range discovered {
		if peer.ID == "" {
			continue
		}
		if _, exists := seen[peer.ID]; exists {
			continue
		}
		endpoint := "discovered"
		if len(peer.Addresses) > 0 {
			endpoint += ":" + peer.Addresses[0]
		}
		state.PeersState = append(state.PeersState, api.PeerState{ID: peer.ID, Status: "discovered", Endpoint: endpoint})
		state.Peers = len(state.PeersState)
		seen[peer.ID] = struct{}{}
		events = append(events, api.Event{Type: "peer.discovered", PeerID: peer.ID, Message: endpoint})
	}
	return state, events
}

// EndpointObservationsFromDiscoveredPeers converts live discovered sidecar or
// helper addresses into routing observations. Only API/stream-like addresses are
// promoted; relay/control addresses stay out of data-path candidate state.
func EndpointObservationsFromDiscoveredPeers(discovered []discovery.Peer, networkHints routing.NetworkHints) []routing.EndpointObservation {
	observations := []routing.EndpointObservation{}
	seen := map[string]struct{}{}
	for _, peer := range discovered {
		if peer.ID == "" {
			continue
		}
		for _, address := range peer.Addresses {
			if !IsLiveSidecarAddress(address) {
				continue
			}
			key := peer.ID + "\x00" + address
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			observation := routing.EndpointObservation{PeerID: peer.ID, Address: address, Reachable: true, Path: routing.DirectPath}
			if network := routing.ClassifyEndpointNetworkWithHints(address, networkHints); network == routing.LocalNetwork {
				observation.NetworkHint = string(routing.LocalNetwork)
			}
			observations = append(observations, observation)
		}
	}
	return observations
}

func IsLiveSidecarAddress(address string) bool {
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return true
	}
	_, ok := StreamTCPAddress(address)
	return ok
}

func StreamTCPAddress(address string) (string, bool) {
	if strings.HasPrefix(address, "tcp://") {
		return strings.TrimPrefix(address, "tcp://"), true
	}
	if strings.Contains(address, "://") || strings.HasPrefix(address, "/") {
		return "", false
	}
	if strings.Contains(address, ":") {
		return address, true
	}
	return "", false
}
