package discovery

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	corediscovery "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	routingdiscovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/structuredlog"
)

const dhtQueryLimit = 64

// Libp2pDHTRouter is the concrete public-DHT adapter for the discovery.Source
// seam. It owns a libp2p host and Kademlia DHT instance, bootstraps against
// public multiaddrs, advertises the configured application namespace, and finds
// peers advertising the same namespace. Manual/configured peers remain wired as
// separate static discovery sources by the daemon.
type Libp2pDHTRouter struct {
	host            host.Host
	dht             *dht.IpfsDHT
	discovery       *routingdiscovery.RoutingDiscovery
	mu              sync.Mutex
	advertise       map[string]struct{}
	payloads        map[string][]byte
	bootstrapped    bool
	bootstrapAt     time.Time
	bootstrapWait   chan struct{}
	bootstrapErr    error
	bootstrapPeers  []string
	resolver        multiaddrResolver
	modeReason      string
	quicCM          *quicreuse.ConnManager
	stopSTUN        func()
	remotePublic    atomic.Bool
	autonat         *autonatv2.AutoNAT
	closed          chan struct{}
	closeOnce       sync.Once
	dialbackRunning atomic.Bool
}

type Libp2pDHTRouterOptions struct {
	NodeID     string
	AutoRelay  bool
	ListenPort int
	// Announce replaces container-internal listen addresses with the
	// host-reachable published port and the HTTPS API URL.
	Announce bool
	// DHTMode overrides the default. Empty stays auto: the node reserves a
	// relay while it is private and answers queries only after a remote
	// peer dials it back. A self-dial, including NAT hairpin, is not that proof.
	DHTMode string
}

func NewLibp2pDHTRouter(ctx context.Context, opts Libp2pDHTRouterOptions) (*Libp2pDHTRouter, error) {
	var hostBox atomic.Value
	var dhtBox atomic.Value
	libp2pOpts := []libp2p.Option{}
	identity, err := configuredLibp2pIdentity()
	if err != nil {
		return nil, err
	}
	if identity != nil {
		libp2pOpts = append(libp2pOpts, identity)
	}
	if opts.ListenPort > 0 {
		libp2pOpts = append(libp2pOpts, libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", opts.ListenPort),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", opts.ListenPort),
		))
		libp2pOpts = append(libp2pOpts, libp2pListenOptions(opts.ListenPort)...)
		quicBuildMu.Lock()
		quicBuilt.Store(nil)
		defer quicBuildMu.Unlock()
	}
	mode, reason, err := libp2pDHTMode(opts.DHTMode)
	if err != nil {
		return nil, err
	}
	if opts.Announce {
		libp2pOpts = append(libp2pOpts, libp2p.AddrsFactory(announcedAddrs))
	}
	// Traversal stays on unless discovery.nat.disabled or FSE_NAT_TRAVERSAL
	// turns it off. The host stays private, and AutoRelay keeps a reservation,
	// until AutoNAT v2 or an authenticated peer dials this node from outside.
	// A dial this process makes to itself is not that confirmation.
	if NATEnabled() {
		libp2pOpts = append(libp2pOpts,
			libp2p.EnableRelay(),
			libp2p.EnableNATService(),
			libp2p.EnableAutoNATv2(),
			libp2p.EnableHolePunching(holepunch.WithTracer(holepunchTracer{})),
			libp2p.EnableRelayService(helperRelayOptions()...),
			libp2p.EnableAutoRelayWithPeerSource(func(relayCtx context.Context, num int) <-chan peer.AddrInfo {
				return relayCandidates(relayCtx, num, &hostBox, &dhtBox)
			}, autorelay.WithMinCandidates(1), autorelay.WithBootDelay(5*time.Second), autorelay.WithBackoff(30*time.Second), autorelay.WithMinInterval(2*time.Second)),
			libp2p.ForceReachabilityPrivate(),
		)
	}
	h, err := libp2p.New(libp2pOpts...)
	if err != nil {
		return nil, err
	}
	quicCM := takeQUICConnManager()
	hostBox.Store(h)
	kad, err := dht.New(ctx, h, dht.Mode(mode), dht.DisableAutoRefresh())
	if err != nil {
		_ = h.Close()
		return nil, err
	}
	dhtBox.Store(kad)
	router := &Libp2pDHTRouter{
		host:       h,
		dht:        kad,
		discovery:  routingdiscovery.NewRoutingDiscovery(kad),
		advertise:  map[string]struct{}{},
		payloads:   map[string][]byte{},
		modeReason: reason,
		quicCM:     quicCM,
		stopSTUN:   startLibp2pSTUN(h, quicCM),
		closed:     make(chan struct{}),
	}
	h.SetStreamHandler(protocol.ID(PairingProtocol), router.handlePairingStream)
	h.SetStreamHandler(protocol.ID(ObserveProtocol), router.handleObserve)
	noteRouterHost(h)
	registerRouter(router)
	watchConnections(h.Network())
	go watchReachability(h)
	go watchAutoRelay(h)
	go router.watchAutoNAT()
	go router.republishWhenRelayReserved()
	if opts.Announce {
		structuredlog.Event("info", "libp2p.announce", "libp2p is advertising the published port", map[string]any{
			"peerId":        h.ID().String(),
			"listenPort":    opts.ListenPort,
			"publishedPort": PublishedLibp2pPort(opts.ListenPort),
			"dhtMode":       dhtModeName(mode),
			"dhtModeReason": reason,
			"addresses":     multiaddrStrings(h.Addrs()),
		})
	}
	// Stay private so AutoRelay reserves a relay until a remote peer dials back.
	if NATEnabled() {
		router.notePrivateReachability()
		// Start before any peer connects so the AutoNAT client observes the
		// identify event. A client started later never learns that peer.
		router.remoteAutoNAT()
		go router.runRemoteDialback()
	}
	return router, nil
}

func (r *Libp2pDHTRouter) Bootstrap(ctx context.Context, peers []string) error {
	if r == nil || r.host == nil || r.dht == nil {
		return fmt.Errorf("libp2p DHT router is not initialized")
	}
	// A node that already has routing peers does not repeat the public walk
	// on every discovery poll. Lookup still queries when a peer is missing.
	if r.routingTableSize() > 0 {
		r.mu.Lock()
		recent := !r.bootstrapAt.IsZero() && time.Since(r.bootstrapAt) < 10*time.Minute
		r.mu.Unlock()
		if recent {
			return nil
		}
	}
	infos, err := parsePeerAddrInfosWith(ctx, peers, r.activeResolver())
	if err != nil {
		return err
	}
	infos = append(r.extraBootstrapInfos(peers), infos...)
	if r.connectBootstrap(ctx, preferDialableTransports(infos)) > 0 {
		r.waitForRoutingTable(ctx)
	}
	err = r.dht.Bootstrap(ctx)
	if err == nil && r.routingTableSize() > 0 {
		r.mu.Lock()
		r.bootstrapAt = time.Now()
		r.mu.Unlock()
	}
	return err
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

// BootstrapPeers is the list Bootstrap uses, including the public default.
func (r *Libp2pDHTRouter) BootstrapPeers() []string {
	return r.bootstrapPeerList()
}

// libp2pDHTMode chooses who answers DHT queries. An explicit FSE_LIBP2P_DHT_MODE
// wins. Otherwise the node stays in auto mode: it reserves a relay while it
// is private, and it answers queries only after a dial-back proves the libp2p
// port is open. A published port or a LAN address is not that proof.
func libp2pDHTMode(override string) (dht.ModeOpt, string, error) {
	envMode := override
	if strings.TrimSpace(override) == "" {
		envMode = os.Getenv("FSE_LIBP2P_DHT_MODE")
	}
	return selectLibp2pDHTMode(envMode, publishedLibp2pPortConfigured(), len(announceHosts()) > 0)
}

func selectLibp2pDHTMode(envMode string, publishedPort bool, reachableHost bool) (dht.ModeOpt, string, error) {
	switch strings.ToLower(strings.TrimSpace(envMode)) {
	case "auto":
		return dht.ModeAuto, "env", nil
	case "auto-server":
		return dht.ModeAutoServer, "env", nil
	case "server":
		return dht.ModeServer, "env", nil
	case "":
	default:
		return 0, "", fmt.Errorf("FSE_LIBP2P_DHT_MODE must be auto, auto-server, or server")
	}
	// A mapped port and a LAN advertise address are not dial-back proof. ModeAuto
	// answers DHT queries only after reachability is public, and it reserves a
	// relay while it is private. Forcing server mode here made a NATed node
	// advertise a closed port and skip relay reservation.
	_ = publishedPort
	_ = reachableHost
	return dht.ModeAuto, "auto", nil
}

func publishedLibp2pPortConfigured() bool {
	raw := strings.TrimSpace(os.Getenv(EnvPublishedLibp2pPort))
	if raw == "" {
		return false
	}
	port, err := strconv.Atoi(raw)
	return err == nil && port >= 1 && port <= 65535
}

func dhtModeName(mode dht.ModeOpt) string {
	switch mode {
	case dht.ModeServer:
		return "server"
	case dht.ModeAutoServer:
		return "auto-server"
	default:
		return "auto"
	}
}

// notePublicReachability tells a ModeAuto node that a remote peer dialed it,
// so it starts answering DHT queries. source is autonat-v2 or
// authenticated-peer. A node that already started as a server does not need this.
func (r *Libp2pDHTRouter) notePublicReachability(source string) {
	if r == nil || r.host == nil || r.dht == nil || source == "" || r.dht.Mode() == dht.ModeServer {
		return
	}
	if !r.remotePublic.CompareAndSwap(false, true) {
		return
	}
	mesh.autonatPublic.Store(true)
	markSelfReachabilityEmit()
	emitter, err := r.host.EventBus().Emitter(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		r.remotePublic.Store(false)
		structuredlog.Event("warn", "libp2p.dht.reachable", "libp2p could not mark its published address reachable", map[string]any{"error": err.Error(), "source": source})
		return
	}
	defer emitter.Close()
	if err := emitter.Emit(event.EvtLocalReachabilityChanged{Reachability: network.ReachabilityPublic}); err != nil {
		r.remotePublic.Store(false)
		structuredlog.Event("warn", "libp2p.dht.reachable", "libp2p could not mark its published address reachable", map[string]any{"error": err.Error(), "source": source})
		return
	}
	structuredlog.Event("info", "libp2p.dht.reachable", "a remote peer dialed this node", map[string]any{"dhtModeReason": r.modeReason, "source": source})
}

// watchAutoNAT promotes the node when AutoNAT v2 learns that another node
// could dial an advertised address. The probe is not a connection this
// process opened.
func (r *Libp2pDHTRouter) watchAutoNAT() {
	if r == nil || r.host == nil {
		return
	}
	sub, err := r.host.EventBus().Subscribe(new(event.EvtHostReachableAddrsChanged))
	if err != nil {
		return
	}
	defer sub.Close()
	for ev := range sub.Out() {
		changed, ok := ev.(event.EvtHostReachableAddrsChanged)
		if !ok {
			continue
		}
		source := remoteReachabilitySource(len(changed.Reachable), "", r.host.ID(), false, false, false)
		if source == "" {
			continue
		}
		r.notePublicReachability(source)
	}
}

// notePrivateReachability tells AutoRelay this node cannot be dialed directly,
// so it reserves a relay instead of advertising a closed port.
func (r *Libp2pDHTRouter) notePrivateReachability() {
	if r == nil || r.host == nil {
		return
	}
	emitter, err := r.host.EventBus().Emitter(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		structuredlog.Event("warn", "libp2p.dht.private", "libp2p could not mark itself private", map[string]any{"error": err.Error()})
		return
	}
	defer emitter.Close()
	if err := emitter.Emit(event.EvtLocalReachabilityChanged{Reachability: network.ReachabilityPrivate}); err != nil {
		structuredlog.Event("warn", "libp2p.dht.private", "libp2p could not mark itself private", map[string]any{"error": err.Error()})
		return
	}
	structuredlog.Event("info", "libp2p.dht.private", "libp2p is behind NAT and will reserve a relay", map[string]any{"dhtModeReason": r.modeReason})
}

// republishWhenRelayReserved refreshes provider records after a circuit
// address exists. A record published at startup otherwise names only the
// private listen address, and a peer behind another NAT cannot dial it.
func (r *Libp2pDHTRouter) republishWhenRelayReserved() {
	if r == nil || r.host == nil {
		return
	}
	sub, err := r.host.EventBus().Subscribe(new(event.EvtAutoRelayAddrsUpdated))
	if err != nil {
		return
	}
	defer sub.Close()
	for ev := range sub.Out() {
		updated, ok := ev.(event.EvtAutoRelayAddrsUpdated)
		if !ok || len(updated.RelayAddrs) == 0 || r.discovery == nil {
			continue
		}
		r.mu.Lock()
		namespaces := make([]string, 0, len(r.advertise))
		for namespace := range r.advertise {
			namespaces = append(namespaces, namespace)
		}
		r.mu.Unlock()
		if len(namespaces) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		for _, namespace := range namespaces {
			if _, advErr := r.discovery.Advertise(ctx, namespace); advErr != nil {
				structuredlog.Event("warn", "libp2p.relay.republish", "relay reservation did not refresh the DHT record", map[string]any{
					"namespace": namespace,
					"error":     advErr.Error(),
				})
			}
		}
		cancel()
		structuredlog.Event("info", "libp2p.relay.republish", "DHT records were refreshed on the reserved relay", map[string]any{"namespaces": len(namespaces)})
	}
}

// Addrs are the addresses the host is advertising, including relay circuits
// after a reservation.
func (r *Libp2pDHTRouter) Addrs() []string {
	if r == nil || r.host == nil {
		return nil
	}
	return multiaddrStrings(r.host.Addrs())
}

// LookupPeerAddrs asks the DHT for a paired peer when no relay address was stored.
func (r *Libp2pDHTRouter) LookupPeerAddrs(ctx context.Context, peerID string) []string {
	if r == nil || r.host == nil || strings.TrimSpace(peerID) == "" {
		return nil
	}
	pid, err := peer.Decode(strings.TrimSpace(peerID))
	if err != nil {
		return nil
	}
	addrs := r.host.Peerstore().Addrs(pid)
	if len(addrs) == 0 && r.dht != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		findCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		info, findErr := r.dht.FindPeer(findCtx, pid)
		cancel()
		if findErr == nil {
			addrs = info.Addrs
		}
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr == nil {
			continue
		}
		out = append(out, addr.Encapsulate(ma.StringCast("/p2p/"+pid.String())).String())
	}
	return out
}

func relayCandidates(ctx context.Context, num int, hostBox, dhtBox *atomic.Value) <-chan peer.AddrInfo {
	ch := make(chan peer.AddrInfo)
	go func() {
		defer close(ch)
		if num <= 0 {
			return
		}
		for _, info := range preferredRelayInfos() {
			select {
			case <-ctx.Done():
				return
			case ch <- info:
			}
			num--
			if num == 0 {
				return
			}
		}
		if hostBox == nil || dhtBox == nil {
			return
		}
		h, _ := hostBox.Load().(host.Host)
		if h == nil {
			return
		}
		seen := map[peer.ID]struct{}{}
		send := func(id peer.ID) bool {
			if id == "" || id == h.ID() {
				return true
			}
			if _, ok := seen[id]; ok {
				return true
			}
			seen[id] = struct{}{}
			info := peer.AddrInfo{ID: id, Addrs: h.Peerstore().Addrs(id)}
			select {
			case <-ctx.Done():
				return false
			case ch <- info:
			}
			return true
		}
		// Connected peers include the bootstrap dial, which may not be in the
		// routing table yet. Those peers are the only relay candidates when
		// no relay was configured.
		for _, id := range h.Network().Peers() {
			num--
			if num < 0 || !send(id) {
				return
			}
		}
		kad, _ := dhtBox.Load().(*dht.IpfsDHT)
		if kad == nil || kad.RoutingTable() == nil {
			return
		}
		for _, id := range kad.RoutingTable().ListPeers() {
			num--
			if num < 0 || !send(id) {
				return
			}
		}
	}()
	return ch
}

func lastAddrOf(info peer.AddrInfo) string {
	if len(info.Addrs) == 0 || info.Addrs[0] == nil {
		return ""
	}
	return info.Addrs[0].String()
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
	var lastErr error
	var lastAddr string
	for _, info := range attempts[:limit] {
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		r.host.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
		connectCtx, cancel := context.WithTimeout(ctx, bootstrapDialTimeout)
		err := r.host.Connect(connectCtx, info)
		cancel()
		if err == nil {
			structuredlog.Event("info", "libp2p.bootstrap.connected", "libp2p bootstrap dial connected", map[string]any{
				"address": lastAddrOf(info),
				"peer":    info.ID.String(),
			})
			return 1
		}
		lastErr = err
		if len(info.Addrs) > 0 {
			lastAddr = info.Addrs[0].String()
		}
	}
	if lastErr != nil {
		structuredlog.Event("warn", "libp2p.bootstrap.dial", "libp2p bootstrap dial failed", map[string]any{
			"error":      lastErr.Error(),
			"address":    lastAddr,
			"candidates": limit,
		})
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
	_, already := r.advertise[namespace]
	if !already {
		r.advertise[namespace] = struct{}{}
	}
	r.mu.Unlock()
	if !already {
		if _, err := r.discovery.Advertise(ctx, namespace); err != nil {
			r.mu.Lock()
			delete(r.advertise, namespace)
			r.mu.Unlock()
			return nil, err
		}
	}
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

// ProbeSTUNNow sends one STUN round from this router's QUIC socket.
// The background probe does not run under go test, so tests call this.
func (r *Libp2pDHTRouter) ProbeSTUNNow(ctx context.Context) (advertise.NATStatus, error) {
	if r == nil || r.host == nil || r.quicCM == nil {
		return advertise.NATStatus{}, fmt.Errorf("libp2p UDP socket is not available")
	}
	conn, _, err := shareQUICSocket(r.host, r.quicCM)
	if err != nil {
		return advertise.NATStatus{}, err
	}
	defer conn.Close()
	return advertise.ObserveSTUN(ctx, conn), nil
}

func (r *Libp2pDHTRouter) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.closed != nil {
			close(r.closed)
		}
	})
	unregisterRouter(r)
	if r.stopSTUN != nil {
		r.stopSTUN()
		r.stopSTUN = nil
	}
	r.mu.Lock()
	an := r.autonat
	r.autonat = nil
	r.mu.Unlock()
	if an != nil {
		an.Close()
	}
	if r.host == nil {
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
