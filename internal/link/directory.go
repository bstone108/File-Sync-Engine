package link

import (
	"context"
	"errors"
	"sync"
)

// Directory stores pairing records for lookup. The public DHT adapter publishes
// the encrypted record under Namespace(code). Namespace uses only the locator
// half. Callers still have to Open the payload with the full code before
// trusting it.
type Directory interface {
	Publish(ctx context.Context, namespace string, payload []byte) error
	Lookup(ctx context.Context, namespace string) ([][]byte, error)
	Unpublish(ctx context.Context, namespace string) error
}

// DHT is the subset of the libp2p discovery router used for pairing codes.
type DHT interface {
	PublishPairing(ctx context.Context, namespace string, payload []byte) error
	LookupPairing(ctx context.Context, namespace string) ([][]byte, error)
	UnpublishPairing(ctx context.Context, namespace string) error
	PeerID() string
}

// DHTDirectory adapts the existing discovery router to Directory.
type DHTDirectory struct {
	DHT DHT
}

func NewDHTDirectory(dht DHT) DHTDirectory {
	return DHTDirectory{DHT: dht}
}

func (d DHTDirectory) Publish(ctx context.Context, namespace string, payload []byte) error {
	if d.DHT == nil {
		return errors.New("pairing DHT is not configured")
	}
	return d.DHT.PublishPairing(ctx, namespace, payload)
}

func (d DHTDirectory) Lookup(ctx context.Context, namespace string) ([][]byte, error) {
	if d.DHT == nil {
		return nil, errors.New("pairing DHT is not configured")
	}
	return d.DHT.LookupPairing(ctx, namespace)
}

func (d DHTDirectory) Unpublish(ctx context.Context, namespace string) error {
	if d.DHT == nil {
		return errors.New("pairing DHT is not configured")
	}
	return d.DHT.UnpublishPairing(ctx, namespace)
}

// MemoryDirectory is an in-process directory used by tests and by two engines
// that share a lookup seam. Publish appends so a second writer can simulate
// a DHT hijack attempt under the same namespace.
type MemoryDirectory struct {
	mu       sync.Mutex
	payloads map[string][][]byte
}

func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{payloads: map[string][][]byte{}}
}

func (m *MemoryDirectory) Publish(_ context.Context, namespace string, payload []byte) error {
	if namespace == "" || len(payload) == 0 {
		return errors.New("pairing record is incomplete")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.payloads[namespace] = append(m.payloads[namespace], append([]byte(nil), payload...))
	return nil
}

func (m *MemoryDirectory) Lookup(_ context.Context, namespace string) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := m.payloads[namespace]
	out := make([][]byte, 0, len(stored))
	for _, payload := range stored {
		out = append(out, append([]byte(nil), payload...))
	}
	return out, nil
}

func (m *MemoryDirectory) Unpublish(_ context.Context, namespace string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.payloads, namespace)
	return nil
}

// UnavailableDirectory reports that no DHT is configured.
type UnavailableDirectory struct{}

func (UnavailableDirectory) Publish(context.Context, string, []byte) error {
	return errors.New("pairing codes require DHT discovery; paste a pairing package instead")
}

func (UnavailableDirectory) Lookup(context.Context, string) ([][]byte, error) {
	return nil, errors.New("pairing codes require DHT discovery; paste a pairing package instead")
}

func (UnavailableDirectory) Unpublish(context.Context, string) error {
	return errors.New("pairing codes require DHT discovery; paste a pairing package instead")
}
