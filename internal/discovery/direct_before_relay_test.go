package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
)

func TestDialStreamTriesDirectBeforeRelayHop(t *testing.T) {
	dialer := newTestHost(t, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	target := newTestHost(t, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	SetAPIHost(dialer)
	t.Cleanup(func() { SetAPIHost(nil) })

	direct := ""
	for _, addr := range target.Addrs() {
		if addr != nil && addr.String() != "" {
			direct = addr.String()
			break
		}
	}
	if direct == "" {
		t.Fatal("target has no listen address")
	}
	blackhole, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blackhole.Close()
	go func() {
		for {
			conn, acceptErr := blackhole.Accept()
			if acceptErr != nil {
				return
			}
			time.Sleep(30 * time.Second)
			_ = conn.Close()
		}
	}()
	_, port, err := net.SplitHostPort(blackhole.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	const relayID = "12D3KooWPjceQrSwdWXPyLLeABRXmuqt69Rg3sBYbU1Nft9HyQ6X"
	circuit := "/ip4/127.0.0.1/tcp/" + port + "/p2p/" + relayID + "/p2p-circuit/p2p/" + target.ID().String()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	session, err := DialStream(ctx, target.ID().String(), []string{circuit, direct})
	if err != nil {
		t.Fatal(err)
	}
	if session.Client == nil {
		t.Fatal("dial did not return a stream client")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("direct dial waited %s behind the relay hop", elapsed)
	}
}
