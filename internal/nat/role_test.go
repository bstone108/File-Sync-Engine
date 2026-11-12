package nat

import "testing"

func TestClassifyUsesAutoNATSTUNAndVPN(t *testing.T) {
	overall, tcp, udp := ClassifyInbound(Evidence{STUN: "cone"})
	if overall != ClassCone || tcp != ClassCone || udp != ClassCone {
		t.Fatalf("cone = %s %s %s", overall, tcp, udp)
	}
	overall, tcp, udp = ClassifyInbound(Evidence{STUN: "symmetric"})
	if overall != ClassSymmetric || udp != ClassSymmetric {
		t.Fatalf("symmetric = %s %s", overall, udp)
	}
	overall, _, udp = ClassifyInbound(Evidence{VPN: true, STUN: "cone"})
	if overall != ClassNone || udp != ClassNone {
		t.Fatalf("vpn = %s udp %s", overall, udp)
	}
	overall, tcp, udp = ClassifyInbound(Evidence{VPN: true, AutoNATPublic: true, STUN: "symmetric"})
	if overall != ClassPublic || tcp != ClassPublic || udp != ClassPublic {
		t.Fatalf("autonat overrides vpn = %s %s %s", overall, tcp, udp)
	}
	overall, _, _ = ClassifyInbound(Evidence{Forced: "no-inbound", AutoNATPublic: true})
	if overall != ClassNone {
		t.Fatalf("forced = %s", overall)
	}
}

func TestTwoWayThenOneWayThenPoll(t *testing.T) {
	if !ShouldDialNow(ClassPublic, ClassNone, false) || DialMode(ClassPublic, ClassNone, false) != "two-way" {
		t.Fatal("both sides dial when nothing is up")
	}
	if ShouldDialNow(ClassPublic, ClassNone, true) || DialMode(ClassPublic, ClassNone, true) != "accept" {
		t.Fatal("reachable side accepts once the session is up")
	}
	if !ShouldDialNow(ClassNone, ClassCone, true) || DialMode(ClassNone, ClassCone, true) != "one-way" {
		t.Fatal("restricted side keeps the outbound session")
	}
	if DialMode(ClassNone, ClassCone, false) != "poll" {
		t.Fatal("a dropped session falls back to polling")
	}
	if !ShouldDialNow(ClassCone, ClassCone, true) || DialMode(ClassCone, ClassCone, true) != "two-way" {
		t.Fatal("equal cone peers both keep dialing")
	}
	if !SkipHolePunch(ClassNone, ClassCone) || !SkipHolePunch(ClassCone, ClassSymmetric) {
		t.Fatal("hole punch is skipped when either side cannot be reached")
	}
	if SkipHolePunch(ClassCone, ClassPublic) {
		t.Fatal("cone to public can still punch")
	}
}

func TestTunnelInterfaceNames(t *testing.T) {
	for _, name := range []string{"tun0", "wg0", "tailscale0", "utun3", "zt0", "nordlynx"} {
		if !TunnelInterface(name) {
			t.Fatalf("%s should be a tunnel", name)
		}
	}
	for _, name := range []string{"eth0", "wlan0", "en0", "br0"} {
		if TunnelInterface(name) {
			t.Fatalf("%s is not a tunnel", name)
		}
	}
}
