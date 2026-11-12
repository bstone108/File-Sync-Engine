package discovery

import (
	"strings"

	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	ma "github.com/multiformats/go-multiaddr"
)

// identityRelayGater refuses relayed connections with DHT peers that are not
// configured identity peers. A relayed stranger is what starts DCUtR against
// the public swarm. Direct DHT connections stay open so discovery still works.
type identityRelayGater struct{}

func (identityRelayGater) InterceptPeerDial(peer.ID) bool { return true }

func (identityRelayGater) InterceptAddrDial(id peer.ID, addr ma.Multiaddr) bool {
	return allowRelayPeer(id, addr)
}

func (identityRelayGater) InterceptAccept(network.ConnMultiaddrs) bool { return true }

func (identityRelayGater) InterceptSecured(dir network.Direction, id peer.ID, addrs network.ConnMultiaddrs) bool {
	if addrs == nil {
		return true
	}
	return allowRelayPeer(id, addrs.RemoteMultiaddr())
}

func (identityRelayGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func allowRelayPeer(id peer.ID, addr ma.Multiaddr) bool {
	if addr == nil || !strings.Contains(addr.String(), "p2p-circuit") {
		return true
	}
	mesh.mu.Lock()
	n := len(mesh.dialbackPeers)
	_, ok := mesh.dialbackPeers[id.String()]
	mesh.mu.Unlock()
	if n == 0 {
		return true
	}
	return ok
}
