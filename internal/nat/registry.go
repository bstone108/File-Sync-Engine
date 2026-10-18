package nat

import (
	"sync"
	"time"
)

// pathFreshFor is how long a successful dial keeps the GUI path. A later
// success refreshes it. A path that nobody has confirmed again is not shown
// as a live connection.
const pathFreshFor = 2 * time.Minute

// Observation is the path currently used for one peer.
type Observation struct {
	PeerID  string
	Path    Path
	Address string
	At      time.Time
}

// Registry remembers the best live path for each peer.
type Registry struct {
	mu    sync.Mutex
	peers map[string]Observation
}

// DefaultPaths is the process-wide path table shown by the API and GUI.
var DefaultPaths = &Registry{}

// Observe records a path. A worse path does not replace a better one.
func (r *Registry) Observe(peerID, address string, path Path) {
	if r == nil || peerID == "" || path == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peers == nil {
		r.peers = map[string]Observation{}
	}
	current := r.peers[peerID]
	if !Better(current.Path, path) && current.Path != "" {
		return
	}
	r.peers[peerID] = Observation{PeerID: peerID, Path: path, Address: address, At: time.Now()}
}

// ObserveLimited records that the only live path is a limited relay.
// It replaces an unclassified or unlimited-relay label. A LAN, direct,
// hole-punched, or Tailscale path stays in place.
func (r *Registry) ObserveLimited(peerID, address string) {
	if r == nil || peerID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peers == nil {
		r.peers = map[string]Observation{}
	}
	current := r.peers[peerID]
	switch current.Path {
	case "", PathRelay, PathLimitedRelay:
	default:
		return
	}
	if address == "" {
		address = current.Address
	}
	r.peers[peerID] = Observation{PeerID: peerID, Path: PathLimitedRelay, Address: address, At: time.Now()}
}

// Touch keeps an existing path fresh after another successful request on the
// cached connection. It does not invent a path that was never dialed.
func (r *Registry) Touch(peerID string) {
	if r == nil || peerID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.peers[peerID]
	if !ok || current.Path == "" {
		return
	}
	current.At = time.Now()
	r.peers[peerID] = current
}

// Active returns the best path recorded for peerID while it is still fresh.
func (r *Registry) Active(peerID string) (Observation, bool) {
	if r == nil || peerID == "" {
		return Observation{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	obs, ok := r.peers[peerID]
	if !ok || obs.Path == "" || time.Since(obs.At) > pathFreshFor {
		return Observation{}, false
	}
	return obs, true
}

// Forget drops a peer's path. A failed dial calls it so the API does not
// keep reporting a connection that is no longer up.
func (r *Registry) Forget(peerID string) {
	if r == nil || peerID == "" {
		return
	}
	r.mu.Lock()
	delete(r.peers, peerID)
	r.mu.Unlock()
}

// Reset clears the table. Tests use it between cases.
func (r *Registry) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.peers = map[string]Observation{}
	r.mu.Unlock()
}
