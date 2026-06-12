package discovery

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	corediscovery "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	routingdiscovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	ma "github.com/multiformats/go-multiaddr"
)

const dhtQueryLimit = 64

// Libp2pDHTRouter is the concrete public-DHT adapter for the discovery.Source
// seam. It owns a libp2p host and Kademlia DHT instance, bootstraps against
// public multiaddrs, advertises the configured application namespace, and finds
// peers advertising the same namespace. Manual/configured peers remain wired as
// separate static discovery sources by the daemon.
type Libp2pDHTRouter struct {
	host      host.Host
	dht       *dht.IpfsDHT
	discovery *routingdiscovery.RoutingDiscovery
	mu        sync.Mutex
	advertise map[string]struct{}
}

type Libp2pDHTRouterOptions struct {
	NodeID string
}

func NewLibp2pDHTRouter(ctx context.Context, opts Libp2pDHTRouterOptions) (*Libp2pDHTRouter, error) {
	h, err := libp2p.New()
	if err != nil {
		return nil, err
	}
	kad, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))
	if err != nil {
		_ = h.Close()
		return nil, err
	}
	return &Libp2pDHTRouter{
		host:      h,
		dht:       kad,
		discovery: routingdiscovery.NewRoutingDiscovery(kad),
		advertise: map[string]struct{}{},
	}, nil
}

func (r *Libp2pDHTRouter) Bootstrap(ctx context.Context, peers []string) error {
	if r == nil || r.host == nil || r.dht == nil {
		return fmt.Errorf("libp2p DHT router is not initialized")
	}
	infos, err := parsePeerAddrInfos(peers)
	if err != nil {
		return err
	}
	for _, info := range infos {
		r.host.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = r.host.Connect(connectCtx, info)
		cancel()
	}
	return r.dht.Bootstrap(ctx)
}

func (r *Libp2pDHTRouter) FindPeers(ctx context.Context, namespace string) ([]Peer, error) {
	if r == nil || r.discovery == nil || r.host == nil {
		return nil, fmt.Errorf("libp2p DHT router is not initialized")
	}
	if namespace == "" {
		return nil, fmt.Errorf("public DHT namespace is required")
	}
	r.mu.Lock()
	if _, ok := r.advertise[namespace]; !ok {
		if _, err := r.discovery.Advertise(ctx, namespace); err != nil {
			r.mu.Unlock()
			return nil, err
		}
		r.advertise[namespace] = struct{}{}
	}
	r.mu.Unlock()
	peerCh, err := r.discovery.FindPeers(ctx, namespace, corediscovery.Limit(dhtQueryLimit))
	if err != nil {
		return nil, err
	}
	peers := []Peer{}
	for info := range peerCh {
		if info.ID == "" || info.ID == r.host.ID() {
			continue
		}
		addresses := peerAddresses(info)
		if len(addresses) == 0 {
			continue
		}
		peers = append(peers, Peer{ID: info.ID.String(), Addresses: addresses})
		if len(peers) >= dhtQueryLimit {
			break
		}
	}
	return peers, nil
}

func (r *Libp2pDHTRouter) Close() error {
	if r == nil || r.host == nil {
		return nil
	}
	return r.host.Close()
}

func parsePeerAddrInfos(addresses []string) ([]peer.AddrInfo, error) {
	infos := make([]peer.AddrInfo, 0, len(addresses))
	for _, address := range addresses {
		if address == "" {
			continue
		}
		multiaddr, err := ma.NewMultiaddr(address)
		if err != nil {
			return nil, fmt.Errorf("parse DHT bootstrap peer %q: %w", address, err)
		}
		info, err := peer.AddrInfoFromP2pAddr(multiaddr)
		if err != nil {
			return nil, fmt.Errorf("parse DHT bootstrap peer %q: %w", address, err)
		}
		infos = append(infos, *info)
	}
	return infos, nil
}

func peerAddresses(info peer.AddrInfo) []string {
	addresses := make([]string, 0, len(info.Addrs))
	for _, addr := range info.Addrs {
		withPeer := addr.Encapsulate(ma.StringCast("/p2p/" + info.ID.String()))
		addresses = append(addresses, withPeer.String())
	}
	sort.Strings(addresses)
	return addresses
}
