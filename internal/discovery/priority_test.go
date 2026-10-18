package discovery

import (
	"testing"

	"filesyncengine/internal/config"
)

func TestNeedsDHTBackupOnlyAsFallback(t *testing.T) {
	t.Cleanup(ResetCatalog)
	manual := []config.PeerConfig{{ID: "kiki", Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://192.168.1.20:22420"}}}}
	if ok, reason := NeedsDHTBackup(manual, nil, nil); ok || reason != "" {
		t.Fatalf("saved profile should not use DHT: %v %q", ok, reason)
	}
	if ok, reason := NeedsDHTBackup(nil, nil, nil); !ok || reason != "no-peers" {
		t.Fatalf("empty engine = %v %q", ok, reason)
	}
	bare := []config.PeerConfig{{ID: "neko"}}
	if ok, reason := NeedsDHTBackup(bare, nil, nil); !ok || reason != "no-connection-details" {
		t.Fatalf("peer without details = %v %q", ok, reason)
	}
	NoteExchangedPeers([]Peer{{ID: "neko", Addresses: []string{"https://100.64.1.4:22420"}}})
	if ok, reason := NeedsDHTBackup(bare, ExchangedPeers(), nil); ok {
		t.Fatalf("exchanged address should satisfy lookup: %v %q", ok, reason)
	}
	NotePeerUnreachable("kiki")
	if ok, reason := NeedsDHTBackup(manual, nil, UnreachableIDs()); !ok || reason != "peer-unreachable" {
		t.Fatalf("unreachable peer = %v %q", ok, reason)
	}
}

func TestAddressOrderIsExchangeThenLANThenDHT(t *testing.T) {
	t.Cleanup(ResetCatalog)
	NoteExchangedPeers([]Peer{{ID: "kiki", Addresses: []string{"https://helper.example:22420"}}})
	NoteLocalPeers([]Peer{{ID: "kiki", Addresses: []string{"https://192.168.1.20:22420"}}})
	NoteDHTPeers([]Peer{{ID: "kiki", Addresses: []string{"https://203.0.113.9:22420"}}})
	exchanged, local, dht := AddressesFor("kiki")
	if len(exchanged) != 1 || exchanged[0] != "https://helper.example:22420" {
		t.Fatalf("exchanged = %#v", exchanged)
	}
	if len(local) != 1 || local[0] != "https://192.168.1.20:22420" {
		t.Fatalf("local = %#v", local)
	}
	if len(dht) != 1 || dht[0] != "https://203.0.113.9:22420" {
		t.Fatalf("dht = %#v", dht)
	}
}

func TestGatedSourceSkipsRoutineDHT(t *testing.T) {
	called := 0
	source := gatedSource{
		inner: StaticSource{peers: []Peer{{ID: "dht-peer", Addresses: []string{"/ip4/203.0.113.9/tcp/4001"}}}},
		allow: func() (bool, string) { return false, "" },
	}
	peers, err := source.Peers()
	if err != nil || len(peers) != 0 || called != 0 {
		t.Fatalf("peers=%+v err=%v", peers, err)
	}
	source.allow = func() (bool, string) {
		called++
		return true, "no-peers"
	}
	peers, err = source.Peers()
	if err != nil || len(peers) != 1 || called != 1 {
		t.Fatalf("backup peers=%+v called=%d err=%v", peers, called, err)
	}
}
