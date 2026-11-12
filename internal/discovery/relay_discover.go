package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	circuitproto "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/proto"

	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/structuredlog"
)

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
			structuredlog.Event("info", "libp2p.relay.discover", "identified a hop relay", map[string]any{"peer": identified.Peer.String()})
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
