package discovery

import "sort"

// PeerExchangePlan describes the safe address-sharing work after two trusted
// peers connect. ShareWithRemote is the local trusted graph to send to the
// newly connected peer. RelayToKnown tells each already-known peer about the
// newcomer. Learned contains remote graph peers not already known locally.
type PeerExchangePlan struct {
	ShareWithRemote []Peer
	RelayToKnown    map[string][]Peer
	Learned         []Peer
}

// PlanPeerExchange builds a deterministic peer-exchange plan from trusted peer
// graph data. It performs only graph/address planning: callers are responsible
// for authenticating peers and enforcing privacy policy before calling it.
func PlanPeerExchange(selfID string, localKnown []Peer, remote Peer, remoteKnown []Peer) PeerExchangePlan {
	localGraph := peerAddressMap(localKnown)
	remotePeer := normalizePeer(remote)

	plan := PeerExchangePlan{RelayToKnown: map[string][]Peer{}}
	plan.ShareWithRemote = peersFromAddressMap(localGraph)
	if remotePeer.ID != "" && remotePeer.ID != selfID {
		for _, known := range plan.ShareWithRemote {
			if known.ID == remotePeer.ID {
				continue
			}
			plan.RelayToKnown[known.ID] = []Peer{remotePeer}
		}
	}

	remoteGraph := peerAddressMap(remoteKnown)
	delete(remoteGraph, selfID)
	delete(remoteGraph, remotePeer.ID)
	for id := range localGraph {
		delete(remoteGraph, id)
	}
	plan.Learned = peersFromAddressMap(remoteGraph)
	return plan
}

func peerAddressMap(peers []Peer) map[string]map[string]struct{} {
	peerAddresses := map[string]map[string]struct{}{}
	for _, peer := range peers {
		if peer.ID == "" {
			continue
		}
		addresses := peerAddresses[peer.ID]
		if addresses == nil {
			addresses = map[string]struct{}{}
			peerAddresses[peer.ID] = addresses
		}
		for _, address := range peer.Addresses {
			if address == "" {
				continue
			}
			addresses[address] = struct{}{}
		}
	}
	return peerAddresses
}

func peersFromAddressMap(peerAddresses map[string]map[string]struct{}) []Peer {
	ids := make([]string, 0, len(peerAddresses))
	for id := range peerAddresses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	peers := make([]Peer, 0, len(ids))
	for _, id := range ids {
		addresses := make([]string, 0, len(peerAddresses[id]))
		for address := range peerAddresses[id] {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		peers = append(peers, Peer{ID: id, Addresses: addresses})
	}
	return peers
}

func normalizePeer(peer Peer) Peer {
	peers := peersFromAddressMap(peerAddressMap([]Peer{peer}))
	if len(peers) == 0 {
		return Peer{}
	}
	return peers[0]
}
