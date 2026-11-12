package discovery

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"

	"filesyncengine/internal/config"
	"filesyncengine/internal/nat"
	"filesyncengine/internal/structuredlog"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	// ObserveProtocol reports the address a public same-identity helper saw.
	ObserveProtocol = "/fse/observe/1.0.0"
	// EnvNATTraversal disables AutoNAT, hole punching, relay, and STUN when
	// set to 0, false, or off.
	EnvNATTraversal = "FSE_NAT_TRAVERSAL"
	// EnvMeshHelper disables the same-identity helper when set to 0, false, or off.
	EnvMeshHelper = "FSE_MESH_HELPER"
	// EnvRelayPeers overrides the public relay candidate list.
	EnvRelayPeers = "FSE_RELAY_PEERS"
)

// meshState is the process-wide helper policy. It is on unless config or the
// environment turns it off.
type meshState struct {
	mu                sync.Mutex
	helperEnabled     bool
	natEnabled        bool
	maxReservations   int
	publicRelays      []string
	reservedRelays    []string
	discoveredRelays  []string
	bootstrapPeers    map[string]struct{}
	identityRelays    []string
	selfID            string
	punchNodes        []MeshNode
	configMembers     map[string]struct{}
	connectionMembers map[string]struct{}
	dialbackPeers     map[string]struct{}
	portExposed       atomic.Bool
	autonatPublic     atomic.Bool
	selfEmit          atomic.Bool
}

var mesh = &meshState{helperEnabled: true, natEnabled: true, maxReservations: 32}

func init() {
	mesh.configMembers = map[string]struct{}{}
	mesh.connectionMembers = map[string]struct{}{}
	mesh.dialbackPeers = map[string]struct{}{}
	applyNATEnv()
}

func applyNATEnv() {
	mesh.natEnabled = !disabledEnv(EnvNATTraversal)
	mesh.helperEnabled = mesh.natEnabled && !disabledEnv(EnvMeshHelper)
}

func disabledEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "false", "off", "no", "disabled":
		return true
	default:
		return false
	}
}

// ConfigureNAT applies discovery.nat. The environment overrides still win.
func ConfigureNAT(cfg config.NATConfig) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	applyNATEnv()
	if cfg.Disabled {
		mesh.natEnabled = false
		mesh.helperEnabled = false
	}
	if cfg.MeshHelperDisabled {
		mesh.helperEnabled = false
	}
	if cfg.MaxRelayReservations > 0 {
		mesh.maxReservations = cfg.MaxRelayReservations
	}
	if len(cfg.RelayPeers) > 0 {
		mesh.publicRelays = append([]string(nil), cfg.RelayPeers...)
	}
	if raw := strings.TrimSpace(os.Getenv(EnvRelayPeers)); raw != "" {
		mesh.publicRelays = splitCSV(raw)
	}
}

// NATEnabled reports whether the traversal stack should be configured.
func NATEnabled() bool {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	return mesh.natEnabled
}

// NotePortExposed records a UPnP or NAT-PMP mapping. A mapped port lets this
// instance help same-identity peers even before AutoNAT agrees.
func NotePortExposed() {
	mesh.portExposed.Store(true)
}

// SetIdentityMembers replaces the libp2p peer IDs allowed to use this helper
// because they belong to an enabled identity group in local config.
func SetIdentityMembers(ids []string) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	mesh.configMembers = idSet(ids)
}

// SetConnectionMembers replaces helper peer IDs learned from replicated
// same-identity connection details.
func SetConnectionMembers(ids []string) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	mesh.connectionMembers = idSet(ids)
}

// SetIdentityRelays stores circuit addresses of same-identity helpers.
// They are tried before public relays.
func SetIdentityRelays(addrs []string) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	mesh.identityRelays = append([]string(nil), addrs...)
}

// HelperRelayAddrs returns same-identity helper relays and this node's circuit
// addresses. The public bootstrap list is not copied into reconnect profiles.
func HelperRelayAddrs() []string {
	mesh.mu.Lock()
	relays := append([]string(nil), mesh.identityRelays...)
	mesh.mu.Unlock()
	seen := map[string]struct{}{}
	out := make([]string, 0, len(relays))
	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	for _, addr := range relays {
		add(addr)
	}
	for _, addr := range SharedListenAddrs() {
		if strings.Contains(addr, "p2p-circuit") {
			add(addr)
		}
	}
	return out
}

// OrderedRelays returns same-identity relays when any exist. Otherwise it
// returns configured public relays and relays discovered in the DHT. The
// public bootstrap list is not a relay: those nodes do not speak the hop
// protocol, and dialing them cannot reach an identity peer.
func OrderedRelays() []string {
	mesh.mu.Lock()
	public := append([]string(nil), mesh.publicRelays...)
	public = append(public, mesh.discoveredRelays...)
	own := ownRelayAddrsLocked()
	boot := map[string]struct{}{}
	for id := range mesh.bootstrapPeers {
		boot[id] = struct{}{}
	}
	mesh.mu.Unlock()
	var filtered []string
	for _, addr := range public {
		if _, ok := boot[relayPeerID(addr)]; ok {
			continue
		}
		filtered = append(filtered, addr)
	}
	return DataRelays(own, filtered)
}

// NoteDiscoveredRelay remembers a DHT relay in memory. The set is bounded
// and is never written to config.
func NoteDiscoveredRelay(addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" || !strings.Contains(addr, "/p2p/") || strings.Contains(addr, "p2p-circuit") {
		return
	}
	if bootstrapRelay(addr) {
		return
	}
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	next := []string{addr}
	for _, existing := range mesh.discoveredRelays {
		if existing == addr {
			continue
		}
		next = append(next, existing)
		if len(next) >= maxReservedRelays {
			break
		}
	}
	if len(next) > maxReservedRelays {
		next = next[:maxReservedRelays]
	}
	mesh.discoveredRelays = next
}

// NoteBootstrapPeer records a DHT bootstrap peer so it is not used as a relay.
func NoteBootstrapPeer(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	if mesh.bootstrapPeers == nil {
		mesh.bootstrapPeers = map[string]struct{}{}
	}
	mesh.bootstrapPeers[id] = struct{}{}
}

func bootstrapRelay(addr string) bool {
	return isBootstrapID(relayPeerID(addr))
}

func isBootstrapID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	_, ok := mesh.bootstrapPeers[id]
	return ok
}

func relayPeerID(addr string) string {
	addr = strings.TrimSpace(addr)
	cut := strings.Index(addr, "/p2p-circuit")
	head := addr
	if cut > 0 {
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

// UnlimitedRelayAddrs returns same-identity relays first, then explicitly
// configured public relays. The default bootstrap list is omitted: public
// kubo relays are limited circuits and cannot carry file bytes.
func UnlimitedRelayAddrs() []string {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	return nat.PreferRelays(ownRelayAddrsLocked(), mesh.publicRelays)
}

// RefreshIdentityMembers allows reservations from configured peers in an
// enabled identity group.
func RefreshIdentityMembers(cfg config.Config) {
	enabled := map[string]struct{}{}
	for _, group := range cfg.Identity.Groups {
		if group.Enabled && group.ID != "" {
			enabled[group.ID] = struct{}{}
		}
	}
	var ids []string
	var dialback []string
	for _, peer := range cfg.Peers {
		if peer.DHTPeerID == "" {
			continue
		}
		// A configured peer was stored after its session authenticated.
		dialback = append(dialback, peer.DHTPeerID)
		if peer.IdentityGroupID == "" {
			continue
		}
		if _, ok := enabled[peer.IdentityGroupID]; !ok {
			continue
		}
		ids = append(ids, peer.DHTPeerID)
	}
	setDialbackPeers(dialback)
	SetIdentityMembers(ids)
}

func setDialbackPeers(ids []string) {
	mesh.mu.Lock()
	mesh.dialbackPeers = idSet(ids)
	mesh.mu.Unlock()
}

func authenticatedDialbackPeer(id peer.ID) bool {
	if id == "" {
		return false
	}
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	_, ok := mesh.dialbackPeers[id.String()]
	return ok
}

// remoteReachabilitySource reports why a node may become public.
// A dial this process opens, including one that hairpins to its own public
// address, is not a source. AutoNAT v2 means another node dialed an
// advertised address. An authenticated peer's inbound direct connection is
// the other confirmation. Relay circuits are not that dial.
func remoteReachabilitySource(autonatReachable int, remote, local peer.ID, inbound, circuit, authenticated bool) string {
	if autonatReachable > 0 {
		return "autonat-v2"
	}
	if !inbound || circuit || remote == "" || remote == local || !authenticated {
		return ""
	}
	return "authenticated-peer"
}

func idSet(ids []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (m *meshState) allows(id peer.ID) bool {
	if id == "" || !m.helperOn() || !m.public() {
		return false
	}
	key := id.String()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.configMembers[key]; ok {
		return true
	}
	_, ok := m.connectionMembers[key]
	return ok
}

func (m *meshState) helperOn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.helperEnabled
}

func (m *meshState) public() bool {
	return m.portExposed.Load() || m.autonatPublic.Load()
}

type sameIdentityACL struct{}

func (sameIdentityACL) AllowReserve(p peer.ID, _ ma.Multiaddr) bool {
	return labPublicRelay() || mesh.allows(p)
}

func (sameIdentityACL) AllowConnect(src peer.ID, _ ma.Multiaddr, dest peer.ID) bool {
	return labPublicRelay() || (mesh.allows(src) && mesh.allows(dest))
}

func labPublicRelay() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("FSE_LAB_PUBLIC_RELAY")), "1") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("FSE_LAB_PUBLIC_RELAY")), "true")
}

func helperResources() relayv2.Resources {
	resources := relayv2.DefaultResources()
	mesh.mu.Lock()
	resources.MaxReservations = mesh.maxReservations
	mesh.mu.Unlock()
	if resources.MaxReservations <= 0 {
		resources.MaxReservations = 32
	}
	resources.MaxCircuits = 4
	resources.MaxReservationsPerIP = 4
	resources.MaxReservationsPerPeer = 1
	// A nil limit is an unlimited circuit. Public kubo relays stay limited
	// (about 128 KiB and 2 minutes). This helper is the bulk-transfer fallback
	// when hole punching cannot open a direct path.
	resources.Limit = nil
	return resources
}

func helperRelayOptions() []relayv2.Option {
	return []relayv2.Option{relayv2.WithResources(helperResources()), relayv2.WithACL(sameIdentityACL{})}
}

type holepunchTracer struct{}

func (holepunchTracer) Trace(evt *holepunch.Event) {
	if evt == nil || evt.Remote == "" {
		return
	}
	switch evt.Type {
	case holepunch.StartHolePunchEvtT:
		fields := map[string]any{"peer": evt.Remote.String()}
		structuredlog.Event("info", "libp2p.holepunch.start", "libp2p hole punch started", fields)
		structuredlog.Event("info", "libp2p.dcutr.start", "DCUtR hole punch started", fields)
	case holepunch.EndHolePunchEvtT:
		end, ok := evt.Evt.(holepunch.EndHolePunchEvt)
		if !ok {
			return
		}
		if end.Success {
			nat.DefaultPaths.Observe(evt.Remote.String(), "", nat.PathHolePunched)
			fields := map[string]any{"peer": evt.Remote.String()}
			structuredlog.Event("info", "libp2p.holepunch.ok", "libp2p hole punch succeeded", fields)
			structuredlog.Event("info", "libp2p.dcutr.ok", "DCUtR hole punch succeeded", fields)
			return
		}
		fields := map[string]any{"peer": evt.Remote.String(), "error": end.Error}
		structuredlog.Event("warn", "libp2p.holepunch.failed", "libp2p hole punch failed", fields)
		structuredlog.Event("warn", "libp2p.dcutr.failed", "DCUtR hole punch failed", fields)
	}
}

func watchAutoRelay(h host.Host) {
	if h == nil {
		return
	}
	sub, err := h.EventBus().Subscribe(new(event.EvtAutoRelayAddrsUpdated))
	if err != nil {
		return
	}
	defer sub.Close()
	for ev := range sub.Out() {
		updated, ok := ev.(event.EvtAutoRelayAddrsUpdated)
		if !ok {
			continue
		}
		addrs := make([]string, 0, len(updated.RelayAddrs))
		for _, addr := range updated.RelayAddrs {
			if addr != nil {
				addrs = append(addrs, addr.String())
			}
		}
		NoteReservedRelays(addrs)
		structuredlog.Event("info", "libp2p.relay.reserved", "libp2p reserved a relay", map[string]any{"addresses": addrs})
	}
}

func markSelfReachabilityEmit() {
	mesh.selfEmit.Store(true)
}

func noteReachability(public bool) {
	if public {
		if mesh.selfEmit.CompareAndSwap(true, false) {
			return
		}
		mesh.autonatPublic.Store(true)
		return
	}
	mesh.selfEmit.Store(false)
	mesh.autonatPublic.Store(false)
}

var (
	routerRegMu  sync.Mutex
	routerByPeer = map[peer.ID]*Libp2pDHTRouter{}
)

func registerRouter(r *Libp2pDHTRouter) {
	if r == nil || r.host == nil {
		return
	}
	routerRegMu.Lock()
	routerByPeer[r.host.ID()] = r
	routerRegMu.Unlock()
}

func unregisterRouter(r *Libp2pDHTRouter) {
	if r == nil || r.host == nil {
		return
	}
	routerRegMu.Lock()
	delete(routerByPeer, r.host.ID())
	routerRegMu.Unlock()
}

func routerFor(id peer.ID) *Libp2pDHTRouter {
	routerRegMu.Lock()
	defer routerRegMu.Unlock()
	return routerByPeer[id]
}

func watchConnections(n network.Network) {
	n.Notify(&network.NotifyBundle{
		ConnectedF: func(_ network.Network, conn network.Conn) {
			if conn == nil {
				return
			}
			addr := ""
			if conn.RemoteMultiaddr() != nil {
				addr = conn.RemoteMultiaddr().String()
			}
			stat := conn.Stat()
			source := remoteReachabilitySource(0, conn.RemotePeer(), conn.LocalPeer(), stat.Direction == network.DirInbound, strings.Contains(addr, "p2p-circuit"), authenticatedDialbackPeer(conn.RemotePeer()))
			if router := routerFor(conn.LocalPeer()); router != nil {
				if source != "" {
					router.notePublicReachability(source)
				} else if conn.RemotePeer() != conn.LocalPeer() && authenticatedDialbackPeer(conn.RemotePeer()) {
					// A public bootstrap connection is not a reason to probe.
					// Own authenticated peers coordinate that dial-back.
					router.scheduleRemoteDialback()
				}
			}
			path := nat.Classify(addr)
			if stat.Limited {
				path = nat.PathLimitedRelay
			}
			if current, ok := nat.DefaultPaths.Active(conn.RemotePeer().String()); ok && current.Path == nat.PathHolePunched && path != nat.PathRelay {
				return
			}
			nat.DefaultPaths.Observe(conn.RemotePeer().String(), addr, path)
		},
		DisconnectedF: func(_ network.Network, conn network.Conn) {
			if conn == nil {
				return
			}
			nat.DefaultPaths.Forget(conn.RemotePeer().String())
		},
	})
}

// ParseObservation reads a helper's observed address and relay address.
func ParseObservation(body string) (observed, relayAddr string) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "observed":
			observed = fields[1]
		case "relay":
			relayAddr = fields[1]
		}
	}
	return observed, relayAddr
}

func writeObservation(w io.Writer, observed, relayAddr string) error {
	if _, err := fmt.Fprintf(w, "observed %s\n", observed); err != nil {
		return err
	}
	if relayAddr == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "relay %s\n", relayAddr)
	return err
}

// HelperAnnouncement is what a publicly reachable instance publishes to the
// same identity group.
type HelperAnnouncement struct {
	DHTPeerID  string
	RelayAddrs []string
	Reachable  string
	Enabled    bool
}

// CurrentHelperAnnouncement reports the local helper, when this process is
// publicly reachable and the helper is enabled.
func CurrentHelperAnnouncement(peerID string, listenAddrs []string) HelperAnnouncement {
	enabled := mesh.helperOn() && mesh.public()
	reachable := "private"
	if mesh.public() {
		reachable = "public"
	}
	var relays []string
	if enabled {
		for _, addr := range listenAddrs {
			if addr == "" || strings.Contains(addr, "p2p-circuit") {
				continue
			}
			relays = append(relays, addr)
		}
	}
	return HelperAnnouncement{DHTPeerID: peerID, RelayAddrs: relays, Reachable: reachable, Enabled: enabled}
}

func watchReachability(h host.Host) {
	if h == nil {
		return
	}
	sub, err := h.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		return
	}
	defer sub.Close()
	for ev := range sub.Out() {
		changed, ok := ev.(event.EvtLocalReachabilityChanged)
		if !ok {
			continue
		}
		noteReachability(changed.Reachability == network.ReachabilityPublic)
	}
}

func staticRelayAddrs() []string {
	raw := strings.TrimSpace(os.Getenv("FSE_STATIC_RELAYS"))
	if raw == "" {
		return nil
	}
	return strings.Fields(strings.ReplaceAll(raw, ",", " "))
}

func preferredRelayInfos() []peer.AddrInfo {
	var out []peer.AddrInfo
	seen := map[string]struct{}{}
	// A configured relay is reserved first so both peers punch through the
	// same hop. Random DHT peers are not a shared reservation.
	candidates := append(staticRelayAddrs(), OrderedRelays()...)
	for _, raw := range candidates {
		addr, err := ma.NewMultiaddr(raw)
		if err != nil {
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil || info == nil || info.ID == "" {
			continue
		}
		if _, ok := seen[info.ID.String()]; ok {
			continue
		}
		seen[info.ID.String()] = struct{}{}
		out = append(out, *info)
	}
	return out
}

func (r *Libp2pDHTRouter) handleObserve(stream network.Stream) {
	if stream == nil {
		return
	}
	defer stream.Close()
	if stream.Conn() == nil || !mesh.allows(stream.Conn().RemotePeer()) {
		_ = stream.Reset()
		return
	}
	observed := ""
	if remote := stream.Conn().RemoteMultiaddr(); remote != nil {
		observed = remote.String()
	}
	relayAddr := ""
	if r != nil && r.host != nil && len(r.host.Addrs()) > 0 {
		relayAddr = r.host.Addrs()[0].Encapsulate(ma.StringCast("/p2p/" + r.host.ID().String())).String()
	}
	_ = writeObservation(stream, observed, relayAddr)
}

func reservationLimit() int {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	if mesh.maxReservations <= 0 {
		return 32
	}
	return mesh.maxReservations
}

// ReservationLimit is the same-identity relay slot cap.
func ReservationLimit() int { return reservationLimit() }
