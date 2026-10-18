package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/advertise"
)

func TestLibp2pIdentityEnvUsesTheProvidedKey(t *testing.T) {
	t.Setenv("FSE_LIBP2P_PRIVATE_KEY", "CAESQEJDZ+N7os4cl6ICoZdZTNr5kYkeVxBxnQHp0WD798pTTu5F0fYA5fqAt9f59jrDBJ8m/Z3zpnUPmtq5Di9ml+E=")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	router, err := NewLibp2pDHTRouter(ctx, Libp2pDHTRouterOptions{NodeID: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	if router.PeerID() != "12D3KooWF8UhVBmTEmqqbEjYg9wdQpV7E8KzAoXj7vBHrnDWBkrU" {
		t.Fatalf("peer id = %s", router.PeerID())
	}
}

func TestLibp2pPortEnvFallsBackAndPublishes(t *testing.T) {
	t.Setenv(EnvLibp2pPort, "nope")
	if Libp2pListenPort() != DefaultLibp2pPort {
		t.Fatalf("listen = %d", Libp2pListenPort())
	}
	t.Setenv(EnvLibp2pPort, "22421")
	t.Setenv(EnvPublishedLibp2pPort, "32421")
	if PublishedLibp2pPort(Libp2pListenPort()) != 32421 {
		t.Fatalf("published = %d", PublishedLibp2pPort(Libp2pListenPort()))
	}
}

func TestAnnouncedMultiaddrsUsePublishedPortAndAPI(t *testing.T) {
	if advertise.HostReachable("172.17.0.2") {
		t.Fatal("bridge address was treated as reachable")
	}
	var hosts []string
	for _, host := range []string{"172.17.0.2", "192.0.2.10", "published-a"} {
		if advertise.HostReachable(host) {
			hosts = append(hosts, host)
		}
	}
	addrs := announcedMultiaddrs(hosts, "https://published-a:22430", 22423)
	joined := strings.Join(multiaddrStrings(addrs), " ")
	if strings.Contains(joined, "172.17.0.2") {
		t.Fatalf("bridge address advertised: %s", joined)
	}
	for _, want := range []string{
		"/ip4/192.0.2.10/tcp/22423",
		"/ip4/192.0.2.10/udp/22423/quic-v1",
		"/dns4/published-a/tcp/22423",
		"/dns4/published-a/tcp/22430/https",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	var apiURL string
	for _, addr := range addrs {
		if raw, ok := apiURLFromMultiaddr(addr); ok {
			apiURL = raw
		}
	}
	if apiURL != "https://published-a:22430" {
		t.Fatalf("api url = %q", apiURL)
	}
	dialable := dialablePairingAddrs(addrs)
	for _, addr := range dialable {
		if _, ok := apiURLFromMultiaddr(addr); ok {
			t.Fatalf("API address was dialed as libp2p: %s", addr)
		}
	}
}

func TestAnnounceHostsKeepEveryPath(t *testing.T) {
	t.Setenv(advertise.EnvVarList, "https://203.0.113.9:22420")
	t.Setenv(advertise.EnvExcludeTailscale, "")
	SetAnnounceAPIURL("https://100.64.8.8:22420")
	t.Cleanup(func() { SetAnnounceAPIURL("") })
	joined := strings.Join(multiaddrStrings(announcedAddrs(nil)), " ")
	for _, want := range []string{"/ip4/203.0.113.9/tcp/", "/ip4/100.64.8.8/tcp/"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	t.Setenv(advertise.EnvExcludeTailscale, "1")
	excluded := strings.Join(multiaddrStrings(announcedAddrs(nil)), " ")
	if strings.Contains(excluded, "100.64.8.8") {
		t.Fatalf("tailscale still announced: %s", excluded)
	}
	if !strings.Contains(excluded, "/ip4/203.0.113.9/tcp/") {
		t.Fatalf("public path missing: %s", excluded)
	}
}

func TestAnnouncedAddrsReplaceBridgeListenAddrs(t *testing.T) {
	t.Setenv(EnvPublishedLibp2pPort, "22423")
	SetAnnounceAPIURL("https://192.0.2.20:22430")
	t.Cleanup(func() { SetAnnounceAPIURL("") })
	bridge, err := ma.NewMultiaddr("/ip4/172.17.0.4/tcp/22421")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(multiaddrStrings(announcedAddrs([]ma.Multiaddr{bridge})), " ")
	if strings.Contains(joined, "172.17.0.4") {
		t.Fatalf("bridge listen address kept: %s", joined)
	}
	for _, want := range []string{"/ip4/192.0.2.20/tcp/22423", "/ip4/192.0.2.20/udp/22423/quic-v1", "/ip4/192.0.2.20/tcp/22430/https"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
}

func TestProductionRouterReusesThePublishedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvLibp2pPort, strconv.Itoa(port))
	t.Setenv(EnvPublishedLibp2pPort, strconv.Itoa(port))
	SetAnnounceAPIURL("https://192.0.2.10:22420")
	t.Cleanup(func() {
		ResetSharedRouter()
		SetAnnounceAPIURL("")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, err := ProductionRouter(ctx, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProductionRouter(ctx, "node-b")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("production router was created twice")
	}
	foundTCP := false
	foundQUIC := false
	for _, addr := range first.host.Addrs() {
		text := addr.String()
		if strings.Contains(text, fmt.Sprintf("/tcp/%d", port)) && !strings.Contains(text, "quic") {
			foundTCP = true
		}
		if strings.Contains(text, fmt.Sprintf("/udp/%d/quic-v1", port)) {
			foundQUIC = true
		}
	}
	if !foundTCP || !foundQUIC {
		t.Fatalf("listen addrs = %v", multiaddrStrings(first.host.Addrs()))
	}
}
