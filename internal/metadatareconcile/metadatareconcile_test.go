package metadatareconcile

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/state"
)

var errTestDial = errors.New("dial failed")

func TestPeerStreamEndpointCandidatesMergesManualSidecarAndFiltersNonStreamEndpoints(t *testing.T) {
	peer := config.PeerConfig{
		ID: "peer-a",
		Endpoints: []config.EndpointConfig{
			{Kind: "manual", Address: "tcp://wan.example:22000", NetworkHint: "wan"},
			{Kind: "relay", Address: "tcp://relay.example:22000"},
			{Kind: "manual", Address: "http://not-stream.example:8080"},
			{Kind: "unknown", Address: "tcp://ignored.example:22000"},
		},
	}
	observations := []routing.EndpointObservation{
		{PeerID: "peer-a", Address: "tcp://sidecar.local:22000", Reachable: true, NetworkHint: "local"},
		{PeerID: "peer-a", Address: "http://ignored.local:8080", Reachable: true},
		{PeerID: "other", Address: "tcp://other.local:22000", Reachable: true},
	}

	candidates := PeerStreamEndpointCandidates(peer, observations, routing.NetworkHints{})

	if len(candidates) != 3 {
		t.Fatalf("candidate count = %d, want 3: %+v", len(candidates), candidates)
	}
	if candidates[0].Address != "tcp://sidecar.local:22000" || candidates[0].Path != routing.DirectPath || candidates[0].Network != routing.LocalNetwork {
		t.Fatalf("sidecar observation should be preferred as local direct stream candidate: %+v", candidates[0])
	}
	if candidates[1].Address != "tcp://wan.example:22000" || candidates[1].Path != routing.DirectPath || candidates[1].Network != routing.WANNetwork {
		t.Fatalf("manual stream candidate not preserved: %+v", candidates[1])
	}
	if candidates[2].Address != "tcp://relay.example:22000" || candidates[2].Path != routing.RelayPath {
		t.Fatalf("relay stream candidate not preserved as relay fallback: %+v", candidates[2])
	}
}

func TestProcessCatchupSkipsMissingDisabledAndPathlessFolders(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveManifest("enabled", "local.txt", block.Manifest{Path: "local.txt", Size: 1, Blocks: []block.Block{{Offset: 0, Size: 1, Hash: []byte("local")}}}); err != nil {
		t.Fatalf("SaveManifest enabled: %v", err)
	}
	if err := store.SaveManifest("disabled", "local.txt", block.Manifest{Path: "local.txt", Size: 1, Blocks: []block.Block{{Offset: 0, Size: 1, Hash: []byte("local")}}}); err != nil {
		t.Fatalf("SaveManifest disabled: %v", err)
	}
	if err := store.SaveManifest("pathless", "local.txt", block.Manifest{Path: "local.txt", Size: 1, Blocks: []block.Block{{Offset: 0, Size: 1, Hash: []byte("local")}}}); err != nil {
		t.Fatalf("SaveManifest pathless: %v", err)
	}
	if err := store.SavePeerFolderState("peer-a", state.FolderSummary{FolderID: "enabled", Cursor: 0, StateHash: "behind"}); err != nil {
		t.Fatalf("SavePeerFolderState enabled: %v", err)
	}
	if err := store.SavePeerFolderState("peer-a", state.FolderSummary{FolderID: "disabled", Cursor: 0, StateHash: "behind"}); err != nil {
		t.Fatalf("SavePeerFolderState disabled: %v", err)
	}
	if err := store.SavePeerFolderState("peer-a", state.FolderSummary{FolderID: "pathless", Cursor: 0, StateHash: "behind"}); err != nil {
		t.Fatalf("SavePeerFolderState pathless: %v", err)
	}

	calls := 0
	result := ProcessCatchup(context.Background(), CatchupOptions{
		Publisher: api.NewServer(api.State{}, ""),
		Config: config.Config{
			Peers: []config.PeerConfig{{ID: "peer-a"}},
			Folders: []config.FolderConfig{
				{ID: "enabled", Enabled: true, Path: t.TempDir()},
				{ID: "disabled", Enabled: false, Path: t.TempDir()},
				{ID: "pathless", Enabled: true},
			},
		},
		Store: store,
		Dial: func(context.Context, config.PeerConfig, config.FolderConfig) (io.ReadWriteCloser, error) {
			calls++
			return nil, errTestDial
		},
	})

	if calls != 1 {
		t.Fatalf("dial calls = %d, want only enabled folder dialed", calls)
	}
	if result.Started != 0 || result.Completed != 0 || result.Failed != 1 {
		t.Fatalf("result = %+v, want one failed attempted catch-up", result)
	}
}

func TestRuntimeDialerUsesFirstReachableTCPStreamCandidate(t *testing.T) {
	var dialedNetwork string
	var dialedAddress string
	dialer := RuntimeDialer(RuntimeDialerOptions{
		EndpointObservations: []routing.EndpointObservation{{PeerID: "peer-a", Address: "tcp://sidecar.local:22000", Reachable: true, NetworkHint: "local"}},
		NetworkHints:         routing.NetworkHints{},
		DialTCP: func(ctx context.Context, network string, address string) (io.ReadWriteCloser, error) {
			dialedNetwork = network
			dialedAddress = address
			return testReadWriteCloser{}, nil
		},
	})

	stream, err := dialer(context.Background(), config.PeerConfig{ID: "peer-a", Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "tcp://wan.example:22000", NetworkHint: "wan"}}}, config.FolderConfig{ID: "folder-a"})
	if err != nil {
		t.Fatalf("runtime dialer returned error: %v", err)
	}
	if stream == nil {
		t.Fatal("runtime dialer returned nil stream")
	}
	if dialedNetwork != "tcp" || dialedAddress != "sidecar.local:22000" {
		t.Fatalf("dialed (%q,%q), want tcp sidecar.local:22000", dialedNetwork, dialedAddress)
	}
}

func TestRuntimeDialerReportsPeerWithoutReachableTCPStreamEndpoint(t *testing.T) {
	dialer := RuntimeDialer(RuntimeDialerOptions{
		EndpointObservations: []routing.EndpointObservation{{PeerID: "peer-a", Address: "http://sidecar.local:8080", Reachable: true}},
		DialTCP: func(ctx context.Context, network string, address string) (io.ReadWriteCloser, error) {
			t.Fatalf("DialTCP called for non-stream endpoint %s", address)
			return nil, nil
		},
	})

	_, err := dialer(context.Background(), config.PeerConfig{ID: "peer-a", Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://wan.example:8080"}}}, config.FolderConfig{ID: "folder-a"})
	if err == nil || err.Error() != "peer peer-a has no tcp stream endpoint for metadata catch-up" {
		t.Fatalf("error = %v, want no tcp stream endpoint error", err)
	}
}

type testReadWriteCloser struct{}

func (testReadWriteCloser) Read([]byte) (int, error)    { return 0, io.EOF }
func (testReadWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (testReadWriteCloser) Close() error                { return nil }

func TestStreamEndpointPathClassifiesSupportedMetadataStreamEndpointKinds(t *testing.T) {
	cases := []struct {
		kind string
		path routing.PathKind
		ok   bool
	}{
		{kind: "manual", path: routing.DirectPath, ok: true},
		{kind: "vpn", path: routing.DirectPath, ok: true},
		{kind: "sidecar", path: routing.DirectPath, ok: true},
		{kind: "relay", path: routing.RelayPath, ok: true},
		{kind: "proxy", path: routing.RelayPath, ok: true},
		{kind: "http", ok: false},
	}
	for _, tc := range cases {
		path, ok := StreamEndpointPath(config.EndpointConfig{Kind: tc.kind})
		if ok != tc.ok || path != tc.path {
			t.Fatalf("StreamEndpointPath(%q) = (%q,%v), want (%q,%v)", tc.kind, path, ok, tc.path, tc.ok)
		}
	}
}
