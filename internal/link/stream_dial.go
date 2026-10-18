package link

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/nat"
)

type dialCacheEntry struct {
	base   string
	client *http.Client
	at     time.Time
}

var (
	dialCacheMu sync.Mutex
	dialCache   = map[string]dialCacheEntry{}
)

func cachedDial(peerID string) (string, *http.Client, bool) {
	if peerID == "" {
		return "", nil, false
	}
	dialCacheMu.Lock()
	defer dialCacheMu.Unlock()
	entry, ok := dialCache[peerID]
	if !ok || entry.client == nil || time.Since(entry.at) > 45*time.Second {
		return "", nil, false
	}
	return entry.base, entry.client, true
}

func rememberDial(peerID, base string, client *http.Client) {
	if peerID == "" || client == nil || base == "" {
		return
	}
	dialCacheMu.Lock()
	dialCache[peerID] = dialCacheEntry{base: base, client: client, at: time.Now()}
	dialCacheMu.Unlock()
}

func forgetDial(peerID string) {
	dialCacheMu.Lock()
	delete(dialCache, peerID)
	dialCacheMu.Unlock()
}

func resetDialCache() {
	dialCacheMu.Lock()
	dialCache = map[string]dialCacheEntry{}
	dialCacheMu.Unlock()
}

func dialLibp2p(ctx context.Context, peer config.PeerConfig, do func(context.Context, string, *http.Client) error) (string, error) {
	targets := libp2pTargets(peer)
	if len(targets) == 0 {
		return "", errNoStream
	}
	var last error
	for _, target := range targets {
		session, err := discovery.DialStream(ctx, target.id, target.addrs)
		if err != nil {
			last = err
			continue
		}
		err = do(ctx, discovery.StreamBaseURL, session.Client)
		if err == nil {
			path := session.Path
			if path == "" {
				path = nat.PathRelay
			}
			rememberDial(peer.ID, discovery.StreamBaseURL, session.Client)
			nat.DefaultPaths.Observe(peer.ID, session.Remote, path)
			if peer.DHTPeerID != "" {
				nat.DefaultPaths.Observe(peer.DHTPeerID, session.Remote, path)
			}
			discovery.NotePeerReachable(peer.ID)
			return discovery.StreamBaseURL, nil
		}
		if !failoverError(err) {
			return "", answeredError{err: err}
		}
		last = err
	}
	if last == nil {
		last = errNoStream
	}
	return "", last
}

var errNoStream = streamDialError("peer has no libp2p address")

type streamDialError string

func (e streamDialError) Error() string { return string(e) }

type libp2pTarget struct {
	id    string
	addrs []string
}

func libp2pTargets(peer config.PeerConfig) []libp2pTarget {
	raw := append([]string{}, peer.Relays...)
	raw = append(raw, peer.Addresses...)
	for _, endpoint := range peer.Endpoints {
		raw = append(raw, endpoint.Address)
	}
	exchanged, local, dht := discovery.AddressesFor(peer.ID)
	raw = append(raw, exchanged...)
	raw = append(raw, local...)
	raw = append(raw, dht...)
	if peer.DHTPeerID != "" && peer.DHTPeerID != peer.ID {
		moreExchanged, moreLocal, moreDHT := discovery.AddressesFor(peer.DHTPeerID)
		raw = append(raw, moreExchanged...)
		raw = append(raw, moreLocal...)
		raw = append(raw, moreDHT...)
	}
	seen := map[string]struct{}{}
	var addrs []string
	for _, address := range raw {
		address = strings.TrimSpace(address)
		if address == "" || strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
			continue
		}
		address = CircuitForPeer(address, peer.DHTPeerID)
		if !KeepPairedAddress(address, peer.DHTPeerID) {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addrs = append(addrs, address)
	}
	if len(addrs) == 0 || strings.TrimSpace(peer.DHTPeerID) == "" {
		return nil
	}
	return []libp2pTarget{{id: peer.DHTPeerID, addrs: addrs}}
}
