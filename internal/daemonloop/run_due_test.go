package daemonloop

import (
	"testing"
	"time"
)

func TestRunDueWorkRunsDueActionsInDiscoveryThenMetadataOrder(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	schedule := NewPollSchedule(now, now)
	calls := []string{}

	result := RunDueWork(&schedule, now, DueWorkOptions{
		DiscoveryInterval: time.Minute,
		MetadataInterval:  2 * time.Minute,
		Discovery: func() {
			calls = append(calls, "discovery")
		},
		Metadata: func() {
			calls = append(calls, "metadata")
		},
	})

	if !result.DiscoveryRan || !result.MetadataRan {
		t.Fatalf("expected both due actions to run, got %+v", result)
	}
	if got := calls; len(got) != 2 || got[0] != "discovery" || got[1] != "metadata" {
		t.Fatalf("expected discovery then metadata callbacks, got %v", got)
	}
	if !schedule.NextDiscovery().Equal(now.Add(time.Minute)) {
		t.Fatalf("expected discovery deadline advanced from tick time, got %s", schedule.NextDiscovery())
	}
	if !schedule.NextMetadata().Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("expected metadata deadline advanced from tick time, got %s", schedule.NextMetadata())
	}
}

func TestRunDueWorkSkipsCallbacksBeforeDeadline(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	schedule := NewPollSchedule(now.Add(time.Minute), now.Add(2*time.Minute))
	called := false

	result := RunDueWork(&schedule, now, DueWorkOptions{
		DiscoveryInterval: time.Minute,
		MetadataInterval:  time.Minute,
		Discovery: func() {
			called = true
		},
		Metadata: func() {
			called = true
		},
	})

	if result.DiscoveryRan || result.MetadataRan {
		t.Fatalf("expected no due actions before deadlines, got %+v", result)
	}
	if called {
		t.Fatal("callbacks should not run before deadlines")
	}
}
