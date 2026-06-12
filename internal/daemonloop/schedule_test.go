package daemonloop

import (
	"testing"
	"time"
)

func TestPollScheduleRunsDueWorkAndAdvancesFromCurrentTime(t *testing.T) {
	start := time.Unix(100, 0)
	schedule := NewPollSchedule(start, start)
	later := start.Add(5 * time.Second)

	if !schedule.DiscoveryDue(later, 30*time.Second) {
		t.Fatalf("expected initial discovery poll to be due")
	}
	if got, want := schedule.NextDiscovery(), later.Add(30*time.Second); !got.Equal(want) {
		t.Fatalf("next discovery = %v, want %v", got, want)
	}
	if schedule.DiscoveryDue(later.Add(29*time.Second), 30*time.Second) {
		t.Fatalf("discovery poll should not be due before advanced deadline")
	}

	if !schedule.MetadataDue(later, 45*time.Second) {
		t.Fatalf("expected initial metadata reconciliation to be due")
	}
	if got, want := schedule.NextMetadata(), later.Add(45*time.Second); !got.Equal(want) {
		t.Fatalf("next metadata = %v, want %v", got, want)
	}
	if schedule.MetadataDue(later.Add(44*time.Second), 45*time.Second) {
		t.Fatalf("metadata reconciliation should not be due before advanced deadline")
	}
}

func TestPollScheduleSkipsWorkBeforeDeadline(t *testing.T) {
	start := time.Unix(200, 0)
	schedule := NewPollSchedule(start.Add(10*time.Second), start.Add(20*time.Second))

	if schedule.DiscoveryDue(start.Add(9*time.Second), 30*time.Second) {
		t.Fatalf("discovery should not run before deadline")
	}
	if schedule.MetadataDue(start.Add(19*time.Second), 45*time.Second) {
		t.Fatalf("metadata should not run before deadline")
	}
}
