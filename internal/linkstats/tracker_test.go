package linkstats

import (
	"testing"
	"time"
)

func TestTrackerAveragesRecentBytesAndReducesPending(t *testing.T) {
	now := time.Date(2026, 10, 15, 16, 0, 0, 0, time.UTC)
	tracker := New()
	tracker.now = func() time.Time { return now }

	tracker.SetPending("peer-b", 3000)
	tracker.AddTransferred("peer-b", 1500)
	got := tracker.Get("peer-b")
	if got.PendingBytes != 1500 {
		t.Fatalf("pending = %d", got.PendingBytes)
	}
	if got.AverageBytesPerSecond != 1500/30 {
		t.Fatalf("rate = %d", got.AverageBytesPerSecond)
	}

	now = now.Add(31 * time.Second)
	got = tracker.Get("peer-b")
	if got.AverageBytesPerSecond != 0 || got.PendingBytes != 1500 {
		t.Fatalf("aged sample = %+v", got)
	}

	total := tracker.Total()
	if total.PendingBytes != 1500 {
		t.Fatalf("total pending = %d", total.PendingBytes)
	}
}

func TestTrackerIgnoresEmptyPeers(t *testing.T) {
	tracker := New()
	tracker.AddTransferred("", 10)
	tracker.SetPending("", 10)
	if got := tracker.Get(""); got != (Sample{}) {
		t.Fatalf("empty peer = %+v", got)
	}
}
