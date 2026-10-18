package advertise

import (
	"bytes"
	"context"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/structuredlog"
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

func TestClassifySTUNConeSymmetricAndUnknown(t *testing.T) {
	cone := ClassifySTUN([]STUNSample{
		{Server: "a", IP: "1.2.3.4", Port: 44000},
		{Server: "b", IP: "1.2.3.4", Port: 44000},
	})
	if cone.Type != NATCone || !cone.Advertise || cone.Mapped != "1.2.3.4:44000" {
		t.Fatalf("cone = %+v", cone)
	}
	symmetric := ClassifySTUN([]STUNSample{
		{Server: "a", IP: "1.2.3.4", Port: 44000},
		{Server: "b", IP: "1.2.3.4", Port: 44001},
	})
	if symmetric.Type != NATSymmetric || symmetric.Advertise {
		t.Fatalf("symmetric = %+v", symmetric)
	}
	unknown := ClassifySTUN([]STUNSample{
		{Server: "a", IP: "1.2.3.4", Port: 44000},
		{Server: "b", Err: context.DeadlineExceeded},
	})
	if unknown.Type != NATUnknown || !unknown.Advertise || unknown.Mapped != "1.2.3.4:44000" {
		t.Fatalf("unknown = %+v", unknown)
	}
	private := ClassifySTUN([]STUNSample{{Server: "a", IP: "192.168.1.20", Port: 44000}})
	if private.Type != NATUnknown || private.Advertise {
		t.Fatalf("private = %+v", private)
	}
	if got := ClassifySTUN(nil); got.Type != NATUnknown || got.Advertise || got.Mapped != "" {
		t.Fatalf("empty = %+v", got)
	}
}

func TestObserveSTUNLogsMappedAddressAndNATType(t *testing.T) {
	t.Cleanup(ResetNATStatus)
	servers := startFakeSTUNServers(t, map[string]int{"a": 44000, "b": 44000})
	BindConfigPath("")
	t.Setenv(EnvSTUNServers, servers[0]+","+servers[1])
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	structuredlog.Reset()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		structuredlog.Reset()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status := ObserveSTUN(ctx, conn)
	if status.Type != NATCone || status.Mapped != "1.2.3.4:44000" || !status.Advertise {
		t.Fatalf("status = %+v", status)
	}
	body := buf.String()
	for _, want := range []string{"nat.stun.ok", "nat.stun.result", `"natType":"cone"`, "1.2.3.4:44000", servers[0]} {
		if !strings.Contains(body, want) {
			t.Fatalf("log missing %s: %s", want, body)
		}
	}
}

func startFakeSTUNServers(t *testing.T, mappedPorts map[string]int) []string {
	t.Helper()
	names := make([]string, 0, len(mappedPorts))
	for name := range mappedPorts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	var closers []func() error
	for _, name := range names {
		packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		closers = append(closers, packet.Close)
		port := mappedPorts[name]
		out = append(out, packet.LocalAddr().String())
		go serveFakeSTUN(packet, port)
	}
	t.Cleanup(func() {
		for _, closeFn := range closers {
			_ = closeFn()
		}
	})
	return out
}

func serveFakeSTUN(packet net.PacketConn, mappedPort int) {
	buf := make([]byte, 1500)
	for {
		n, addr, err := packet.ReadFrom(buf)
		if err != nil {
			return
		}
		req := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
		if err := req.Decode(); err != nil {
			continue
		}
		res := stun.New()
		res.Type = stun.BindingSuccess
		res.TransactionID = req.TransactionID
		mapped := stun.XORMappedAddress{IP: net.ParseIP("1.2.3.4"), Port: mappedPort}
		if err := mapped.AddTo(res); err != nil {
			continue
		}
		res.Encode()
		_, _ = packet.WriteTo(res.Raw, addr)
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
