package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	corediscovery "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
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
	host           host.Host
	dht            *dht.IpfsDHT
	discovery      *routingdiscovery.RoutingDiscovery
	mu             sync.Mutex
	advertise      map[string]struct{}
	payloads       map[string][]byte
	bootstrapped   bool
	bootstrapPeers []string
	resolver       multiaddrResolver
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
	router := &Libp2pDHTRouter{
		host:      h,
		dht:       kad,
		discovery: routingdiscovery.NewRoutingDiscovery(kad),
		advertise: map[string]struct{}{},
		payloads:  map[string][]byte{},
	}
	h.SetStreamHandler(protocol.ID(PairingProtocol), router.handlePairingStream)
	return router, nil
}

func (r *Libp2pDHTRouter) Bootstrap(ctx context.Context, peers []string) error {
	if r == nil || r.host == nil || r.dht == nil {
		return fmt.Errorf("libp2p DHT router is not initialized")
	}
	infos, err := parsePeerAddrInfosWith(ctx, peers, r.activeResolver())
	if err != nil {
		return err
	}
	infos = append(r.extraBootstrapInfos(peers), infos...)
	if r.connectBootstrap(ctx, preferDialableTransports(infos)) > 0 {
		r.waitForRoutingTable(ctx)
	}
	return r.dht.Bootstrap(ctx)
}

// extraBootstrapInfos adds the libp2p TCP bootstrap peer when the daemon is
// using the built-in public list. It is not written to config. A test resolver
// keeps the fixture-only dial list.
func (r *Libp2pDHTRouter) extraBootstrapInfos(peers []string) []peer.AddrInfo {
	if r == nil || !samePeers(peers, DefaultPublicDHTBootstrapPeers) {
		return nil
	}
	r.mu.Lock()
	customResolver := r.resolver != nil
	r.mu.Unlock()
	if customResolver {
		return nil
	}
	addr, err := ma.NewMultiaddr("/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ")
	if err != nil {
		return nil
	}
	info, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil || info == nil {
		return nil
	}
	return []peer.AddrInfo{*info}
}

func samePeers(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// SetResolver replaces DNS multiaddr resolution. Tests use it to expand the
// default /dnsaddr bootstrap without querying the public DNS.
func (r *Libp2pDHTRouter) SetResolver(resolver multiaddrResolver) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolver = resolver
}

// SetBootstrapPeers selects the peers used the next time pairing bootstraps.
// An empty list keeps the public default, including /dnsaddr/bootstrap.libp2p.io.
func (r *Libp2pDHTRouter) SetBootstrapPeers(peers []string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(peers) == 0 {
		r.bootstrapPeers = nil
		return
	}
	r.bootstrapPeers = append([]string(nil), peers...)
}

func (r *Libp2pDHTRouter) bootstrapPeerList() []string {
	if r == nil {
		return append([]string(nil), DefaultPublicDHTBootstrapPeers...)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bootstrapPeers) == 0 {
		return append([]string(nil), DefaultPublicDHTBootstrapPeers...)
	}
	return append([]string(nil), r.bootstrapPeers...)
}

func (r *Libp2pDHTRouter) connectBootstrap(ctx context.Context, infos []peer.AddrInfo) int {
	attempts := singleTransportDials(infos)
	limit := len(attempts)
	if limit > maxBootstrapCandidates {
		limit = maxBootstrapCandidates
	}
	// One address at a time. Concurrent dials of the public /dnsaddr peers
	// fail together from some networks, while the same addresses connect
	// when each dial finishes before the next one starts.
	for _, info := range attempts[:limit] {
		r.host.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
		connectCtx, cancel := context.WithTimeout(ctx, bootstrapDialTimeout)
		err := r.host.Connect(connectCtx, info)
		cancel()
		if err == nil {
			return 1
		}
	}
	return 0
}

// singleTransportDials separates each address. libp2p treats a combined TCP,
// WebSocket, and QUIC list as one dial and all three fail together on the
// public bootstrap, while the same /dnsaddr/p2p address connects alone.
func singleTransportDials(infos []peer.AddrInfo) []peer.AddrInfo {
	out := make([]peer.AddrInfo, 0, len(infos))
	for _, info := range infos {
		if info.ID == "" {
			continue
		}
		for _, addr := range info.Addrs {
			if addr == nil || strings.Contains(addr.String(), "/quic") {
				continue
			}
			out = append(out, peer.AddrInfo{ID: info.ID, Addrs: []ma.Multiaddr{addr}})
		}
	}
	return out
}

func (r *Libp2pDHTRouter) waitForRoutingTable(ctx context.Context) {
	if r == nil || r.dht == nil || r.dht.RoutingTable() == nil {
		return
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if r.dht.RoutingTable().Size() > 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
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

func peerAddresses(info peer.AddrInfo) []string {
	addresses := make([]string, 0, len(info.Addrs))
	for _, addr := range info.Addrs {
		withPeer := addr.Encapsulate(ma.StringCast("/p2p/" + info.ID.String()))
		addresses = append(addresses, withPeer.String())
	}
	sort.Strings(addresses)
	return addresses
}
