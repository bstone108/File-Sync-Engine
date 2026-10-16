package advertise

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func TestSTUNServersDefaultAndDisable(t *testing.T) {
	t.Setenv(EnvSTUNServers, "")
	SetSTUNServers(nil)
	got := STUNServers()
	if len(got) != 2 || got[0] != "stun.l.google.com:19302" || got[1] != "stun.cloudflare.com:3478" {
		t.Fatalf("defaults = %#v", got)
	}
	t.Setenv(EnvSTUNServers, "off")
	if len(STUNServers()) != 0 {
		t.Fatal("off did not disable STUN")
	}
	t.Setenv(EnvSTUNServers, "stun.example:3478, stun.other:19302")
	got = STUNServers()
	if len(got) != 2 || got[0] != "stun.example:3478" {
		t.Fatalf("override = %#v", got)
	}
}

func TestQuerySTUNReadsMappedAddress(t *testing.T) {
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		n, addr, err := packet.ReadFrom(buf)
		if err != nil {
			return
		}
		req := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
		if err := req.Decode(); err != nil {
			return
		}
		res := stun.New()
		res.Type = stun.BindingSuccess
		res.TransactionID = req.TransactionID
		mapped := stun.XORMappedAddress{IP: net.ParseIP("203.0.113.8"), Port: 42421}
		if err := mapped.AddTo(res); err != nil {
			return
		}
		res.Encode()
		_, _ = packet.WriteTo(res.Raw, addr)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ip, port, err := querySTUN(ctx, packet.LocalAddr().String())
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.8" || port != 42421 {
		t.Fatalf("mapped = %s:%d", ip, port)
	}
}
