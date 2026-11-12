package discovery

import (
	"reflect"
	"testing"
)

func TestOwnCoordinatorBeatsPublicInfrastructure(t *testing.T) {
	nodes := []MeshNode{
		{ID: "a", Links: []string{"d"}},
		{ID: "d", Public: true, Addrs: []string{"/ip4/198.51.100.8/tcp/22421/p2p/d"}, Links: []string{"b"}},
		{ID: "b", Links: []string{"d"}},
	}
	public := []Coordinator{
		{ID: "stun.l.google.com:19302", Role: RolePublicSTUN, Addrs: []string{"stun.l.google.com:19302"}},
		{ID: "bootstrap", Role: RolePublicDHT, Addrs: []string{"/dnsaddr/bootstrap.libp2p.io"}},
	}
	got, ok := SelectPunchCoordinator("a", "b", nodes, public)
	if !ok || got.Role != RoleOwnPublic || got.ID != "d" {
		t.Fatalf("coordinator = %+v ok=%v", got, ok)
	}
	if PublicCoordination(got, ok) {
		t.Fatal("public infrastructure was selected while an own engine can coordinate")
	}
	if !reflect.DeepEqual(got.Path, []string{"d", "b"}) {
		t.Fatalf("path = %#v", got.Path)
	}
}

func TestMeshBounceReachesOwnPublicEngine(t *testing.T) {
	// a and b are both NATed. c is our private peer. d is our public engine.
	// a cannot dial d directly, so the punch is arranged through c.
	nodes := []MeshNode{
		{ID: "a", Links: []string{"c"}},
		{ID: "c", Links: []string{"d"}},
		{ID: "d", Public: true, Addrs: []string{"/ip4/198.51.100.8/tcp/22421/p2p/d"}, Links: []string{"b"}},
		{ID: "b"},
	}
	public := []Coordinator{{ID: "stun", Role: RolePublicSTUN, Addrs: []string{"stun.l.google.com:19302"}}}
	got, ok := SelectPunchCoordinator("a", "b", nodes, public)
	if !ok || got.Role != RoleOwnMesh || got.ID != "d" {
		t.Fatalf("coordinator = %+v ok=%v", got, ok)
	}
	if !reflect.DeepEqual(got.Path, []string{"c", "d", "b"}) {
		t.Fatalf("path = %#v", got.Path)
	}
	if PublicCoordination(got, ok) {
		t.Fatal("mesh bounce fell through to public STUN")
	}
}

func TestOwnMeshWithoutPublicEngineSkipsPublicNetworks(t *testing.T) {
	nodes := []MeshNode{
		{ID: "a", Links: []string{"c"}},
		{ID: "c", Links: []string{"b"}},
		{ID: "b"},
	}
	public := []Coordinator{{ID: "stun", Role: RolePublicSTUN}}
	got, ok := SelectPunchCoordinator("a", "b", nodes, public)
	if !ok || got.Role != RoleDirect || got.ID != "b" {
		t.Fatalf("coordinator = %+v ok=%v", got, ok)
	}
	if PublicCoordination(got, ok) {
		t.Fatal("a chain of our peers still selected public infrastructure")
	}
}

func TestPublicFallbackOnlyWhenNoOwnEngine(t *testing.T) {
	nodes := []MeshNode{{ID: "a"}, {ID: "b"}}
	public := []Coordinator{
		{ID: "stun.l.google.com:19302", Role: RolePublicSTUN, Addrs: []string{"stun.l.google.com:19302"}},
		{ID: "bootstrap", Role: RolePublicDHT, Addrs: []string{"/dnsaddr/bootstrap.libp2p.io"}},
	}
	got, ok := SelectPunchCoordinator("a", "b", nodes, public)
	if !ok || got.Role != RolePublicSTUN || got.ID != "stun.l.google.com:19302" {
		t.Fatalf("coordinator = %+v ok=%v", got, ok)
	}
	if !PublicCoordination(got, ok) {
		t.Fatal("public fallback was not marked public")
	}
}

func TestDataRelaysPreferOwnRelay(t *testing.T) {
	own := []string{"/ip4/198.51.100.8/tcp/22421/p2p/helper"}
	public := []string{"/dnsaddr/bootstrap.libp2p.io"}
	got := DataRelays(own, public)
	if !reflect.DeepEqual(got, own) {
		t.Fatalf("own relays = %#v", got)
	}
	if got := DataRelays(nil, public); !reflect.DeepEqual(got, public) {
		t.Fatalf("public fallback = %#v", got)
	}
}
