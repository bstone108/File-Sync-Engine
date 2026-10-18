package discovery

import (
	"strings"
	"sync"

	"filesyncengine/internal/config"
)

// Discovery runs in this order. The public DHT is only the backup.
const (
	PriorityManual       = 1
	PriorityPeerExchange = 2
	PriorityLocal        = 3
	PriorityDHT          = 4
)

const (
	backupNoPeers     = "no-peers"
	backupNoDetails   = "no-connection-details"
	backupUnreachable = "peer-unreachable"
	backupNotNeeded   = ""
)

// NeedsDHTBackup reports whether a public DHT walk is allowed.
// Saved addresses, peer exchange, and LAN discovery are tried first.
// The DHT is used when this engine has no usable connection details, or when
// a known peer stayed unreachable through those earlier paths.
func NeedsDHTBackup(peers []config.PeerConfig, exchanged []Peer, unreachable []string) (bool, string) {
	usable := map[string]struct{}{}
	for _, peer := range peers {
		if peer.ID == "" {
			continue
		}
		if peerHasDetails(peer) {
			usable[peer.ID] = struct{}{}
		}
	}
	for _, peer := range exchanged {
		if peer.ID != "" && len(peer.Addresses) > 0 {
			usable[peer.ID] = struct{}{}
		}
	}
	if len(peers) == 0 && len(usable) == 0 {
		return true, backupNoPeers
	}
	if len(usable) == 0 {
		return true, backupNoDetails
	}
	for _, id := range unreachable {
		if id == "" {
			continue
		}
		if _, ok := usable[id]; ok {
			return true, backupUnreachable
		}
	}
	return false, backupNotNeeded
}

func peerHasDetails(peer config.PeerConfig) bool {
	if len(peer.Addresses) > 0 || len(peer.Relays) > 0 {
		return true
	}
	for _, endpoint := range peer.Endpoints {
		if endpoint.Address != "" {
			return true
		}
	}
	return false
}

var (
	catalogMu      sync.Mutex
	exchangedPeers []Peer
	localPeers     []Peer
	dhtPeers       []Peer
	unreachableSet = map[string]struct{}{}
	localSource    Source
	pendingDHT     = map[string]struct{}{}
)

// NoteExchangedPeers stores addresses learned from a connected peer or from
// same-identity connection details. They are tried before LAN and DHT.
func NoteExchangedPeers(peers []Peer) {
	catalogMu.Lock()
	exchangedPeers = mergePeers(exchangedPeers, peers)
	catalogMu.Unlock()
}

// NoteLocalPeers stores LAN announcements. They are tried after saved and
// exchanged addresses and before the DHT.
func NoteLocalPeers(peers []Peer) {
	catalogMu.Lock()
	localPeers = mergePeers(localPeers, peers)
	catalogMu.Unlock()
}

// NoteDHTPeers stores a backup lookup for one dial attempt. The addresses are
// not peer endpoints. PendingDHTAddress stays true until that attempt
// authenticates as the expected peer, so a lookup cannot be written to config.
func NoteDHTPeers(peers []Peer) {
	catalogMu.Lock()
	dhtPeers = mergePeers(dhtPeers, peers)
	for _, peer := range peers {
		for _, address := range peer.Addresses {
			address = strings.TrimSpace(address)
			if address != "" {
				pendingDHT[address] = struct{}{}
			}
		}
	}
	catalogMu.Unlock()
}

// PendingDHTAddress reports an address learned from the DHT that has not
// authenticated as the expected peer. Callers must not persist it.
func PendingDHTAddress(address string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return false
	}
	catalogMu.Lock()
	defer catalogMu.Unlock()
	_, ok := pendingDHT[address]
	return ok
}

// TakeDHTAddresses returns the DHT candidates for one peer and forgets them.
// A later dial does not reuse the same unauthenticated lookup.
func TakeDHTAddresses(peerID string) []string {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	addrs := addressesOf(dhtPeers, peerID)
	dhtPeers = removePeer(dhtPeers, peerID)
	return addrs
}

// MarkAddressAuthenticated records that a dial authenticated this address as
// the expected peer. Only then may a caller persist it.
func MarkAddressAuthenticated(address string) {
	address = strings.TrimSpace(address)
	if address == "" {
		return
	}
	catalogMu.Lock()
	delete(pendingDHT, address)
	catalogMu.Unlock()
}

// NotePeerUnreachable marks a known peer that could not be reached through
// saved, exchanged, and LAN addresses. The next discovery pass may use the DHT.
func NotePeerUnreachable(id string) {
	if id == "" {
		return
	}
	catalogMu.Lock()
	unreachableSet[id] = struct{}{}
	catalogMu.Unlock()
}

// NotePeerReachable clears the DHT re-find flag after a connection succeeds.
func NotePeerReachable(id string) {
	catalogMu.Lock()
	delete(unreachableSet, id)
	catalogMu.Unlock()
}

// ExchangedPeers returns the peer-exchange and identity-connection addresses.
func ExchangedPeers() []Peer {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	return append([]Peer(nil), exchangedPeers...)
}

// UnreachableIDs returns peers that need a DHT re-find.
func UnreachableIDs() []string {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	out := make([]string, 0, len(unreachableSet))
	for id := range unreachableSet {
		out = append(out, id)
	}
	return out
}

// AddressesFor returns learned addresses for one peer in discovery order:
// peer exchange, then LAN, then DHT.
func AddressesFor(peerID string) (exchanged, local, dht []string) {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	return addressesOf(exchangedPeers, peerID), addressesOf(localPeers, peerID), addressesOf(dhtPeers, peerID)
}

// UseLocalSource installs the LAN announcement source used by the daemon poll.
func UseLocalSource(source Source) {
	catalogMu.Lock()
	localSource = source
	catalogMu.Unlock()
}

// LocalSource is the LAN announcement source, remembering what it learns.
func LocalSource() Source {
	src := currentLocalSource()
	if src == nil {
		return nil
	}
	return rememberingSource{inner: src, note: NoteLocalPeers}
}

// BackupDHTSource polls the public DHT only when allow reports a backup reason.
func BackupDHTSource(inner Source, allow func() (bool, string)) Source {
	return gatedSource{inner: inner, allow: allow}
}

func currentLocalSource() Source {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	return localSource
}

// ResetCatalog clears learned addresses. Tests use it between cases.
func ResetCatalog() {
	catalogMu.Lock()
	exchangedPeers = nil
	localPeers = nil
	dhtPeers = nil
	unreachableSet = map[string]struct{}{}
	localSource = nil
	pendingDHT = map[string]struct{}{}
	catalogMu.Unlock()
}

func removePeer(peers []Peer, id string) []Peer {
	if id == "" {
		return peers
	}
	out := make([]Peer, 0, len(peers))
	for _, peer := range peers {
		if peer.ID != id {
			out = append(out, peer)
		}
	}
	return out
}

func addressesOf(peers []Peer, id string) []string {
	for _, peer := range peers {
		if peer.ID == id {
			return append([]string(nil), peer.Addresses...)
		}
	}
	return nil
}

func mergePeers(existing, extra []Peer) []Peer {
	index := map[string]int{}
	out := append([]Peer(nil), existing...)
	for i, peer := range out {
		index[peer.ID] = i
	}
	for _, peer := range extra {
		if peer.ID == "" || len(peer.Addresses) == 0 {
			continue
		}
		if at, ok := index[peer.ID]; ok {
			seen := map[string]struct{}{}
			for _, addr := range out[at].Addresses {
				seen[addr] = struct{}{}
			}
			for _, addr := range peer.Addresses {
				if addr == "" {
					continue
				}
				if _, dup := seen[addr]; dup {
					continue
				}
				seen[addr] = struct{}{}
				out[at].Addresses = append(out[at].Addresses, addr)
			}
			continue
		}
		index[peer.ID] = len(out)
		out = append(out, Peer{ID: peer.ID, Addresses: append([]string(nil), peer.Addresses...)})
	}
	return out
}

type rememberingSource struct {
	inner Source
	note  func([]Peer)
}

func (s rememberingSource) Peers() ([]Peer, error) {
	if s.inner == nil {
		return nil, nil
	}
	peers, err := s.inner.Peers()
	if err != nil {
		return nil, err
	}
	if s.note != nil {
		s.note(peers)
	}
	return peers, nil
}

// gatedSource skips the public DHT unless NeedsDHTBackup allows it.
type gatedSource struct {
	inner Source
	allow func() (bool, string)
}

func (s gatedSource) Peers() ([]Peer, error) {
	if s.allow != nil {
		ok, _ := s.allow()
		if !ok {
			return nil, nil
		}
	}
	if s.inner == nil {
		return nil, nil
	}
	peers, err := s.inner.Peers()
	if err != nil {
		return nil, err
	}
	NoteDHTPeers(peers)
	return peers, nil
}
