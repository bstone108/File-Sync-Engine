package link

import (
	"net/url"
	"strings"
)

// KeepPairedAddress reports whether address is safe to store or dial for the
// paired peer. HTTP API URLs from a signed profile are kept. A libp2p address
// is kept only when its target peer id is this peer, so a DHT node's own
// address cannot become an endpoint. Port 4001 is the public DHT default and
// is not an engine API.
func KeepPairedAddress(address, dhtPeerID string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return false
	}
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		parsed, err := url.Parse(address)
		if err != nil || parsed.Hostname() == "" {
			return false
		}
		return parsed.Port() != "4001"
	}
	if !strings.HasPrefix(address, "/") {
		return false
	}
	target := multiaddrTarget(address)
	dhtPeerID = strings.TrimSpace(dhtPeerID)
	return target != "" && dhtPeerID != "" && target == dhtPeerID
}

// CircuitForPeer appends the paired peer id to a relay reservation that names
// the relay but not the target. Addresses that already name a different peer
// are left unchanged so KeepPairedAddress can reject them.
func CircuitForPeer(address, dhtPeerID string) string {
	address = strings.TrimSpace(address)
	dhtPeerID = strings.TrimSpace(dhtPeerID)
	if address == "" || dhtPeerID == "" || !strings.Contains(address, "p2p-circuit") {
		return address
	}
	if strings.HasSuffix(address, "/p2p/"+dhtPeerID) {
		return address
	}
	parts := strings.SplitN(address, "p2p-circuit", 2)
	if len(parts) == 2 && strings.Contains(parts[1], "/p2p/") {
		return address
	}
	return strings.TrimRight(address, "/") + "/p2p/" + dhtPeerID
}

func multiaddrTarget(address string) string {
	parts := strings.Split(address, "/p2p/")
	if len(parts) < 2 {
		return ""
	}
	last := parts[len(parts)-1]
	if cut := strings.Index(last, "/"); cut >= 0 {
		last = last[:cut]
	}
	return strings.TrimSpace(last)
}
