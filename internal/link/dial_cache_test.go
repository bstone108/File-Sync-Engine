package link

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"filesyncengine/internal/config"
)

func TestDialReusesTheConnectionForTheNextSmallFile(t *testing.T) {
	resetDialCache()
	t.Cleanup(resetDialCache)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepts atomic.Int32
	counting := &countListener{Listener: ln, accepts: &accepts}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}
	defer server.Close()
	go func() { _ = server.Serve(counting) }()

	peer := config.PeerConfig{
		ID:     "keep-alive",
		APIKey: "secret",
		Endpoints: []config.EndpointConfig{{
			Kind: "manual", Address: "http://" + ln.Addr().String(), NetworkHint: "local",
		}},
	}
	fetch := func() {
		t.Helper()
		_, err := Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/folder-block", nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	fetch()
	fetch()
	if got := accepts.Load(); got != 1 {
		t.Fatalf("TCP accepts = %d, want 1", got)
	}
}

type countListener struct {
	net.Listener
	accepts *atomic.Int32
}

func (l *countListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.accepts.Add(1)
	}
	return conn, err
}
