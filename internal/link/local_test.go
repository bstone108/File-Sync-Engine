package link

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestLocalDirectoryServesARecordToAnotherHost(t *testing.T) {
	publisher, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.Close() })
	looker, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = looker.Close() })
	looker.SetDialTargets([]string{publisher.Addr()})

	payload := []byte("sealed-pairing-record")
	if err := publisher.Publish(context.Background(), "pair-namespace", payload); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := looker.Lookup(ctx, "pair-namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0]) != string(payload) {
		t.Fatalf("lookup = %q", got)
	}
	if err := publisher.Unpublish(context.Background(), "pair-namespace"); err != nil {
		t.Fatal(err)
	}
	got, err = looker.Lookup(ctx, "pair-namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unpublished lookup = %q", got)
	}
}

func TestHostsInNetSkipsLargeAndTinyNetworks(t *testing.T) {
	wide := mustNet(t, "172.17.0.8/16")
	if hosts := hostsInNet(wide, LocalPairPort); len(hosts) != 0 {
		t.Fatalf("/16 scan = %d hosts", len(hosts))
	}
	lan := mustNet(t, "192.168.9.20/24")
	hosts := hostsInNet(lan, LocalPairPort)
	if len(hosts) != 253 {
		t.Fatalf("/24 scan = %d hosts, want 253", len(hosts))
	}
	for _, host := range hosts {
		if host == "192.168.9.20:22427" || host == "192.168.9.0:22427" || host == "192.168.9.255:22427" {
			t.Fatalf("scan included %s", host)
		}
	}
}

func TestLocalLookupUsesAUDPRecordWhenTCPMisses(t *testing.T) {
	looker, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = looker.Close() })
	slow, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slow.Close() })
	go func() {
		conn, acceptErr := slow.Accept()
		if acceptErr != nil {
			return
		}
		time.Sleep(400 * time.Millisecond)
		_ = conn.Close()
	}()
	looker.SetDialTargets([]string{slow.Addr().String()})
	payload := []byte("sealed-pairing-record-from-udp")
	body, err := json.Marshal(localPairMessage{Type: localPairType, Op: "record", Namespace: "pair-namespace", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", looker.Addr())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		conn, dialErr := net.DialUDP("udp", nil, udpAddr)
		if dialErr != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write(body)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := looker.Lookup(ctx, "pair-namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0]) != string(payload) {
		t.Fatalf("lookup = %q", got)
	}
}

func TestLocalLookupFetchesALargeRecordOverTCP(t *testing.T) {
	publisher, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.Close() })
	looker, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = looker.Close() })
	looker.SetDialTargets([]string{publisher.Addr()})
	payload := make([]byte, 2000)
	for i := range payload {
		payload[i] = byte(i)
	}
	if err := publisher.Publish(context.Background(), "pair-namespace", payload); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := looker.Lookup(ctx, "pair-namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0]) != string(payload) {
		t.Fatalf("lookup length = %d", len(got))
	}
}

func mustNet(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	network.IP = ip
	return network
}
