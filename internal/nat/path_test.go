package nat

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestClassifyPaths(t *testing.T) {
	cases := []struct {
		addr string
		want Path
	}{
		{"/ip4/192.168.1.20/tcp/22421", PathLAN},
		{"https://10.1.1.5:22420", PathLAN},
		{"tcp://100.64.1.8:22420", PathTailscale},
		{"/ip4/203.0.113.9/tcp/22421", PathDirect},
		{"/ip4/203.0.113.9/tcp/4001/p2p/QmRelay/p2p-circuit", PathRelay},
		{"relay://helper/peer", PathRelay},
		{"http://172.17.0.4:22420", PathLAN},
	}
	for _, tc := range cases {
		if got := Classify(tc.addr); got != tc.want {
			t.Fatalf("Classify(%q) = %s, want %s", tc.addr, got, tc.want)
		}
	}
}

func TestRaceUsesRelayThenUpgrades(t *testing.T) {
	relay := Candidate{Address: "relay://helper/peer", Path: PathRelay}
	direct := Candidate{Address: "203.0.113.9:22421", Path: PathDirect}
	started := make(chan struct{}, 2)
	addr, path, err := Race(context.Background(), []Candidate{relay, direct}, func(_ context.Context, address string) error {
		started <- struct{}{}
		if address == relay.Address {
			return nil
		}
		time.Sleep(40 * time.Millisecond)
		return nil
	}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if addr != direct.Address || path != PathDirect {
		t.Fatalf("winner = %s %s, want direct", addr, path)
	}
	if len(started) != 2 {
		t.Fatalf("started = %d, want both paths", len(started))
	}
}

func TestRaceKeepsRelayWhenDirectFails(t *testing.T) {
	addr, path, err := Race(context.Background(), []Candidate{
		{Address: "203.0.113.9:22421", Path: PathDirect},
		{Address: "/ip4/198.51.100.2/tcp/4001/p2p/QmRelay/p2p-circuit", Path: PathRelay},
	}, func(_ context.Context, address string) error {
		if Classify(address) == PathDirect {
			return errors.New("connect: connection refused")
		}
		return nil
	}, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if path != PathRelay || addr == "" {
		t.Fatalf("winner = %s %s", addr, path)
	}
}

func TestPreferRelaysPutsHelpersFirst(t *testing.T) {
	got := PreferRelays(
		[]string{"/ip4/198.51.100.8/tcp/22421/p2p/helper", "/ip4/198.51.100.8/tcp/22421/p2p/helper"},
		[]string{"/dnsaddr/bootstrap.libp2p.io", "/ip4/198.51.100.8/tcp/22421/p2p/helper"},
	)
	if len(got) != 2 || got[0] != "/ip4/198.51.100.8/tcp/22421/p2p/helper" || got[1] != "/dnsaddr/bootstrap.libp2p.io" {
		t.Fatalf("order = %#v", got)
	}
}

func TestTailscaleRange(t *testing.T) {
	if !isTailscale(net.ParseIP("100.100.1.2")) {
		t.Fatal("tailscale CGNAT was not classified")
	}
	if isTailscale(net.ParseIP("100.63.1.2")) {
		t.Fatal("address below the CGNAT range was classified as tailscale")
	}
}
