package discovery

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"

	"filesyncengine/internal/nat"
	ma "github.com/multiformats/go-multiaddr"
)

func TestLimitedAPIAndPairingOptIn(t *testing.T) {
	if !AllowsLimitedAPI("/v1/sync-journal") {
		t.Fatal("sync-journal must be allowed on a limited relay")
	}
	for _, path := range []string{"/v1/folder-block", "/v1/folder-block-subhashes", "/v1/folder-batch", "/v1/folder-file", "/v1/status"} {
		if AllowsLimitedAPI(path) {
			t.Fatalf("%s was treated as control traffic", path)
		}
	}
	allowed, reason := network.GetAllowLimitedConn(pairingStreamContext(context.Background()))
	if !allowed || reason != "fse-pair" {
		t.Fatalf("pairing limited = %v %q", allowed, reason)
	}
	allowed, reason = network.GetAllowLimitedConn(apiStreamContext(context.Background(), "/v1/sync-journal"))
	if !allowed || reason != "fse-control" {
		t.Fatalf("control limited = %v %q", allowed, reason)
	}
	if allowed, _ := network.GetAllowLimitedConn(apiStreamContext(context.Background(), "/v1/folder-block")); allowed {
		t.Fatal("folder-block opted into a limited connection")
	}
}

func TestLimitedRelayCarriesJournalButNotBlocks(t *testing.T) {
	nat.DefaultPaths.Reset()
	t.Cleanup(nat.DefaultPaths.Reset)
	resetPunchCache()
	t.Cleanup(resetPunchCache)
	prevWait := holePunchWait
	holePunchWait = 200 * time.Millisecond
	t.Cleanup(func() { holePunchWait = prevWait })
	mesh.mu.Lock()
	prevPublic := append([]string(nil), mesh.publicRelays...)
	prevIdentity := append([]string(nil), mesh.identityRelays...)
	mesh.publicRelays = nil
	mesh.identityRelays = nil
	mesh.mu.Unlock()
	t.Cleanup(func() {
		mesh.mu.Lock()
		mesh.publicRelays = prevPublic
		mesh.identityRelays = prevIdentity
		mesh.mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	limited := newRelayHost(t, false)
	unlimited := newRelayHost(t, true)
	left := newPrivateHost(t)
	right := newPrivateHost(t)
	if err := left.Connect(ctx, peerInfo(limited)); err != nil {
		t.Fatal(err)
	}
	if err := right.Connect(ctx, peerInfo(limited)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reserve(ctx, right, peerInfo(limited)); err != nil {
		t.Fatal(err)
	}
	circuit := tcpAddr(t, limited) + "/p2p/" + limited.ID().String() + "/p2p-circuit"
	circuitAddr, err := ma.NewMultiaddr(circuit)
	if err != nil {
		t.Fatal(err)
	}
	if err := left.Connect(ctx, peer.AddrInfo{ID: right.ID(), Addrs: []ma.Multiaddr{circuitAddr}}); err != nil {
		t.Fatal(err)
	}
	if unlimitedPeerConn(left, right.ID()) != nil {
		t.Fatal("limited relay circuit was not limited")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync-journal":
			_, _ = io.WriteString(w, `{"entries":[],"cursor":1}`)
		case "/v1/folder-block":
			_, _ = io.WriteString(w, "block-ok")
		default:
			http.NotFound(w, r)
		}
	})
	apiMu.Lock()
	prevHandler, prevHost := apiHandler, apiHost
	apiMu.Unlock()
	SetAPIHandler(handler)
	SetAPIHost(left)
	ServeAPI(right)
	t.Cleanup(func() {
		SetAPIHandler(prevHandler)
		SetAPIHost(prevHost)
	})

	session, err := DialStream(ctx, right.ID().String(), []string{circuit})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := session.Client.Get(StreamBaseURL + "/v1/sync-journal?folder=shared")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(journal.Body)
	_ = journal.Body.Close()
	if journal.StatusCode != http.StatusOK || !strings.Contains(string(body), "cursor") {
		t.Fatalf("journal = %d %s", journal.StatusCode, body)
	}
	if session.CurrentPath() != nat.PathLimitedRelay {
		t.Fatalf("journal path = %s", session.CurrentPath())
	}

	_, err = session.Client.Get(StreamBaseURL + "/v1/folder-block?folder=shared&path=a&index=0")
	if !errors.Is(err, ErrLimitedRelay) {
		t.Fatalf("bulk over limited relay err = %v", err)
	}
	obs, ok := nat.DefaultPaths.Active(right.ID().String())
	if !ok || obs.Path != nat.PathLimitedRelay {
		t.Fatalf("path after refusal = %+v ok=%v", obs, ok)
	}

	rawCtx := network.WithAllowLimitedConn(ctx, "test")
	stream, err := left.NewStream(rawCtx, right.ID(), APIProtocol)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "/v1/folder-block?folder=shared", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := req.Write(stream); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(stream), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("X-FSE-Limited-Relay") != "1" {
		t.Fatalf("server status = %d header = %q", resp.StatusCode, resp.Header.Get("X-FSE-Limited-Relay"))
	}

	if err := right.Connect(ctx, peerInfo(unlimited)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reserve(ctx, right, peerInfo(unlimited)); err != nil {
		t.Fatal(err)
	}
	mesh.mu.Lock()
	mesh.publicRelays = []string{tcpAddr(t, unlimited) + "/p2p/" + unlimited.ID().String()}
	mesh.mu.Unlock()
	resetPunchCache()
	block, err := session.Client.Get(StreamBaseURL + "/v1/folder-block?folder=shared&path=a&index=0")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(block.Body)
	_ = block.Body.Close()
	if block.StatusCode != http.StatusOK || string(body) != "block-ok" {
		t.Fatalf("block = %d %s", block.StatusCode, body)
	}
	conn := unlimitedPeerConn(left, right.ID())
	if conn == nil || conn.Stat().Limited {
		t.Fatal("bulk transfer did not move to an unlimited relay")
	}
	if session.CurrentPath() == nat.PathLimitedRelay {
		t.Fatal("session still reports the limited relay after the unlimited circuit")
	}
}

func newRelayHost(t *testing.T, unlimited bool) host.Host {
	t.Helper()
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.ForceReachabilityPublic(),
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.EnableRelayService(relayv2.WithACL(allowEveryone{})),
	}
	if unlimited {
		opts[len(opts)-1] = libp2p.EnableRelayService(relayv2.WithACL(allowEveryone{}), relayv2.WithInfiniteLimits())
	}
	return newTestHost(t, opts...)
}

func newPrivateHost(t *testing.T) host.Host {
	t.Helper()
	return newTestHost(t,
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.ForceReachabilityPrivate(),
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.EnableRelay(),
	)
}

func peerInfo(h host.Host) peer.AddrInfo {
	return peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}
}

func tcpAddr(t *testing.T, h host.Host) string {
	t.Helper()
	for _, addr := range h.Addrs() {
		if addr != nil && strings.Contains(addr.String(), "/tcp/") && !strings.Contains(addr.String(), "quic") {
			return addr.String()
		}
	}
	t.Fatal("host has no TCP address")
	return ""
}
