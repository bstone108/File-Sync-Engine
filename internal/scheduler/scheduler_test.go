package scheduler

import (
	"testing"
	"time"
)

func TestSchedulerDebouncesWatcherEvents(t *testing.T) {
	start := time.Unix(100, 0)
	s := New(Options{EventDebounce: 200 * time.Millisecond, FallbackInterval: time.Minute})
	s.AddFolder("docs", start)
	s.Notify("docs", start.Add(10*time.Millisecond))
	s.Notify("docs", start.Add(100*time.Millisecond))

	if due := s.Due(start.Add(250 * time.Millisecond)); len(due) != 0 {
		t.Fatalf("scan ran before quiet debounce: %+v", due)
	}
	due := s.Due(start.Add(301 * time.Millisecond))
	if len(due) != 1 || due[0] != "docs" {
		t.Fatalf("expected docs due after debounce, got %+v", due)
	}
	if due := s.Due(start.Add(302 * time.Millisecond)); len(due) != 0 {
		t.Fatalf("event scan should be consumed once, got %+v", due)
	}
}

func TestSchedulerFallbackScanCatchesMissedWatcherEvents(t *testing.T) {
	start := time.Unix(200, 0)
	s := New(Options{EventDebounce: 100 * time.Millisecond, FallbackInterval: 15 * time.Second})
	s.AddFolder("docs", start)

	if due := s.Due(start.Add(14 * time.Second)); len(due) != 0 {
		t.Fatalf("fallback ran too early: %+v", due)
	}
	due := s.Due(start.Add(15 * time.Second))
	if len(due) != 1 || due[0] != "docs" {
		t.Fatalf("expected fallback scan for docs, got %+v", due)
	}
	if due := s.Due(start.Add(29 * time.Second)); len(due) != 0 {
		t.Fatalf("fallback should reset after scan, got %+v", due)
	}
}
