package link

import (
	"context"
	"strings"
	"testing"
	"time"
)

type blockingDirectory struct {
	started chan struct{}
	payload []byte
}

func (d *blockingDirectory) Publish(ctx context.Context, namespace string, payload []byte) error {
	select {
	case <-d.started:
	default:
		close(d.started)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
	}
	d.payload = append([]byte(nil), payload...)
	return nil
}

func (d *blockingDirectory) Lookup(context.Context, string) ([][]byte, error) {
	return nil, nil
}

func (d *blockingDirectory) Unpublish(context.Context, string) error {
	return nil
}

func TestLookupOnlyDirectoryIsNotReportedAsPublishFailure(t *testing.T) {
	dir := NewMultiDirectory(PeerAPIDirectory{})
	err := dir.Publish(context.Background(), "pair-namespace", []byte("sealed-pairing-record"))
	if err == nil {
		t.Fatal("lookup-only publish succeeded")
	}
	if strings.Contains(err.Error(), "do not publish") || strings.Contains(err.Error(), "do not store") || strings.Contains(err.Error(), "connected peers") {
		t.Fatalf("publish error blames connected peers: %v", err)
	}
	if !strings.Contains(err.Error(), "not stored") {
		t.Fatalf("publish error = %v", err)
	}
}

func TestPublishReturnsWhenOneDirectoryAccepts(t *testing.T) {
	fast := NewMemoryDirectory()
	slow := &blockingDirectory{started: make(chan struct{})}
	dir := NewMultiDirectory(fast, slow)
	done := make(chan error, 1)
	go func() {
		done <- dir.Publish(context.Background(), "pair-namespace", []byte("sealed-pairing-record"))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("publish waited for the slow directory")
	}
	payloads, err := fast.Lookup(context.Background(), "pair-namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || string(payloads[0]) != "sealed-pairing-record" {
		t.Fatalf("payloads = %q", payloads)
	}
	if err := dir.Unpublish(context.Background(), "pair-namespace"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-slow.started:
	case <-time.After(time.Second):
		t.Fatal("slow publish did not start")
	}
	time.Sleep(50 * time.Millisecond)
	if len(slow.payload) != 0 {
		t.Fatalf("cancelled publish stored %q", slow.payload)
	}
	left, err := fast.Lookup(context.Background(), "pair-namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("unpublished payloads = %q", left)
	}
}
