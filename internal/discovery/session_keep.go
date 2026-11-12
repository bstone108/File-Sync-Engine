package discovery

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/nat"
	"filesyncengine/internal/structuredlog"
)

// quicKeepalive is how often an open identity session is pinged. QUIC's own
// keepalive is also 15s. Together they refresh a NAT or VPN mapping that
// would otherwise drop a quiet UDP flow in about a minute.
const quicKeepalive = 5 * time.Second

// StreamConnected reports a libp2p connection that is still open.
func StreamConnected(peerID string) bool {
	h := currentAPIHost()
	if h == nil || peerID == "" {
		return false
	}
	id, err := peer.Decode(peerID)
	if err != nil {
		return false
	}
	return h.Network().Connectedness(id) == network.Connected
}

type punchFilter struct{}

var punchSkipLog atomic.Int64

func (punchFilter) FilterLocal(id peer.ID, addrs []ma.Multiaddr) []ma.Multiaddr {
	if nat.SkipHolePunch(nat.LocalClass(), nat.PeerClass(id.String())) {
		now := time.Now().Unix()
		prev := punchSkipLog.Load()
		if now-prev >= 60 && punchSkipLog.CompareAndSwap(prev, now) {
			structuredlog.Event("info", "libp2p.dcutr.skip", "DCUtR skipped; a side cannot accept a punch", map[string]any{
				"peer": id.String(), "local": string(nat.LocalClass()), "remote": string(nat.PeerClass(id.String())),
			})
		}
		return nil
	}
	return addrs
}

func (punchFilter) FilterRemote(id peer.ID, addrs []ma.Multiaddr) []ma.Multiaddr {
	if nat.SkipHolePunch(nat.LocalClass(), nat.PeerClass(id.String())) {
		return nil
	}
	return addrs
}

var _ holepunch.AddrFilter = punchFilter{}

func (r *Libp2pDHTRouter) keepIdentitySessions() {
	if r == nil || r.host == nil || r.closed == nil {
		return
	}
	pinger := ping.NewPingService(r.host)
	ticker := time.NewTicker(quicKeepalive)
	defer ticker.Stop()
	for {
		select {
		case <-r.closed:
			return
		case <-ticker.C:
			r.pingIdentity(pinger)
		}
	}
}

func (r *Libp2pDHTRouter) pingIdentity(pinger *ping.PingService) {
	if r == nil || r.host == nil || pinger == nil {
		return
	}
	for _, id := range r.host.Network().Peers() {
		if !authenticatedDialbackPeer(id) {
			continue
		}
		if cm := r.host.ConnManager(); cm != nil {
			cm.Protect(id, "fse-identity")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		failed := false
		select {
		case result := <-pinger.Ping(ctx, id):
			if result.Error != nil {
				failed = true
			} else {
				nat.DefaultPaths.Touch(id.String())
			}
		case <-ctx.Done():
			failed = true
		}
		cancel()
		if failed {
			for _, conn := range r.host.Network().ConnsToPeer(id) {
				if conn != nil {
					_ = conn.Close()
				}
			}
			nat.NotifyDisconnected(id.String())
		}
	}
}

var classWatchOnce sync.Once

func watchInboundClass() {
	classWatchOnce.Do(func() {
		go func() {
			publishInboundClass()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				publishInboundClass()
			}
		}()
	})
}

func publishInboundClass() {
	forced := os.Getenv("FSE_INBOUND_CLASS")
	overall, tcp, udp := nat.ClassifyInbound(nat.Evidence{
		AutoNATPublic: mesh.autonatPublic.Load(),
		STUN:          advertise.CurrentNATStatus().Type,
		VPN:           nat.DefaultRouteIsTunnel(),
		Forced:        forced,
	})
	if nat.SetLocal(overall, map[string]nat.Class{"tcp": tcp, "udp": udp}) {
		structuredlog.Event("info", "nat.inbound", "inbound reachability classified", map[string]any{
			"class": string(overall),
			"tcp":   string(tcp),
			"udp":   string(udp),
		})
	}
}
