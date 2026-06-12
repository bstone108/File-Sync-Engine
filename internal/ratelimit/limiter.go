package ratelimit

import (
	"context"
	"time"
)

// Limiter paces byte transfers against a simple elapsed-time budget. A cap of
// zero disables throttling.
type Limiter struct {
	bytesPerSecond int64
	started        time.Time
	transferred    int64
	elapsed        func() time.Duration
	sleep          func(time.Duration)
}

// NewLimiter returns a limiter backed by the real clock.
func NewLimiter(bytesPerSecond int64) *Limiter {
	started := time.Now()
	return &Limiter{
		bytesPerSecond: bytesPerSecond,
		started:        started,
		elapsed:        func() time.Duration { return time.Since(started) },
		sleep:          time.Sleep,
	}
}

// NewLimiterWithClock is intended for deterministic tests.
func NewLimiterWithClock(bytesPerSecond int64, elapsed func() time.Duration, sleep func(time.Duration)) *Limiter {
	return &Limiter{bytesPerSecond: bytesPerSecond, elapsed: elapsed, sleep: sleep}
}

// Wait records n transferred bytes and blocks until the configured cap allows
// that cumulative total. The sleep is skipped when the cap is zero or negative.
func (l *Limiter) Wait(ctx context.Context, n int) error {
	if l == nil || l.bytesPerSecond <= 0 || n <= 0 {
		return nil
	}
	l.transferred += int64(n)
	wantElapsed := time.Duration(l.transferred) * time.Second / time.Duration(l.bytesPerSecond)
	if l.elapsed == nil {
		return nil
	}
	remaining := wantElapsed - l.elapsed()
	if remaining <= 0 {
		return nil
	}
	if l.sleep == nil {
		l.sleep = time.Sleep
	}
	if ctx == nil {
		l.sleep(remaining)
		return nil
	}
	done := make(chan struct{})
	go func() {
		l.sleep(remaining)
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
