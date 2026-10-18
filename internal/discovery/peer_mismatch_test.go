package discovery

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func TestPeerIDMismatchIsRejectedBeforeAppData(t *testing.T) {
	dialer := newTestHost(t, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	stranger := newTestHost(t, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	var streams atomic.Int32
	var bytes atomic.Int32
	stranger.SetStreamHandler(protocol.ID(APIProtocol), func(stream network.Stream) {
		streams.Add(1)
		buf, _ := io.ReadAll(stream)
		bytes.Add(int32(len(buf)))
		_ = stream.Reset()
	})
	SetAPIHost(dialer)
	t.Cleanup(func() { SetAPIHost(nil) })

	addr := ""
	for _, candidate := range stranger.Addrs() {
		if candidate != nil && candidate.String() != "" {
			addr = candidate.String()
			break
		}
	}
	if addr == "" {
		t.Fatal("stranger has no listen address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := DialStream(ctx, dialer.ID().String(), []string{addr})
	if err == nil {
		t.Fatal("peer id mismatch was accepted")
	}
	if streams.Load() != 0 || bytes.Load() != 0 {
		t.Fatalf("app data reached the stranger: streams=%d bytes=%d", streams.Load(), bytes.Load())
	}
}
