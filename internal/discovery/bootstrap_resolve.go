package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
)

const (
	maxDNSAddrDepth        = 6
	maxBootstrapDials      = 6
	maxBootstrapCandidates = 24
	bootstrapDialTimeout   = 8 * time.Second
	bootstrapResolveBudget = 8 * time.Second
)

// multiaddrResolver expands one DNS component of a multiaddr. go-multiaddr-dns
// resolves only the first dns/dns4/dns6/dnsaddr component, so callers recurse.
type multiaddrResolver interface {
	Resolve(context.Context, ma.Multiaddr) ([]ma.Multiaddr, error)
}

func (r *Libp2pDHTRouter) activeResolver() multiaddrResolver {
	if r == nil {
		return madns.DefaultResolver
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolver != nil {
		return r.resolver
	}
	return madns.DefaultResolver
}

func parsePeerAddrInfos(ctx context.Context, addresses []string) ([]peer.AddrInfo, error) {
	return parsePeerAddrInfosWith(ctx, addresses, madns.DefaultResolver)
}

func parsePeerAddrInfosWith(ctx context.Context, addresses []string, resolver multiaddrResolver) ([]peer.AddrInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resolveCtx, cancel := context.WithTimeout(ctx, bootstrapResolveBudget)
	defer cancel()
	var infos []peer.AddrInfo
	for _, address := range addresses {
		if address == "" {
			continue
		}
		expanded, err := expandBootstrapAddress(resolveCtx, address, resolver, 0)
		if err != nil {
			return nil, err
		}
		parsed, err := addrInfosFromMultiaddrs(address, expanded)
		if err != nil {
			return nil, err
		}
		infos = append(infos, parsed...)
	}
	return mergeAddrInfos(infos), nil
}

// preferDialableTransports puts plain TCP addresses ahead of websocket and
// QUIC ones. Public dnsaddr results list QUIC and websocket first, and those
// are not useful for the short pairing dial.
func preferDialableTransports(infos []peer.AddrInfo) []peer.AddrInfo {
	out := make([]peer.AddrInfo, len(infos))
	for i, info := range infos {
		addrs := append([]ma.Multiaddr(nil), info.Addrs...)
		sort.SliceStable(addrs, func(a, b int) bool {
			return transportRank(addrs[a]) < transportRank(addrs[b])
		})
		out[i] = peer.AddrInfo{ID: info.ID, Addrs: addrs}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return transportRank(firstAddr(out[a])) < transportRank(firstAddr(out[b]))
	})
	return out
}

func firstAddr(info peer.AddrInfo) ma.Multiaddr {
	if len(info.Addrs) == 0 {
		return nil
	}
	return info.Addrs[0]
}

func transportRank(addr ma.Multiaddr) int {
	if addr == nil {
		return 3
	}
	raw := addr.String()
	switch {
	case strings.Contains(raw, "/tcp/") && !strings.Contains(raw, "/ws"):
		return 0
	case strings.Contains(raw, "/tcp/"):
		return 1
	default:
		return 2
	}
}

func expandBootstrapAddress(ctx context.Context, address string, resolver multiaddrResolver, depth int) ([]ma.Multiaddr, error) {
	multiaddr, err := ma.NewMultiaddr(address)
	if err != nil {
		return nil, fmt.Errorf("parse DHT bootstrap peer %q: %w", address, err)
	}
	return expandBootstrapMultiaddr(ctx, multiaddr, resolver, depth)
}

func expandBootstrapMultiaddr(ctx context.Context, multiaddr ma.Multiaddr, resolver multiaddrResolver, depth int) ([]ma.Multiaddr, error) {
	// Only a /dnsaddr with no peer ID is expanded here. A result that already
	// names /p2p/<id> is dialed as written so libp2p resolves it. Expanding
	// those records to /ip4, or handing TCP, WebSocket, and QUIC to one dial,
	// fails the handshake. /dns4 and /dns6 stay as hostnames for the same reason.
	if multiaddr == nil || !multiaddrHasDNSAddr(multiaddr) || multiaddrHasPeerID(multiaddr) {
		return []ma.Multiaddr{multiaddr}, nil
	}
	if depth >= maxDNSAddrDepth {
		return nil, nil
	}
	if resolver == nil {
		return nil, fmt.Errorf("resolve DHT bootstrap peer %q: dns resolver is not configured", multiaddr.String())
	}
	resolved, err := resolver.Resolve(ctx, multiaddr)
	if err != nil {
		return nil, fmt.Errorf("resolve DHT bootstrap peer %q: %w", multiaddr.String(), err)
	}
	out := make([]ma.Multiaddr, 0, len(resolved))
	for _, item := range resolved {
		if item == nil {
			continue
		}
		if multiaddrHasDNSAddr(item) {
			nested, err := expandBootstrapMultiaddr(ctx, item, resolver, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func addrInfosFromMultiaddrs(original string, addrs []ma.Multiaddr) ([]peer.AddrInfo, error) {
	// Dial the resolved transport. Replacing every result with the parent
	// /dnsaddr (which names no peer) makes Connect wait out the dial timeout
	// on an address libp2p cannot open, so the routing table stays empty and
	// neither pairing nor relay reservation can start.
	infos := make([]peer.AddrInfo, 0, len(addrs))
	var lastErr error
	for _, addr := range addrs {
		if addr == nil {
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			lastErr = err
			continue
		}
		infos = append(infos, *info)
	}
	if len(infos) > 0 {
		return infos, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("invalid p2p multiaddr")
	}
	return nil, fmt.Errorf("parse DHT bootstrap peer %q: %w", original, lastErr)
}

func mergeAddrInfos(infos []peer.AddrInfo) []peer.AddrInfo {
	order := make([]peer.ID, 0, len(infos))
	byID := map[peer.ID][]ma.Multiaddr{}
	seenAddr := map[peer.ID]map[string]struct{}{}
	for _, info := range infos {
		if info.ID == "" {
			continue
		}
		if _, ok := seenAddr[info.ID]; !ok {
			order = append(order, info.ID)
			seenAddr[info.ID] = map[string]struct{}{}
		}
		for _, addr := range info.Addrs {
			if addr == nil {
				continue
			}
			if _, ok := seenAddr[info.ID][addr.String()]; ok {
				continue
			}
			seenAddr[info.ID][addr.String()] = struct{}{}
			byID[info.ID] = append(byID[info.ID], addr)
		}
	}
	out := make([]peer.AddrInfo, 0, len(order))
	for _, id := range order {
		out = append(out, peer.AddrInfo{ID: id, Addrs: byID[id]})
	}
	return out
}

func multiaddrHasDNSAddr(multiaddr ma.Multiaddr) bool {
	return multiaddrHasProtocol(multiaddr, ma.P_DNSADDR)
}

func multiaddrHasPeerID(multiaddr ma.Multiaddr) bool {
	return multiaddrHasProtocol(multiaddr, ma.P_P2P)
}

func multiaddrHasProtocol(multiaddr ma.Multiaddr, code int) bool {
	if multiaddr == nil {
		return false
	}
	found := false
	ma.ForEach(multiaddr, func(component ma.Component) bool {
		if component.Protocol().Code == code {
			found = true
			return false
		}
		return true
	})
	return found
}
