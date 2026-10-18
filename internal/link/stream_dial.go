package link

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/nat"
	"filesyncengine/internal/structuredlog"
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
	if id := strings.TrimSpace(peer.DHTPeerID); id != "" {
		_, _, dht := discovery.AddressesFor(id)
		targets = appendEphemeralTargets(targets, id, dht)
		if peer.ID != "" && peer.ID != id {
			_, _, more := discovery.AddressesFor(peer.ID)
			targets = appendEphemeralTargets(targets, id, more)
		}
	}
	if len(targets) == 0 && strings.TrimSpace(peer.DHTPeerID) != "" {
		if addrs := discovery.LookupPeerAddrs(ctx, peer.DHTPeerID); len(addrs) > 0 {
			discovery.NoteDHTPeers([]discovery.Peer{{ID: peer.DHTPeerID, Addresses: addrs}})
			_, _, dht := discovery.AddressesFor(peer.DHTPeerID)
			targets = appendEphemeralTargets(nil, peer.DHTPeerID, dht)
		}
	}
	if len(targets) == 0 {
		structuredlog.Event("info", "libp2p.stream.skip", "libp2p stream was not dialed", map[string]any{
			"peer":      peer.ID,
			"dhtPeerId": peer.DHTPeerID,
			"reason":    "peer has no libp2p address",
		})
		return "", errNoStream
	}
	structuredlog.Event("info", "libp2p.stream.dial", "dialing peer over a libp2p stream", map[string]any{
		"peer":      peer.ID,
		"dhtPeerId": peer.DHTPeerID,
		"addresses": targets[0].addrs,
	})
	var last error
	for _, target := range targets {
		session, err := discovery.DialStream(ctx, target.id, target.addrs)
		if err != nil {
			last = err
			continue
		}
		err = do(ctx, discovery.StreamBaseURL, session.Client)
		if err == nil {
			path := session.CurrentPath()
			if path == "" {
				path = nat.PathRelay
			}
			if remote := session.CurrentRemote(); remote != "" {
				discovery.MarkAddressAuthenticated(remote)
			}
			rememberDial(peer.ID, discovery.StreamBaseURL, session.Client)
			nat.DefaultPaths.Observe(peer.ID, session.CurrentRemote(), path)
			if peer.DHTPeerID != "" {
				nat.DefaultPaths.Observe(peer.DHTPeerID, session.CurrentRemote(), path)
			}
			discovery.NotePeerReachable(peer.ID)
			return discovery.StreamBaseURL, nil
		}
		if errors.Is(err, discovery.ErrLimitedRelay) {
			nat.DefaultPaths.ObserveLimited(peer.ID, session.CurrentRemote())
			if peer.DHTPeerID != "" {
				nat.DefaultPaths.ObserveLimited(peer.DHTPeerID, session.CurrentRemote())
			}
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
	exchanged, local, _ := discovery.AddressesFor(peer.ID)
	raw = append(raw, exchanged...)
	raw = append(raw, local...)
	if peer.DHTPeerID != "" && peer.DHTPeerID != peer.ID {
		moreExchanged, moreLocal, _ := discovery.AddressesFor(peer.DHTPeerID)
		raw = append(raw, moreExchanged...)
		raw = append(raw, moreLocal...)
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

// appendEphemeralTargets adds DHT or lookup addresses for this dial only.
// HTTP URLs and any address that names a different peer are left out, so a
// stranger in the DHT is never dialed and never sent application data.
func appendEphemeralTargets(targets []libp2pTarget, id string, addrs []string) []libp2pTarget {
	var kept []string
	seen := map[string]struct{}{}
	for _, target := range targets {
		for _, address := range target.addrs {
			seen[address] = struct{}{}
		}
	}
	for _, address := range addrs {
		address = strings.TrimSpace(address)
		if address == "" || strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
			continue
		}
		address = CircuitForPeer(address, id)
		if !KeepPairedAddress(address, id) {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		kept = append(kept, address)
	}
	if len(kept) == 0 {
		return targets
	}
	if len(targets) == 0 {
		return []libp2pTarget{{id: id, addrs: kept}}
	}
	targets[0].addrs = append(targets[0].addrs, kept...)
	return targets
}
