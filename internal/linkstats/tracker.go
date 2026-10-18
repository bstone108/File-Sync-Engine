// Package linkstats keeps the short transfer numbers the instance list shows:
// a recent average data rate with a peer, and bytes still waiting to sync.
package linkstats

import (
	"sync"
	"time"
)

const averageWindow = 30 * time.Second

// Sample is one peer's current rate and backlog.
type Sample struct {
	AverageBytesPerSecond int64
	PendingBytes          int64
}

type observation struct {
	at    time.Time
	bytes int64
}

type peerStats struct {
	pending int64
	samples []observation
}

// Tracker records transferred bytes and a pending-byte backlog per peer.
type Tracker struct {
	mu    sync.Mutex
	now   func() time.Time
	peers map[string]*peerStats
}

// Default is the process-wide tracker the sync paths and the API share.
var Default = New()

// New returns an empty tracker.
func New() *Tracker {
	return &Tracker{now: time.Now, peers: map[string]*peerStats{}}
}

// AddTransferred records n bytes just moved with peerID and reduces that
// peer's pending backlog by the same amount.
func (t *Tracker) AddTransferred(peerID string, n int64) {
	if t == nil || peerID == "" || n <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	peer := t.peerLocked(peerID)
	peer.samples = append(peer.samples, observation{at: t.now(), bytes: n})
	if peer.pending > n {
		peer.pending -= n
	} else {
		peer.pending = 0
	}
	t.pruneLocked(peer)
}

// SetPending replaces the unsynced byte backlog for peerID.
func (t *Tracker) SetPending(peerID string, bytes int64) {
	if t == nil || peerID == "" {
		return
	}
	if bytes < 0 {
		bytes = 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peerLocked(peerID).pending = bytes
}

// Get returns the current sample for one peer.
func (t *Tracker) Get(peerID string) Sample {
	if t == nil || peerID == "" {
		return Sample{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	peer := t.peers[peerID]
	if peer == nil {
		return Sample{}
	}
	t.pruneLocked(peer)
	return Sample{AverageBytesPerSecond: averageLocked(peer, t.now()), PendingBytes: peer.pending}
}

// Total sums every peer. The local engine row uses this.
func (t *Tracker) Total() Sample {
	if t == nil {
		return Sample{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var total Sample
	for _, peer := range t.peers {
		t.pruneLocked(peer)
		total.AverageBytesPerSecond += averageLocked(peer, now)
		total.PendingBytes += peer.pending
	}
	return total
}

func (t *Tracker) peerLocked(peerID string) *peerStats {
	peer := t.peers[peerID]
	if peer == nil {
		peer = &peerStats{}
		t.peers[peerID] = peer
	}
	return peer
}

func (t *Tracker) pruneLocked(peer *peerStats) {
	cutoff := t.now().Add(-averageWindow)
	kept := peer.samples[:0]
	for _, sample := range peer.samples {
		if !sample.at.Before(cutoff) {
			kept = append(kept, sample)
		}
	}
	peer.samples = kept
}

func averageLocked(peer *peerStats, now time.Time) int64 {
	cutoff := now.Add(-averageWindow)
	var sum int64
	for _, sample := range peer.samples {
		if !sample.at.Before(cutoff) {
			sum += sample.bytes
		}
	}
	if sum == 0 {
		return 0
	}
	seconds := int64(averageWindow / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return sum / seconds
}
