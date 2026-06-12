package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestLimiterSleepsToKeepBytesUnderConfiguredRate(t *testing.T) {
	elapsed := time.Duration(0)
	var slept []time.Duration
	limiter := NewLimiterWithClock(100, func() time.Duration { return elapsed }, func(d time.Duration) {
		slept = append(slept, d)
		elapsed += d
	})

	if err := limiter.Wait(context.Background(), 50); err != nil {
		t.Fatalf("Wait first chunk: %v", err)
	}
	if len(slept) != 1 || slept[0] != 500*time.Millisecond {
		t.Fatalf("first sleep = %v, want 500ms", slept)
	}
	if err := limiter.Wait(context.Background(), 25); err != nil {
		t.Fatalf("Wait second chunk: %v", err)
	}
	if len(slept) != 2 || slept[1] != 250*time.Millisecond {
		t.Fatalf("second sleep = %v, want 250ms", slept)
	}
}

func TestLimiterDoesNotSleepWhenCapDisabled(t *testing.T) {
	limiter := NewLimiterWithClock(0, func() time.Duration { return 0 }, func(time.Duration) { t.Fatal("disabled limiter should not sleep") })
	if err := limiter.Wait(context.Background(), 1024); err != nil {
		t.Fatalf("Wait disabled cap: %v", err)
	}
}
