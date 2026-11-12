package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	circuitproto "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/proto"

	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/structuredlog"
)

var (
	rendezvousMu    sync.Mutex
	rendezvousGroup string
	discoverLogMu   sync.Mutex
	discoverLogAt   = map[string]time.Time{}
)

// SetRendezvousGroup selects the DHT namespace both members of an identity
// group advertise under. The namespace is derived from the group id so the
// peers find each other's current relay circuits.
func SetRendezvousGroup(groupID string) {
	rendezvousMu.Lock()
	rendezvousGroup = strings.TrimSpace(groupID)
	rendezvousMu.Unlock()
}

func currentRendezvous() string {
	rendezvousMu.Lock()
	groupID := rendezvousGroup
	rendezvousMu.Unlock()
	if groupID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("fse-relay-rendezvous/v1\n" + groupID))
	return "fse-relay/" + hex.EncodeToString(sum[:8])
}

func logDiscoveredRelay(id string) {
	if id == "" {
		return
	}
	discoverLogMu.Lock()
	defer discoverLogMu.Unlock()
	if last, ok := discoverLogAt[id]; ok && time.Since(last) < 10*time.Minute {
		return
	}
	discoverLogAt[id] = time.Now()
	structuredlog.Event("info", "libp2p.relay.discover", "identified a hop relay", map[string]any{"peer": id})
}

// PreferRoutableCircuits drops a relay's private addresses when that relay
// also advertises a public hop. A public relay's 10.x address is not reachable
// from another home WAN. A lab relay that has only private addresses is kept.
func PreferRoutableCircuits(addrs []string) []string {
	type bucket struct {
		public  []string
		private []string
	}
	order := []string{}
	groups := map[string]*bucket{}
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		id := relayPeerID(addr)
		if id == "" {
			id = addr
		}
		group := groups[id]
		if group == nil {
			group = &bucket{}
			groups[id] = group
			order = append(order, id)
		}
		if circuitHopPrivate(addr) {
			group.private = append(group.private, addr)
			continue
		}
		group.public = append(group.public, addr)
	}
	var out []string
	for _, id := range order {
		group := groups[id]
		if len(group.public) > 0 {
			out = append(out, group.public...)
			continue
		}
		out = append(out, group.private...)
	}
	return out
}

func circuitHopPrivate(addr string) bool {
	if !strings.Contains(addr, "/ip4/") {
		return false
	}
	rest := addr[strings.Index(addr, "/ip4/")+5:]
	if cut := strings.Index(rest, "/"); cut >= 0 {
		rest = rest[:cut]
	}
	ip := net.ParseIP(rest)
	return ip != nil && ip.IsPrivate()
}

// PublicRelayNamespace is the DHT provider key for relays that speak the
// circuit hop protocol. Peers reserve these relays. Bootstrap nodes are not
// advertised here.
const PublicRelayNamespace = "fse-public-relay"

func (r *Libp2pDHTRouter) watchIdentifiedRelays() {
	if r == nil || r.host == nil {
		return
	}
	sub, err := r.host.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		return
	}
	defer sub.Close()
	for {
		select {
		case <-r.closed:
			return
		case ev, ok := <-sub.Out():
			if !ok {
				return
			}
			identified, ok := ev.(event.EvtPeerIdentificationCompleted)
			if !ok || identified.Peer == "" || isBootstrapID(identified.Peer.String()) {
				continue
			}
			hop := false
			for _, id := range identified.Protocols {
				if id == protocol.ID(circuitproto.ProtoIDv2Hop) {
					hop = true
					break
				}
			}
			if !hop {
				continue
			}
			noteRelayCandidate(peer.AddrInfo{ID: identified.Peer, Addrs: identified.ListenAddrs})
			logDiscoveredRelay(identified.Peer.String())
		}
	}
}

func (r *Libp2pDHTRouter) watchPublicRelays() {
	if r == nil || r.host == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if labPublicRelay() {
			r.advertisePublicRelay(ctx)
		} else {
			r.findPublicRelays(ctx)
		}
		if rv := currentRendezvous(); rv != "" && r.discovery != nil {
			_, _ = r.discovery.Advertise(ctx, rv)
		}
		cancel()
		select {
		case <-r.closed:
			return
		case <-ticker.C:
		}
	}
}

func (r *Libp2pDHTRouter) advertisePublicRelay(ctx context.Context) {
	if r == nil || r.discovery == nil {
		return
	}
	if _, err := r.discovery.Advertise(ctx, PublicRelayNamespace); err != nil {
		structuredlog.Event("warn", "libp2p.relay.advertise", "public relay was not advertised", map[string]any{"error": err.Error()})
		return
	}
	structuredlog.Event("info", "libp2p.relay.advertise", "public relay is discoverable", map[string]any{"peerId": r.host.ID().String()})
}

func (r *Libp2pDHTRouter) findPublicRelays(ctx context.Context) {
	if r == nil || r.discovery == nil || r.host == nil {
		return
	}
	peers, err := r.discovery.FindPeers(ctx, PublicRelayNamespace)
	if err != nil {
		structuredlog.Event("warn", "libp2p.relay.discover", "public relay lookup failed", map[string]any{"error": err.Error()})
		return
	}
	for found := range peers {
		if found.ID == "" || found.ID == r.host.ID() || isBootstrapID(found.ID.String()) {
			continue
		}
		for _, addr := range relayDialAddrs(found) {
			NoteDiscoveredRelay(addr)
		}
		if len(found.Addrs) > 0 {
			structuredlog.Event("info", "libp2p.relay.discover", "discovered a public relay", map[string]any{
				"peer":      found.ID.String(),
				"addresses": relayDialAddrs(found),
			})
		}
	}
}

func (r *Libp2pDHTRouter) usingPublicBootstrap() bool {
	if r == nil {
		return false
	}
	for _, addr := range r.bootstrapPeerList() {
		if strings.Contains(addr, "bootstrap.libp2p.io") || strings.Contains(addr, "bootstrap.ipni.io") {
			return true
		}
	}
	return false
}

func noteRelayCandidate(info peer.AddrInfo) {
	for _, addr := range relayDialAddrs(info) {
		NoteDiscoveredRelay(addr)
	}
}

func relayDialAddrs(info peer.AddrInfo) []string {
	var out []string
	for _, addr := range info.Addrs {
		if addr == nil {
			continue
		}
		full := addr
		if !strings.Contains(addr.String(), "/p2p/") {
			full = addr.Encapsulate(ma.StringCast("/p2p/" + info.ID.String()))
		}
		out = append(out, full.String())
	}
	return out
}
