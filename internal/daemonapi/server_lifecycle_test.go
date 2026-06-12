package daemonapi

import (
	"errors"
	"net/http"
	"testing"
)

type fakeHTTPServerStarter struct {
	plainCalls int
	tlsCalls   int
	certFile   string
	keyFile    string
	plainErr   error
	tlsErr     error
}

func (f *fakeHTTPServerStarter) ListenAndServe() error {
	f.plainCalls++
	return f.plainErr
}

func (f *fakeHTTPServerStarter) ListenAndServeTLS(certFile, keyFile string) error {
	f.tlsCalls++
	f.certFile = certFile
	f.keyFile = keyFile
	return f.tlsErr
}

func TestServePreparedHTTPServerUsesPlainListenerForPlainPlan(t *testing.T) {
	starter := &fakeHTTPServerStarter{plainErr: http.ErrServerClosed}
	listening := ""
	var stopped []error

	ServePreparedHTTPServer(starter, HTTPServerPlan{Enabled: true, Listen: "127.0.0.1:9000"}, func(addr string) {
		listening = addr
	}, func(err error) {
		stopped = append(stopped, err)
	})

	if listening != "127.0.0.1:9000" {
		t.Fatalf("expected listening log for configured address, got %q", listening)
	}
	if starter.plainCalls != 1 || starter.tlsCalls != 0 {
		t.Fatalf("expected one plain serve call and no TLS calls, got plain=%d tls=%d", starter.plainCalls, starter.tlsCalls)
	}
	if len(stopped) != 0 {
		t.Fatalf("expected normal server close to be suppressed, got %v", stopped)
	}
}

func TestServePreparedHTTPServerUsesTLSListenerForTLSPlan(t *testing.T) {
	serveErr := errors.New("bind failed")
	starter := &fakeHTTPServerStarter{tlsErr: serveErr}
	var stopped []error

	ServePreparedHTTPServer(starter, HTTPServerPlan{Enabled: true, RequiresTLS: true, Listen: "0.0.0.0:9000", CertFile: "api.crt", KeyFile: "api.key"}, func(string) {}, func(err error) {
		stopped = append(stopped, err)
	})

	if starter.plainCalls != 0 || starter.tlsCalls != 1 {
		t.Fatalf("expected one TLS serve call and no plain calls, got plain=%d tls=%d", starter.plainCalls, starter.tlsCalls)
	}
	if starter.certFile != "api.crt" || starter.keyFile != "api.key" {
		t.Fatalf("expected TLS cert/key from plan, got cert=%q key=%q", starter.certFile, starter.keyFile)
	}
	if len(stopped) != 1 || !errors.Is(stopped[0], serveErr) {
		t.Fatalf("expected non-close serve error to be reported, got %v", stopped)
	}
}

func TestServePreparedHTTPServerSkipsDisabledPlan(t *testing.T) {
	starter := &fakeHTTPServerStarter{}
	ServePreparedHTTPServer(starter, HTTPServerPlan{}, func(string) {
		t.Fatalf("disabled plan should not log listening")
	}, func(error) {
		t.Fatalf("disabled plan should not report stopped")
	})
	if starter.plainCalls != 0 || starter.tlsCalls != 0 {
		t.Fatalf("disabled plan should not start serving, got plain=%d tls=%d", starter.plainCalls, starter.tlsCalls)
	}
}
