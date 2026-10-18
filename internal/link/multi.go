package link

import (
	"context"
	"errors"
	"sync"

	"filesyncengine/internal/structuredlog"
)

// MultiDirectory publishes to every directory and looks them up together.
// One successful publish is enough. The first lookup that returns a record wins.
type MultiDirectory struct {
	dirs   []Directory
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func NewMultiDirectory(dirs ...Directory) *MultiDirectory {
	out := make([]Directory, 0, len(dirs))
	for _, dir := range dirs {
		if dir != nil {
			out = append(out, dir)
		}
	}
	return &MultiDirectory{dirs: out, cancel: map[string]context.CancelFunc{}}
}

// Publish stores the record in every directory. The call returns as soon as
// one directory accepts it, so a local publish is not blocked by public DHT
// bootstrap. The remaining publishes keep running until they finish or
// Unpublish cancels them.
func (m *MultiDirectory) Publish(ctx context.Context, namespace string, payload []byte) error {
	if m == nil || len(m.dirs) == 0 {
		return errors.New("pairing directory is not configured")
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	pubCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	if prev := m.cancel[namespace]; prev != nil {
		prev()
	}
	m.cancel[namespace] = cancel
	m.mu.Unlock()

	results := make(chan error, len(m.dirs))
	for _, dir := range m.dirs {
		go func(dir Directory) {
			err := dir.Publish(pubCtx, namespace, payload)
			if err != nil && !errors.Is(err, ErrPeerDirectoryLookupOnly) {
				structuredlog.Event("error", "pairing.publish.failed", "pairing directory rejected a record", map[string]any{"error": err.Error()})
			}
			results <- err
		}(dir)
	}
	var errs []error
	received := 0
	for received < len(m.dirs) {
		err := <-results
		received++
		if err == nil {
			left := len(m.dirs) - received
			go func() {
				for range left {
					<-results
				}
			}()
			return nil
		}
		errs = append(errs, err)
	}
	m.mu.Lock()
	if m.cancel[namespace] != nil {
		delete(m.cancel, namespace)
	}
	m.mu.Unlock()
	cancel()
	return publishError(errs)
}

// PublishAll waits until every directory that stores records has finished.
// A lookup-only peer directory is not a failure. A later DHT error is returned
// even when a local directory already stored the payload, so a restart can
// retry the provider advertisement without dropping the on-disk code.
func (m *MultiDirectory) PublishAll(ctx context.Context, namespace string, payload []byte) error {
	if m == nil || len(m.dirs) == 0 {
		return errors.New("pairing directory is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	results := make(chan error, len(m.dirs))
	for _, dir := range m.dirs {
		go func(dir Directory) {
			err := dir.Publish(ctx, namespace, payload)
			if err != nil && !errors.Is(err, ErrPeerDirectoryLookupOnly) {
				structuredlog.Event("error", "pairing.publish.failed", "pairing directory rejected a record", map[string]any{"error": err.Error()})
			}
			results <- err
		}(dir)
	}
	var errs []error
	stored := false
	for range m.dirs {
		err := <-results
		if err == nil {
			stored = true
			continue
		}
		if errors.Is(err, ErrPeerDirectoryLookupOnly) {
			continue
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		if stored {
			return nil
		}
		return errors.New("pairing record was not stored")
	}
	joined := errors.Join(errs...)
	if stored {
		return partialPublishError{error: joined}
	}
	return joined
}

// partialPublishError means at least one directory stored the record and
// another, usually the DHT, did not. The stored copy must be kept.
type partialPublishError struct{ error }

func publishError(errs []error) error {
	kept := make([]error, 0, len(errs))
	for _, err := range errs {
		if err == nil || errors.Is(err, ErrPeerDirectoryLookupOnly) {
			continue
		}
		kept = append(kept, err)
	}
	if len(kept) == 0 {
		return errors.New("pairing record was not stored")
	}
	return errors.Join(kept...)
}

// Lookup tries directories in order and stops at the first record.
// Callers pass LAN, then connected peers, then the public DHT, so a local
// hit does not wait on a DHT walk.
func (m *MultiDirectory) Lookup(ctx context.Context, namespace string) ([][]byte, error) {
	if m == nil || len(m.dirs) == 0 {
		return nil, errors.New("pairing directory is not configured")
	}
	var errs []error
	for _, dir := range m.dirs {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		payloads, err := dir.Lookup(ctx, namespace)
		if err != nil {
			errs = append(errs, err)
			structuredlog.Event("warn", "pairing.lookup.failed", "pairing directory lookup failed", map[string]any{"error": err.Error()})
			continue
		}
		if len(payloads) > 0 {
			return payloads, nil
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return nil, nil
}

func (m *MultiDirectory) Unpublish(ctx context.Context, namespace string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if cancel := m.cancel[namespace]; cancel != nil {
		cancel()
		delete(m.cancel, namespace)
	}
	m.mu.Unlock()
	var errs []error
	for _, dir := range m.dirs {
		if err := dir.Unpublish(ctx, namespace); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
