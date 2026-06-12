package discoverycontrol

import (
	"errors"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/routing"
)

type staticSource struct {
	peers []discovery.Peer
	err   error
}

func (s staticSource) Peers() ([]discovery.Peer, error) {
	return s.peers, s.err
}

func TestPollSourcesDedupesPeersAndReportsSourceErrors(t *testing.T) {
	peers, events := PollSources([]discovery.Source{
		staticSource{peers: []discovery.Peer{{ID: "peer-a", Addresses: []string{"http://a"}}, {ID: "peer-a", Addresses: []string{"http://duplicate"}}, {ID: ""}}},
		staticSource{err: errors.New("dht unavailable")},
		staticSource{peers: []discovery.Peer{{ID: "peer-b", Addresses: []string{"tcp://b:22000"}}}},
	})

	if len(peers) != 2 || peers[0].ID != "peer-a" || peers[1].ID != "peer-b" {
		t.Fatalf("unexpected peers: %#v", peers)
	}
	if len(events) != 1 || events[0].Type != "discovery.error" || events[0].Message != "dht unavailable" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestMergeDiscoveredPeersPreservesConfiguredPeersAndPublishesNewPeerEvents(t *testing.T) {
	state := api.State{Peers: 1, PeersState: []api.PeerState{{ID: "configured", Status: "configured", Endpoint: "manual:http://configured"}}}

	updated, events := MergeDiscoveredPeers(state, []discovery.Peer{
		{ID: "configured", Addresses: []string{"http://replacement"}},
		{ID: "new-peer", Addresses: []string{"http://new"}},
	})

	if len(updated.PeersState) != 2 {
		t.Fatalf("expected configured plus discovered peer, got %#v", updated.PeersState)
	}
	if updated.PeersState[0].Endpoint != "manual:http://configured" {
		t.Fatalf("configured peer was replaced: %#v", updated.PeersState[0])
	}
	if updated.Peers != 2 || updated.PeersState[1].ID != "new-peer" || updated.PeersState[1].Status != "discovered" || updated.PeersState[1].Endpoint != "discovered:http://new" {
		t.Fatalf("unexpected discovered peer state: %#v", updated.PeersState[1])
	}
	if len(events) != 1 || events[0].Type != "peer.discovered" || events[0].PeerID != "new-peer" || events[0].Message != "discovered:http://new" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestStreamTCPAddressAcceptsSchemeAndBareHostPort(t *testing.T) {
	if address, ok := StreamTCPAddress("tcp://127.0.0.1:22000"); !ok || address != "127.0.0.1:22000" {
		t.Fatalf("expected tcp scheme address, got address=%q ok=%v", address, ok)
	}
	if address, ok := StreamTCPAddress("127.0.0.1:22000"); !ok || address != "127.0.0.1:22000" {
		t.Fatalf("expected bare host:port address, got address=%q ok=%v", address, ok)
	}
	for _, rejected := range []string{"relay://peer", "/tmp/socket", "not-a-tcp-address"} {
		if address, ok := StreamTCPAddress(rejected); ok {
			t.Fatalf("expected %q to be rejected, got %q", rejected, address)
		}
	}
}

func TestEndpointObservationsFromDiscoveredPeersKeepsOnlyLiveSidecarAddresses(t *testing.T) {
	observations := EndpointObservationsFromDiscoveredPeers([]discovery.Peer{
		{ID: "peer-a", Addresses: []string{"http://10.0.0.2:8080", "relay://ignore", "tcp://10.0.0.2:22000", "http://10.0.0.2:8080"}},
		{ID: ""},
	}, routing.NetworkHints{LocalCIDRs: []string{"10.0.0.0/24"}})

	if len(observations) != 2 {
		t.Fatalf("expected HTTP and TCP sidecar observations, got %#v", observations)
	}
	for _, observation := range observations {
		if observation.PeerID != "peer-a" || !observation.Reachable || observation.Path != routing.DirectPath || observation.NetworkHint != string(routing.LocalNetwork) {
			t.Fatalf("unexpected observation: %#v", observation)
		}
	}
}

type recordingPollState struct {
	state   api.State
	events  []api.Event
	updates []api.State
}

func (r *recordingPollState) CurrentState() api.State {
	return r.state
}

func (r *recordingPollState) UpdateState(state api.State) {
	r.state = state
	r.updates = append(r.updates, state)
}

func (r *recordingPollState) Publish(event api.Event) {
	r.events = append(r.events, event)
}

func TestProcessPollPublishesSourceAndPeerEventsAndReturnsSidecarObservations(t *testing.T) {
	recorder := &recordingPollState{state: api.State{Peers: 1, PeersState: []api.PeerState{{ID: "configured", Status: "configured", Endpoint: "manual:http://configured"}}}}

	observations := ProcessPoll(recorder, []discovery.Source{
		staticSource{err: errors.New("lan down")},
		staticSource{peers: []discovery.Peer{{ID: "configured", Addresses: []string{"http://ignored"}}, {ID: "new-peer", Addresses: []string{"http://10.0.0.3:8080", "relay://not-data"}}}},
	}, routing.NetworkHints{LocalCIDRs: []string{"10.0.0.0/24"}})

	if len(recorder.updates) != 1 {
		t.Fatalf("expected one API state update, got %d", len(recorder.updates))
	}
	if recorder.state.Peers != 2 || recorder.state.PeersState[1].ID != "new-peer" || recorder.state.PeersState[1].Endpoint != "discovered:http://10.0.0.3:8080" {
		t.Fatalf("unexpected updated state: %#v", recorder.state)
	}
	if len(recorder.events) != 2 || recorder.events[0].Type != "discovery.error" || recorder.events[1].Type != "peer.discovered" || recorder.events[1].PeerID != "new-peer" {
		t.Fatalf("unexpected published events: %#v", recorder.events)
	}
	if len(observations) != 2 || observations[0].PeerID != "configured" || observations[0].Address != "http://ignored" || observations[1].PeerID != "new-peer" || observations[1].Address != "http://10.0.0.3:8080" || observations[1].NetworkHint != string(routing.LocalNetwork) {
		t.Fatalf("unexpected observations: %#v", observations)
	}
}
