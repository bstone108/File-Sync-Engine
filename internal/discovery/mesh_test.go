package discovery

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"

	"filesyncengine/internal/config"
	"filesyncengine/internal/nat"
	ma "github.com/multiformats/go-multiaddr"
)

func TestSameIdentityACLRejectsStrangersUntilPublic(t *testing.T) {
	mesh.mu.Lock()
	mesh.helperEnabled = true
	mesh.configMembers = map[string]struct{}{}
	mesh.connectionMembers = map[string]struct{}{}
	mesh.mu.Unlock()
	mesh.portExposed.Store(false)
	mesh.autonatPublic.Store(false)
	id, err := peer.Decode("12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU")
	if err != nil {
		t.Fatal(err)
	}
	acl := sameIdentityACL{}
	if acl.AllowReserve(id, nil) {
		t.Fatal("private helper accepted a reservation")
	}
	SetIdentityMembers([]string{id.String()})
	NotePortExposed()
	if !acl.AllowReserve(id, nil) {
		t.Fatal("public helper rejected a same-identity peer")
	}
	other, err := peer.Decode("12D3KooWPjceQrSwdWXPyLLeABRXmuqt69Rg3sBYbU1Nft9HyQ6X")
	if err != nil {
		t.Fatal(err)
	}
	if acl.AllowConnect(id, nil, other) {
		t.Fatal("helper allowed a circuit to a peer outside the identity")
	}
	SetConnectionMembers([]string{other.String()})
	if !acl.AllowConnect(id, ma.StringCast("/ip4/203.0.113.9/tcp/22421"), other) {
		t.Fatal("helper rejected two same-identity peers")
	}
	mesh.mu.Lock()
	mesh.helperEnabled = false
	mesh.mu.Unlock()
	if acl.AllowReserve(id, nil) {
		t.Fatal("disabled helper still reserved a slot")
	}
	mesh.mu.Lock()
	mesh.helperEnabled = true
	mesh.mu.Unlock()
}

func TestRefreshIdentityMembersUsesEnabledGroupsOnly(t *testing.T) {
	cfg := config.Config{
		Identity: config.IdentityConfig{Groups: []config.IdentityGroupConfig{
			{ID: "family", Enabled: true, Token: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
			{ID: "old", Enabled: false},
		}},
		Peers: []config.PeerConfig{
			{ID: "kiki", IdentityGroupID: "family", DHTPeerID: "12D3KooWfamily"},
			{ID: "guest", IdentityGroupID: "old", DHTPeerID: "12D3KooWguest"},
			{ID: "code", DHTPeerID: "12D3KooWcode"},
		},
	}
	RefreshIdentityMembers(cfg)
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	if _, ok := mesh.configMembers["12D3KooWfamily"]; !ok {
		t.Fatalf("members = %#v", mesh.configMembers)
	}
	if _, ok := mesh.configMembers["12D3KooWguest"]; ok {
		t.Fatal("disabled group was allowed")
	}
	if _, ok := mesh.configMembers["12D3KooWcode"]; ok {
		t.Fatal("code-paired peer was treated as same identity")
	}
}

func TestHolePunchUpgradesRelayPath(t *testing.T) {
	nat.DefaultPaths.Reset()
	id, err := peer.Decode("12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU")
	if err != nil {
		t.Fatal(err)
	}
	nat.DefaultPaths.Observe(id.String(), "/p2p-circuit", nat.PathRelay)
	(holepunchTracer{}).Trace(&holepunch.Event{
		Timestamp: time.Now().UnixNano(),
		Remote:    id,
		Type:      holepunch.EndHolePunchEvtT,
		Evt:       holepunch.EndHolePunchEvt{Success: true},
	})
	obs, ok := nat.DefaultPaths.Active(id.String())
	if !ok || obs.Path != nat.PathHolePunched {
		t.Fatalf("path = %+v", obs)
	}
}

func TestParseObservation(t *testing.T) {
	observed, relayAddr := ParseObservation("observed /ip4/203.0.113.8/tcp/42421\nrelay /ip4/198.51.100.4/tcp/22421/p2p/12D3KooWhelper\n")
	if observed != "/ip4/203.0.113.8/tcp/42421" || relayAddr != "/ip4/198.51.100.4/tcp/22421/p2p/12D3KooWhelper" {
		t.Fatalf("observed=%q relay=%q", observed, relayAddr)
	}
}

func TestHelperRelayHasNoDataLimit(t *testing.T) {
	resources := helperResources()
	if resources.Limit != nil {
		t.Fatalf("helper limit = %+v", resources.Limit)
	}
	if resources.MaxCircuits != 4 || resources.MaxReservations < 1 {
		t.Fatalf("caps = %+v", resources)
	}
}

func TestUnlimitedRelaysSkipDefaultBootstrap(t *testing.T) {
	mesh.mu.Lock()
	prevPublic := append([]string(nil), mesh.publicRelays...)
	prevIdentity := append([]string(nil), mesh.identityRelays...)
	mesh.publicRelays = nil
	mesh.identityRelays = []string{"/ip4/198.51.100.8/tcp/22421/p2p/12D3KooWhelper"}
	mesh.mu.Unlock()
	t.Cleanup(func() {
		mesh.mu.Lock()
		mesh.publicRelays = prevPublic
		mesh.identityRelays = prevIdentity
		mesh.mu.Unlock()
	})
	got := UnlimitedRelayAddrs()
	if len(got) != 1 || got[0] != "/ip4/198.51.100.8/tcp/22421/p2p/12D3KooWhelper" {
		t.Fatalf("unlimited = %#v", got)
	}
	ordered := OrderedRelays()
	if len(ordered) < 2 || ordered[0] != got[0] {
		t.Fatalf("ordered = %#v", ordered)
	}
}

func TestOrderedRelaysPreferIdentityHelpers(t *testing.T) {
	mesh.mu.Lock()
	mesh.publicRelays = []string{"/dnsaddr/bootstrap.libp2p.io"}
	mesh.identityRelays = []string{"/ip4/198.51.100.8/tcp/22421/p2p/12D3KooWhelper"}
	mesh.mu.Unlock()
	got := OrderedRelays()
	if len(got) != 2 || got[0] != "/ip4/198.51.100.8/tcp/22421/p2p/12D3KooWhelper" {
		t.Fatalf("relays = %#v", got)
	}
}

func TestSelfEmittedReachabilityDoesNotArmHelper(t *testing.T) {
	mesh.autonatPublic.Store(false)
	mesh.portExposed.Store(false)
	markSelfReachabilityEmit()
	noteReachability(true)
	if mesh.public() {
		t.Fatal("our own reachability emit armed the helper")
	}
	noteReachability(true)
	if !mesh.autonatPublic.Load() {
		t.Fatal("AutoNAT public reachability did not arm the helper")
	}
}
