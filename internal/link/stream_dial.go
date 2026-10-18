package link

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	if ctx == nil {
		ctx = context.Background()
	}
	id := strings.TrimSpace(peer.DHTPeerID)
	direct, circuits := streamDialPlan(peer)
	if id != "" {
		direct, circuits = appendCatalogAddrs(direct, circuits, id, id)
		if peer.ID != "" && peer.ID != id {
			direct, circuits = appendCatalogAddrs(direct, circuits, peer.ID, id)
		}
	}
	lookedUp := false
	if len(direct) == 0 && id != "" {
		// Relays are not a reason to skip the DHT. A direct address has to be
		// tried before a public circuit can use the budget.
		lookedDirect, lookedCircuits := noteLookupAddrs(ctx, id)
		direct = appendUniqueAddrs(direct, lookedDirect)
		circuits = appendUniqueAddrs(circuits, lookedCircuits)
		lookedUp = true
	}
	if len(direct) == 0 && len(circuits) == 0 {
		structuredlog.Event("info", "libp2p.stream.skip", "libp2p stream was not dialed", map[string]any{
			"peer":      peer.ID,
			"dhtPeerId": peer.DHTPeerID,
			"reason":    "peer has no libp2p address",
		})
		return "", errNoStream
	}
	var last error
	try := func(kind string, addrs []string, limit time.Duration) (string, bool) {
		if len(addrs) == 0 || ctx.Err() != nil {
			return "", false
		}
		dialCtx := ctx
		cancel := func() {}
		if limit > 0 && kind != "relay" && len(circuits) > 0 {
			var timeoutCancel context.CancelFunc
			dialCtx, timeoutCancel = context.WithTimeout(ctx, limit)
			cancel = timeoutCancel
		}
		defer cancel()
		structuredlog.Event("info", "libp2p.stream.dial", "dialing peer over a libp2p stream", map[string]any{
			"peer":      peer.ID,
			"dhtPeerId": id,
			"path":      kind,
			"addresses": addrs,
		})
		// The limit covers the dial only. A block transfer on the open stream
		// keeps the caller's context.
		used, err := dialStreamAddrs(ctx, dialCtx, peer, id, addrs, do)
		if err == nil {
			return used, true
		}
		var answered answeredError
		if errors.As(err, &answered) {
			last = answered
			return "", true
		}
		last = err
		return "", false
	}
	finish := func(used string, stop bool) (string, error, bool) {
		if !stop {
			return "", nil, false
		}
		var answered answeredError
		if errors.As(last, &answered) {
			return "", answered.err, true
		}
		return used, nil, true
	}
	if used, stop := try("direct", direct, 6*time.Second); stop {
		if base, err, done := finish(used, stop); done {
			return base, err
		}
	}
	// The guessed hostname port can be wrong. Ask the DHT for the peer's own
	// addresses before a public relay uses the rest of the budget.
	if !lookedUp && ctx.Err() == nil && id != "" && len(direct) > 0 {
		lookedDirect, lookedCircuits := noteLookupAddrs(ctx, id)
		circuits = appendUniqueAddrs(circuits, lookedCircuits)
		if used, stop := try("direct", lookedDirect, 4*time.Second); stop {
			if base, err, done := finish(used, stop); done {
				return base, err
			}
		}
	}
	if used, stop := try("relay", circuits, 0); stop {
		if base, err, done := finish(used, stop); done {
			return base, err
		}
	}
	if last == nil {
		last = errNoStream
	}
	var answered answeredError
	if errors.As(last, &answered) {
		return "", answered.err
	}
	return "", last
}

func dialStreamAddrs(parent, dialCtx context.Context, peer config.PeerConfig, id string, addrs []string, do func(context.Context, string, *http.Client) error) (string, error) {
	session, err := discovery.DialStream(dialCtx, id, addrs)
	if err != nil {
		return "", err
	}
	err = do(parent, discovery.StreamBaseURL, session.Client)
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
	return "", err
}

var errNoStream = streamDialError("peer has no libp2p address")

type streamDialError string

func (e streamDialError) Error() string { return string(e) }

type libp2pTarget struct {
	id    string
	addrs []string
}

func libp2pTargets(peer config.PeerConfig) []libp2pTarget {
	direct, circuits := streamDialPlan(peer)
	addrs := append(append([]string{}, direct...), circuits...)
	if len(addrs) == 0 || strings.TrimSpace(peer.DHTPeerID) == "" {
		return nil
	}
	return []libp2pTarget{{id: peer.DHTPeerID, addrs: addrs}}
}

// streamDialPlan splits dial candidates so a signed hostname is a direct
// libp2p address and a relay circuit stays behind it. HTTP URLs are not
// libp2p addresses. A stranger peer id is left out.
func streamDialPlan(peer config.PeerConfig) (direct, circuits []string) {
	id := strings.TrimSpace(peer.DHTPeerID)
	raw := append([]string{}, peer.Relays...)
	raw = append(raw, peer.Addresses...)
	for _, endpoint := range peer.Endpoints {
		raw = append(raw, endpoint.Address)
	}
	raw = append(raw, directFromSignedHostnames(peer)...)
	exchanged, local, _ := discovery.AddressesFor(peer.ID)
	raw = append(raw, exchanged...)
	raw = append(raw, local...)
	if id != "" && id != peer.ID {
		moreExchanged, moreLocal, _ := discovery.AddressesFor(id)
		raw = append(raw, moreExchanged...)
		raw = append(raw, moreLocal...)
	}
	return splitStreamAddrs(id, raw)
}

// directFromSignedHostnames turns a paired hostname into direct TCP and QUIC
// multiaddrs. The certificate pin already authenticated the name, so the dial
// does not have to wait on a public relay. Literal IPs are left alone: the
// DHT record has the port a public address should use.
func directFromSignedHostnames(peer config.PeerConfig) []string {
	id := strings.TrimSpace(peer.DHTPeerID)
	if id == "" {
		return nil
	}
	port := discovery.Libp2pListenPort()
	if port < 1 || port > 65535 {
		port = discovery.DefaultLibp2pPort
	}
	var out []string
	seen := map[string]struct{}{}
	for _, endpoint := range peer.Endpoints {
		if !endpoint.Authenticated || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(endpoint.Address)), "https://") {
			continue
		}
		if !keepDialEndpoint(endpoint, id) {
			continue
		}
		parsed, err := url.Parse(endpoint.Address)
		if err != nil {
			continue
		}
		host := parsed.Hostname()
		if host == "" || net.ParseIP(host) != nil {
			continue
		}
		for _, raw := range []string{
			fmt.Sprintf("/dns4/%s/tcp/%d/p2p/%s", host, port, id),
			fmt.Sprintf("/dns4/%s/udp/%d/quic-v1/p2p/%s", host, port, id),
		} {
			if _, ok := seen[raw]; ok {
				continue
			}
			seen[raw] = struct{}{}
			out = append(out, raw)
		}
	}
	return out
}

func splitStreamAddrs(id string, raw []string) (direct, circuits []string) {
	seen := map[string]struct{}{}
	for _, address := range raw {
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
		if strings.Contains(address, "p2p-circuit") {
			circuits = append(circuits, address)
			continue
		}
		direct = append(direct, address)
	}
	return direct, circuits
}

func appendCatalogAddrs(direct, circuits []string, catalogID, peerID string) ([]string, []string) {
	_, _, dht := discovery.AddressesFor(catalogID)
	moreDirect, moreCircuits := splitStreamAddrs(peerID, dht)
	return appendUniqueAddrs(direct, moreDirect), appendUniqueAddrs(circuits, moreCircuits)
}

func noteLookupAddrs(ctx context.Context, id string) ([]string, []string) {
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs := discovery.LookupPeerAddrs(lookupCtx, id)
	if len(addrs) == 0 {
		return nil, nil
	}
	discovery.NoteDHTPeers([]discovery.Peer{{ID: id, Addresses: addrs}})
	_, _, dht := discovery.AddressesFor(id)
	return splitStreamAddrs(id, dht)
}

func appendUniqueAddrs(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := map[string]struct{}{}
	for _, address := range base {
		seen[address] = struct{}{}
	}
	for _, address := range extra {
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		base = append(base, address)
	}
	return base
}
