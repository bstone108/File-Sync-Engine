package discovery

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/stun/v3"

	"filesyncengine/internal/advertise"
)

func TestSTUNCandidateAnnouncedForConeOnly(t *testing.T) {
	t.Cleanup(advertise.ResetNATStatus)
	advertise.SetNATStatus(advertise.NATStatus{
		Type: advertise.NATCone, IP: "1.2.3.4", Port: 44000, Mapped: "1.2.3.4:44000", Advertise: true,
	})
	joined := strings.Join(multiaddrStrings(announcedAddrs(nil)), " ")
	if !strings.Contains(joined, "/ip4/1.2.3.4/udp/44000/quic-v1") {
		t.Fatalf("cone candidate missing: %s", joined)
	}
	if strings.Contains(joined, "/ip4/1.2.3.4/tcp/44000") {
		t.Fatalf("STUN port was announced on TCP: %s", joined)
	}
	advertise.SetNATStatus(advertise.NATStatus{
		Type: advertise.NATSymmetric, IP: "1.2.3.4", Port: 44001, Mapped: "1.2.3.4:44001", Advertise: false,
	})
	joined = strings.Join(multiaddrStrings(announcedAddrs(nil)), " ")
	if strings.Contains(joined, "1.2.3.4") {
		t.Fatalf("symmetric mapping was announced: %s", joined)
	}
}

func TestProbeSTUNUsesLibp2pSocket(t *testing.T) {
	t.Cleanup(advertise.ResetNATStatus)
	var source atomic.Int32
	first := fakeSTUN(t, 45000, &source)
	second := fakeSTUN(t, 45000, &source)
	t.Setenv(advertise.EnvSTUNServers, first+","+second)
	port := freeUDPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{ListenPort: port, DHTMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	status, err := router.ProbeSTUNNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Type != advertise.NATCone || status.Mapped != "1.2.3.4:45000" || !status.Advertise {
		t.Fatalf("status = %+v", status)
	}
	if got := int(source.Load()); got != port {
		t.Fatalf("STUN source port = %d, libp2p port = %d", got, port)
	}
	joined := strings.Join(multiaddrStrings(announcedAddrs(nil)), " ")
	if !strings.Contains(joined, "/ip4/1.2.3.4/udp/45000/quic-v1") {
		t.Fatalf("announced = %s", joined)
	}
}

func TestProbeSTUNSymmetricIsNotACandidate(t *testing.T) {
	t.Cleanup(advertise.ResetNATStatus)
	var source atomic.Int32
	first := fakeSTUN(t, 45010, &source)
	second := fakeSTUN(t, 45011, &source)
	t.Setenv(advertise.EnvSTUNServers, first+","+second)
	port := freeUDPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{ListenPort: port, DHTMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	status, err := router.ProbeSTUNNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Type != advertise.NATSymmetric || status.Advertise {
		t.Fatalf("status = %+v", status)
	}
	if got := int(source.Load()); got != port {
		t.Fatalf("STUN source port = %d, libp2p port = %d", got, port)
	}
	joined := strings.Join(multiaddrStrings(announcedAddrs(nil)), " ")
	if strings.Contains(joined, "1.2.3.4") {
		t.Fatalf("symmetric mapping was announced: %s", joined)
	}
}

func fakeSTUN(t *testing.T, mappedPort int, source *atomic.Int32) string {
	t.Helper()
	packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = packet.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := packet.ReadFrom(buf)
			if err != nil {
				return
			}
			if udp, ok := addr.(*net.UDPAddr); ok {
				source.Store(int32(udp.Port))
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
	}()
	return packet.LocalAddr().String()
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := packet.LocalAddr().(*net.UDPAddr).Port
	if err := packet.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
