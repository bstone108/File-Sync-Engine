package daemonstop

import (
	"context"
	"sync"
)

// Signal provides an idempotent daemon stop handler and a channel the runtime can select on.
type Signal struct {
	done chan struct{}
	once sync.Once
}

// NewSignal returns a stop signal with an API-compatible handler.
func NewSignal() *Signal {
	return &Signal{done: make(chan struct{})}
}

// Done is closed after the first successful stop request.
func (s *Signal) Done() <-chan struct{} {
	return s.done
}

// Handler can be registered with the daemon API stop endpoint.
func (s *Signal) Handler(ctx context.Context) error {
	_ = ctx
	s.once.Do(func() { close(s.done) })
	return nil
}
