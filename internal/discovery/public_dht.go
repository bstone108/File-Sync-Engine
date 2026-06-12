package discovery

import (
	"context"
	"errors"
	"sort"
)

const DefaultPublicDHTNamespace = "filesyncengine/v1"

var DefaultPublicDHTBootstrapPeers = []string{
	"/dnsaddr/bootstrap.libp2p.io",
}

type DHTRouter interface {
	Bootstrap(ctx context.Context, peers []string) error
	FindPeers(ctx context.Context, namespace string) ([]Peer, error)
}

type PublicDHTOptions struct {
	Namespace      string
	SelfID         string
	BootstrapPeers []string
}

type PublicDHTSource struct {
	router  DHTRouter
	options PublicDHTOptions
}

func NewPublicDHTSource(router DHTRouter, options PublicDHTOptions) PublicDHTSource {
	return PublicDHTSource{router: router, options: options}
}

func (s PublicDHTSource) Peers() ([]Peer, error) {
	return s.PeersContext(context.Background())
}

func (s PublicDHTSource) PeersContext(ctx context.Context) ([]Peer, error) {
	if s.router == nil {
		return nil, errors.New("public DHT router is required")
	}
	namespace := s.options.Namespace
	if namespace == "" {
		return nil, errors.New("public DHT namespace is required")
	}
	bootstrapPeers := append([]string(nil), s.options.BootstrapPeers...)
	if len(bootstrapPeers) == 0 {
		bootstrapPeers = append([]string(nil), DefaultPublicDHTBootstrapPeers...)
	}
	if err := s.router.Bootstrap(ctx, bootstrapPeers); err != nil {
		return nil, err
	}
	peers, err := s.router.FindPeers(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return normalizeDiscoveredPeers(peers, s.options.SelfID), nil
}

func normalizeDiscoveredPeers(peers []Peer, selfID string) []Peer {
	peerAddresses := map[string]map[string]struct{}{}
	for _, peer := range peers {
		if peer.ID == "" || peer.ID == selfID {
			continue
		}
		addresses := peerAddresses[peer.ID]
		if addresses == nil {
			addresses = map[string]struct{}{}
			peerAddresses[peer.ID] = addresses
		}
		for _, address := range peer.Addresses {
			if address != "" {
				addresses[address] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(peerAddresses))
	for id := range peerAddresses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Peer, 0, len(ids))
	for _, id := range ids {
		addresses := make([]string, 0, len(peerAddresses[id]))
		for address := range peerAddresses[id] {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		out = append(out, Peer{ID: id, Addresses: addresses})
	}
	return out
}
