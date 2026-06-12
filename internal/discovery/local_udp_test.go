package discovery

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNewLocalUDPSourceDefaultsListenAndTarget(t *testing.T) {
	source, err := NewLocalUDPSource("127.0.0.1:0", "", LocalAnnouncement{NodeID: "peer-a", Addresses: []string{"tcp://127.0.0.1:22000"}}, time.Millisecond)
	if err != nil {
		t.Fatalf("NewLocalUDPSource: %v", err)
	}
	defer source.Close()
	if source.Target.String() != DefaultLocalDiscoveryAddress {
		t.Fatalf("target = %q, want %q", source.Target.String(), DefaultLocalDiscoveryAddress)
	}
	if source.Conn.LocalAddr().String() == "" {
		t.Fatalf("expected local socket address")
	}
}

func TestLocalUDPSourceDiscoversPeerAnnouncement(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket listener: %v", err)
	}
	defer listener.Close()

	sender, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket sender: %v", err)
	}
	defer sender.Close()

	source := LocalUDPSource{
		Conn:    listener,
		Target:  sender.LocalAddr(),
		Self:    LocalAnnouncement{NodeID: "peer-a", Addresses: []string{"tcp://127.0.0.1:22000"}},
		Timeout: 250 * time.Millisecond,
	}

	go func() {
		time.Sleep(25 * time.Millisecond)
		data, err := EncodeLocalAnnouncement(LocalAnnouncement{NodeID: "peer-b", Addresses: []string{"tcp://127.0.0.1:22001"}})
		if err != nil {
			return
		}
		_, _ = sender.WriteTo(data, listener.LocalAddr())
	}()

	peers, err := source.PeersContext(context.Background())
	if err != nil {
		t.Fatalf("PeersContext: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected one discovered peer, got %+v", peers)
	}
	if peers[0].ID != "peer-b" || len(peers[0].Addresses) != 1 || peers[0].Addresses[0] != "tcp://127.0.0.1:22001" {
		t.Fatalf("unexpected discovered peer: %+v", peers[0])
	}
}

func TestLocalUDPSourceIgnoresSelfAndDeduplicatesPeerAddresses(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket listener: %v", err)
	}
	defer listener.Close()

	sender, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket sender: %v", err)
	}
	defer sender.Close()

	source := LocalUDPSource{
		Conn:    listener,
		Target:  sender.LocalAddr(),
		Self:    LocalAnnouncement{NodeID: "peer-a", Addresses: []string{"tcp://127.0.0.1:22000"}},
		Timeout: 250 * time.Millisecond,
	}

	go func() {
		time.Sleep(25 * time.Millisecond)
		for _, announcement := range []LocalAnnouncement{
			{NodeID: "peer-a", Addresses: []string{"tcp://127.0.0.1:22000"}},
			{NodeID: "peer-b", Addresses: []string{"tcp://127.0.0.1:22001"}},
			{NodeID: "peer-b", Addresses: []string{"tcp://127.0.0.1:22001", "tcp://127.0.0.1:22002"}},
		} {
			data, err := EncodeLocalAnnouncement(announcement)
			if err != nil {
				return
			}
			_, _ = sender.WriteTo(data, listener.LocalAddr())
		}
	}()

	peers, err := source.PeersContext(context.Background())
	if err != nil {
		t.Fatalf("PeersContext: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected one discovered peer, got %+v", peers)
	}
	want := []string{"tcp://127.0.0.1:22001", "tcp://127.0.0.1:22002"}
	if peers[0].ID != "peer-b" || len(peers[0].Addresses) != len(want) {
		t.Fatalf("unexpected discovered peer: %+v", peers[0])
	}
	for i := range want {
		if peers[0].Addresses[i] != want[i] {
			t.Fatalf("address %d = %q, want %q (peer %+v)", i, peers[0].Addresses[i], want[i], peers[0])
		}
	}
}
