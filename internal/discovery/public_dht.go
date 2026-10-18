package discovery

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// publicDHTPollTimeout keeps a discovery poll from holding the daemon loop.
// Folder sync runs on that loop, so an unbounded DHT walk stalls file transfer.
var publicDHTPollTimeout = 8 * time.Second

// dhtWalkCooldown stops a backup lookup from walking the public DHT on every
// discovery poll. The next walk waits until this interval has passed.
var dhtWalkCooldown = 10 * time.Minute

var (
	dhtWalkMu sync.Mutex
	dhtWalks  = map[string]dhtWalkResult{}
)

type dhtWalkResult struct {
	at    time.Time
	peers []Peer
}

func resetDHTWalkCache() {
	dhtWalkMu.Lock()
	dhtWalks = map[string]dhtWalkResult{}
	dhtWalkMu.Unlock()
}

// ResetDHTWalkCacheForTest clears the backup-walk cooldown.
func ResetDHTWalkCacheForTest() { resetDHTWalkCache() }

func cachedDHTWalk(namespace string) ([]Peer, bool) {
	if namespace == "" || dhtWalkCooldown <= 0 {
		return nil, false
	}
	dhtWalkMu.Lock()
	defer dhtWalkMu.Unlock()
	prev, ok := dhtWalks[namespace]
	if !ok || time.Since(prev.at) >= dhtWalkCooldown {
		return nil, false
	}
	return append([]Peer(nil), prev.peers...), true
}

func rememberDHTWalk(namespace string, peers []Peer) {
	if namespace == "" {
		return
	}
	dhtWalkMu.Lock()
	dhtWalks[namespace] = dhtWalkResult{at: time.Now(), peers: append([]Peer(nil), peers...)}
	dhtWalkMu.Unlock()
}

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
	ctx, cancel := context.WithTimeout(context.Background(), publicDHTPollTimeout)
	defer cancel()
	return s.PeersContext(ctx)
}

func (s PublicDHTSource) PeersContext(ctx context.Context) ([]Peer, error) {
	if s.router == nil {
		return nil, errors.New("public DHT router is required")
	}
	if peers, ok := cachedDHTWalk(s.options.Namespace); ok {
		return peers, nil
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
		rememberDHTWalk(namespace, nil)
		return nil, err
	}
	peers, err := s.router.FindPeers(ctx, namespace)
	if err != nil {
		rememberDHTWalk(namespace, nil)
		return nil, err
	}
	peers = normalizeDiscoveredPeers(peers, s.options.SelfID)
	rememberDHTWalk(namespace, peers)
	return peers, nil
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
