package nat

import "sync"

// Observation is the path currently used for one peer.
type Observation struct {
	PeerID  string
	Path    Path
	Address string
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
	r.peers[peerID] = Observation{PeerID: peerID, Path: path, Address: address}
}

// Active returns the best path recorded for peerID.
func (r *Registry) Active(peerID string) (Observation, bool) {
	if r == nil || peerID == "" {
		return Observation{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	obs, ok := r.peers[peerID]
	return obs, ok && obs.Path != ""
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
