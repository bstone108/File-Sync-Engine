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
	"testing"
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
	if id != "" {
		// A reserved public relay is a dial path even when the DHT has not
		// published an address for this peer. Direct and own relays are still
		// tried first.
		circuits = appendUniqueAddrs(circuits, discovery.ReservedRelayCircuits(id))
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
	if !allowPeerDial(peer.ID, nat.DefaultPaths) {
		return "", errDialDeferred
	}
	var last error
	try := func(kind string, addrs []string, limit time.Duration) (string, bool) {
		if len(addrs) == 0 || ctx.Err() != nil {
			return "", false
		}
		dialCtx := ctx
		cancel := func() {}
		// The limit covers the dial only. Relay dials are included: a dead
		// public circuit used to wait forever and hold the folder sync loop.
		// A transfer that is already moving keeps the caller's context.
		if limit > 0 {
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
			return "", answered, true
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
	own, publicCircuits := partitionCircuits(id, circuits)
	if used, stop := try("relay", own, 20*time.Second); stop {
		if base, err, done := finish(used, stop); done {
			return base, err
		}
	}
	// Public relays are the fallback after our own relays miss. The dial
	// budget is long enough for a live circuit and short enough that a dead
	// public relay cannot hold the folder sync loop.
	if used, stop := try("relay", publicCircuits, 20*time.Second); stop {
		if base, err, done := finish(used, stop); done {
			return base, err
		}
	}
	if last == nil {
		last = errNoStream
	}
	var answered answeredError
	if errors.As(last, &answered) {
		return "", answered
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

// ErrDialDeferred means this offline peer was already tried inside the
// 60 second redial interval. The first attempt is never deferred.
var ErrDialDeferred = errors.New("peer dial is waiting for the offline interval")

var errDialDeferred = ErrDialDeferred

var (
	peerDialMu    sync.Mutex
	peerDialNext  = map[string]time.Time{}
	peerDialBurst = map[string]time.Time{}
	forceDialGate bool
)

// dialBurst keeps the journal read and the following block reads in one sync
// pass. The 60 second offline wait starts after that pass, and each request
// during a transfer extends the burst so a long file is not cut off.
const dialBurst = 30 * time.Second

func allowPeerDial(peerID string, paths *nat.Registry) bool {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return true
	}
	if testing.Testing() && !forceDialGate {
		return true
	}
	interval := discovery.OfflineProbeInterval
	if paths != nil {
		if _, ok := paths.Active(peerID); ok {
			interval = discovery.LiveProbeInterval
		}
	}
	peerDialMu.Lock()
	defer peerDialMu.Unlock()
	now := time.Now()
	if burst := peerDialBurst[peerID]; !burst.IsZero() && now.Before(burst) {
		peerDialBurst[peerID] = now.Add(dialBurst)
		return true
	}
	if next := peerDialNext[peerID]; !next.IsZero() && now.Before(next) {
		return false
	}
	peerDialNext[peerID] = now.Add(interval)
	peerDialBurst[peerID] = now.Add(dialBurst)
	return true
}

// FinishPeerPass closes the in-pass dial burst and schedules the next
// offline or live attempt. A transfer that is still reading blocks extends
// the burst until this is called.
func FinishPeerPass(peerID string, connected bool) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" || (testing.Testing() && !forceDialGate) {
		return
	}
	interval := discovery.OfflineProbeInterval
	if connected {
		interval = discovery.LiveProbeInterval
	}
	peerDialMu.Lock()
	peerDialNext[peerID] = time.Now().Add(interval)
	peerDialBurst[peerID] = time.Time{}
	peerDialMu.Unlock()
}

func resetPeerDialSchedule() {
	peerDialMu.Lock()
	peerDialNext = map[string]time.Time{}
	peerDialBurst = map[string]time.Time{}
	peerDialMu.Unlock()
}

// ResetDialStateForTest clears the process-wide dial cache and redial schedule.
func ResetDialStateForTest() {
	resetDialCache()
	resetPeerDialSchedule()
}

type streamDialError string

func (e streamDialError) Error() string { return string(e) }

type libp2pTarget struct {
	id    string
	addrs []string
}

func libp2pTargets(peer config.PeerConfig) []libp2pTarget {
	direct, circuits := streamDialPlan(peer)
	id := strings.TrimSpace(peer.DHTPeerID)
	if id != "" {
		circuits = appendUniqueAddrs(circuits, discovery.ReservedRelayCircuits(id))
	}
	addrs := append(append([]string{}, direct...), circuits...)
	if len(addrs) == 0 || id == "" {
		return nil
	}
	return []libp2pTarget{{id: id, addrs: addrs}}
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

// partitionCircuits puts circuits through our own engines ahead of public
// relays. Own circuits are added even when the saved peer only learned a
// public circuit from the DHT.
func partitionCircuits(target string, circuits []string) (own, public []string) {
	ownIDs := map[string]struct{}{}
	for _, addr := range discovery.OwnRelayAddrs() {
		if id := relayHopID(addr); id != "" {
			ownIDs[id] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	add := func(addr string, toOwn bool) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		if toOwn {
			own = append(own, addr)
			return
		}
		public = append(public, addr)
	}
	for _, addr := range discovery.OwnRelayCircuits(target) {
		add(addr, true)
	}
	for _, addr := range circuits {
		_, isOwn := ownIDs[relayHopID(addr)]
		add(addr, isOwn)
	}
	return own, public
}

func relayHopID(addr string) string {
	head := addr
	if cut := strings.Index(addr, "/p2p-circuit"); cut > 0 {
		head = addr[:cut]
	}
	parts := strings.Split(head, "/p2p/")
	if len(parts) < 2 {
		return ""
	}
	id := parts[len(parts)-1]
	if i := strings.Index(id, "/"); i >= 0 {
		id = id[:i]
	}
	return strings.TrimSpace(id)
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
