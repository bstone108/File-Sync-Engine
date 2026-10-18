// Package transferprogress records live folder-sync byte counts for the API and GUI.
package transferprogress

import (
	"sync"
	"time"
)

// Snapshot is one peer's current receive progress.
type Snapshot struct {
	Active         bool
	ReceivedBytes  int64
	RemainingBytes int64
	BytesPerSecond int64
	Updated        time.Time
}

type state struct {
	received    int64
	remaining   int64
	windowStart time.Time
	windowBytes int64
	rate        int64
	updated     time.Time
}

var (
	mu     sync.Mutex
	states = map[string]state{}
)

// Note records bytes just received and how many bytes are still outstanding.
func Note(peerID string, delta, remaining int64) {
	if peerID == "" || delta < 0 {
		return
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	current := states[peerID]
	if current.windowStart.IsZero() || now.Sub(current.windowStart) > 5*time.Second {
		current.windowStart = now
		current.windowBytes = 0
	}
	current.windowBytes += delta
	current.received += delta
	current.remaining = remaining
	elapsed := now.Sub(current.windowStart)
	if elapsed > 0 && current.windowBytes > 0 {
		current.rate = int64(float64(current.windowBytes) / elapsed.Seconds())
	}
	if remaining <= 0 && delta == 0 {
		current.rate = 0
	}
	current.updated = now
	states[peerID] = current
}

// ActiveSnapshots returns peers that are still receiving bytes.
func ActiveSnapshots() map[string]Snapshot {
	mu.Lock()
	defer mu.Unlock()
	out := map[string]Snapshot{}
	for id, current := range states {
		snap := snapshotOf(current)
		if snap.Active {
			out[id] = snap
		}
	}
	return out
}

// Reset clears recorded progress. Tests use it between cases.
func Reset() {
	mu.Lock()
	states = map[string]state{}
	mu.Unlock()
}

// Get returns the latest snapshot. An idle peer reports zeros.
func Get(peerID string) Snapshot {
	mu.Lock()
	defer mu.Unlock()
	current, ok := states[peerID]
	if !ok {
		return Snapshot{}
	}
	return snapshotOf(current)
}

func snapshotOf(current state) Snapshot {
	active := current.remaining > 0 && time.Since(current.updated) < 15*time.Second
	rate := current.rate
	if !active {
		rate = 0
	}
	return Snapshot{
		Active:         active,
		ReceivedBytes:  current.received,
		RemainingBytes: current.remaining,
		BytesPerSecond: rate,
		Updated:        current.updated,
	}
}
