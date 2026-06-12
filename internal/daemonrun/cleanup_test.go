package daemonrun

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingCloser struct {
	closed bool
	err    error
}

func (r *recordingCloser) Close() error {
	r.closed = true
	return r.err
}

type recordingHTTPServer struct {
	shutdown bool
	deadline bool
	err      error
}

func (r *recordingHTTPServer) Shutdown(ctx context.Context) error {
	r.shutdown = true
	deadline, ok := ctx.Deadline()
	r.deadline = ok && time.Until(deadline) <= time.Second
	return r.err
}

func TestCleanupClosesMonitorAndShutsDownHTTPServerWithTimeout(t *testing.T) {
	monitor := &recordingCloser{}
	server := &recordingHTTPServer{}

	errs := Cleanup(CleanupOptions{Monitor: monitor, HTTPServer: server, HTTPShutdownTimeout: 200 * time.Millisecond})

	if len(errs) != 0 {
		t.Fatalf("expected no cleanup errors, got %v", errs)
	}
	if !monitor.closed {
		t.Fatalf("expected monitor to be closed")
	}
	if !server.shutdown {
		t.Fatalf("expected HTTP server to be shut down")
	}
	if !server.deadline {
		t.Fatalf("expected HTTP shutdown to receive a bounded context")
	}
}

func TestCleanupCollectsMonitorAndHTTPShutdownErrors(t *testing.T) {
	monitorErr := errors.New("monitor close failed")
	serverErr := errors.New("server shutdown failed")
	monitor := &recordingCloser{err: monitorErr}
	server := &recordingHTTPServer{err: serverErr}

	errs := Cleanup(CleanupOptions{Monitor: monitor, HTTPServer: server, HTTPShutdownTimeout: 200 * time.Millisecond})

	if len(errs) != 2 {
		t.Fatalf("expected two cleanup errors, got %d: %v", len(errs), errs)
	}
	if !errors.Is(errs[0], monitorErr) {
		t.Fatalf("expected monitor error first, got %v", errs[0])
	}
	if !errors.Is(errs[1], serverErr) {
		t.Fatalf("expected server error second, got %v", errs[1])
	}
}

func TestCleanupIgnoresNilResources(t *testing.T) {
	errs := Cleanup(CleanupOptions{HTTPShutdownTimeout: 200 * time.Millisecond})

	if len(errs) != 0 {
		t.Fatalf("expected no cleanup errors for nil resources, got %v", errs)
	}
}
