package natlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/foldershare"
	"filesyncengine/internal/link"
)

type relayAllowAll struct{}

func (relayAllowAll) AllowReserve(peer.ID, ma.Multiaddr) bool          { return true }
func (relayAllowAll) AllowConnect(peer.ID, ma.Multiaddr, peer.ID) bool { return true }

type relayInfo struct {
	Addr string `json:"addr"`
	Peer string `json:"peer"`
}

type leftInfo struct {
	Peer    string `json:"peer"`
	Circuit string `json:"circuit"`
	Direct  string `json:"direct"`
}

// TestNATNamespaceEnginesSync puts two engines behind separate NAT namespaces
// with no port forwards. A public relay is the only path between them, and a
// file must still sync.
func TestNATNamespaceEnginesSync(t *testing.T) {
	if role := os.Getenv("FSE_NATNS_ROLE"); role != "" {
		runNATRole(t, role)
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root network namespaces")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("iproute2 is required")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		t.Skip("iptables is required")
	}

	id := os.Getpid() % 10000
	nsA := fmt.Sprintf("fsea%d", id)
	nsB := fmt.Sprintf("fseb%d", id)
	wan := fmt.Sprintf("fsew%d", id)
	aHost, aNS := fmt.Sprintf("vah%d", id), fmt.Sprintf("van%d", id)
	bHost, bNS := fmt.Sprintf("vbh%d", id), fmt.Sprintf("vbn%d", id)
	dir := t.TempDir()

	runCmd(t, "sysctl", "-w", "net.ipv4.ip_forward=1")
	runCmd(t, "ip", "link", "add", wan, "type", "bridge")
	runCmd(t, "ip", "addr", "add", "10.200.0.1/24", "dev", wan)
	runCmd(t, "ip", "link", "set", wan, "up")
	addNAT(t, nsA, aHost, aNS, "10.201.0.1/24", "10.201.0.2/24")
	addNAT(t, nsB, bHost, bNS, "10.202.0.1/24", "10.202.0.2/24")
	runCmd(t, "iptables", "-I", "FORWARD", "1", "-s", "10.201.0.0/24", "-d", "10.202.0.0/24", "-j", "DROP")
	runCmd(t, "iptables", "-I", "FORWARD", "1", "-s", "10.202.0.0/24", "-d", "10.201.0.0/24", "-j", "DROP")
	runCmd(t, "iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "10.201.0.0/24", "-d", "10.200.0.0/24", "-j", "MASQUERADE")
	runCmd(t, "iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "10.202.0.0/24", "-d", "10.200.0.0/24", "-j", "MASQUERADE")
	t.Cleanup(func() {
		_ = exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", "10.201.0.0/24", "-d", "10.200.0.0/24", "-j", "MASQUERADE").Run()
		_ = exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", "10.202.0.0/24", "-d", "10.200.0.0/24", "-j", "MASQUERADE").Run()
		_ = exec.Command("iptables", "-D", "FORWARD", "-s", "10.201.0.0/24", "-d", "10.202.0.0/24", "-j", "DROP").Run()
		_ = exec.Command("iptables", "-D", "FORWARD", "-s", "10.202.0.0/24", "-d", "10.201.0.0/24", "-j", "DROP").Run()
		_ = exec.Command("ip", "netns", "del", nsA).Run()
		_ = exec.Command("ip", "netns", "del", nsB).Run()
		_ = exec.Command("ip", "link", "del", wan).Run()
		_ = exec.Command("ip", "link", "del", aHost).Run()
		_ = exec.Command("ip", "link", "del", bHost).Run()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	relayHost, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/10.200.0.1/tcp/0"),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(relayv2.WithACL(relayAllowAll{})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relayHost.Close() })
	relayAddr := tcpMultiaddr(t, relayHost)
	if err := writeJSON(filepath.Join(dir, "relay.json"), relayInfo{Addr: relayAddr, Peer: relayHost.ID().String()}); err != nil {
		t.Fatal(err)
	}
	_ = ctx

	leftCmd := namespaceTest(nsA, dir, "left")
	rightCmd := namespaceTest(nsB, dir, "right")
	if err := leftCmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := rightCmd.Start(); err != nil {
		t.Fatal(err)
	}
	leftDone := make(chan error, 1)
	rightDone := make(chan error, 1)
	go func() { leftDone <- leftCmd.Wait() }()
	go func() { rightDone <- rightCmd.Wait() }()
	timeout := time.After(90 * time.Second)
	select {
	case err := <-rightDone:
		if err != nil {
			t.Fatalf("receiver: %v\n%s", err, rightCmd.String())
		}
	case <-timeout:
		_ = leftCmd.Process.Kill()
		_ = rightCmd.Process.Kill()
		t.Fatal("timed out waiting for the NAT sync")
	}
	select {
	case err := <-leftDone:
		if err != nil {
			t.Fatalf("sender: %v", err)
		}
	case <-timeout:
		_ = leftCmd.Process.Kill()
		t.Fatal("sender did not exit")
	}
	got, err := os.ReadFile(filepath.Join(dir, "synced.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "nat-sync-ok" {
		t.Fatalf("synced %q", got)
	}
}

func addNAT(t *testing.T, ns, hostVeth, nsVeth, hostCIDR, nsCIDR string) {
	t.Helper()
	runCmd(t, "ip", "netns", "add", ns)
	runCmd(t, "ip", "link", "add", hostVeth, "type", "veth", "peer", "name", nsVeth)
	runCmd(t, "ip", "link", "set", hostVeth, "up")
	runCmd(t, "ip", "addr", "add", hostCIDR, "dev", hostVeth)
	runCmd(t, "ip", "link", "set", nsVeth, "netns", ns)
	runCmd(t, "ip", "netns", "exec", ns, "ip", "link", "set", "lo", "up")
	runCmd(t, "ip", "netns", "exec", ns, "ip", "link", "set", nsVeth, "up")
	runCmd(t, "ip", "netns", "exec", ns, "ip", "addr", "add", nsCIDR, "dev", nsVeth)
	runCmd(t, "ip", "netns", "exec", ns, "ip", "route", "add", "default", "via", strings.Split(hostCIDR, "/")[0])
}

func namespaceTest(ns, dir, role string) *exec.Cmd {
	cmd := exec.Command("ip", "netns", "exec", ns, os.Args[0], "-test.run", "^TestNATNamespaceEnginesSync$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), "FSE_NATNS_ROLE="+role, "FSE_NATNS_DIR="+dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func runNATRole(t *testing.T, role string) {
	dir := os.Getenv("FSE_NATNS_DIR")
	if dir == "" {
		t.Fatal("missing FSE_NATNS_DIR")
	}
	switch role {
	case "left":
		runLeft(t, dir)
	case "right":
		runRight(t, dir)
	default:
		t.Fatalf("unknown role %s", role)
	}
}

func runLeft(t *testing.T, dir string) {
	info := waitJSON[relayInfo](t, filepath.Join(dir, "relay.json"), 20*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	h := privateHost(t, "/ip4/10.201.0.2/tcp/0")
	relayAddr, err := ma.NewMultiaddr(info.Addr + "/p2p/" + info.Peer)
	if err != nil {
		t.Fatal(err)
	}
	relayPeer, err := peer.Decode(info.Peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Connect(ctx, peer.AddrInfo{ID: relayPeer, Addrs: []ma.Multiaddr{relayAddr}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reserve(ctx, h, peer.AddrInfo{ID: relayPeer, Addrs: []ma.Multiaddr{relayAddr}}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "left-folder")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("nat-sync-ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "left", "node.jsonc")
	foldershare.Reconcile(config.Config{NodeName: "left", Folders: []config.FolderConfig{{
		ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendOnly,
	}}}, configPath, nil)
	discovery.SetAPIHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync-journal":
			http.ServeFile(w, r, foldershare.JournalPath(configPath, "shared"))
		case "/v1/folder-block":
			data, readErr := os.ReadFile(filepath.Join(root, r.URL.Query().Get("path")))
			if readErr != nil {
				http.Error(w, readErr.Error(), http.StatusNotFound)
				return
			}
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	discovery.SetAPIHost(h)
	ln, err := net.Listen("tcp", "10.201.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := writeJSON(filepath.Join(dir, "left.json"), leftInfo{
		Peer:    h.ID().String(),
		Circuit: info.Addr + "/p2p/" + info.Peer + "/p2p-circuit",
		Direct:  ln.Addr().String(),
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(70 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "right.done")); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("receiver did not finish")
}

func runRight(t *testing.T, dir string) {
	info := waitJSON[leftInfo](t, filepath.Join(dir, "left.json"), 40*time.Second)
	dialer := net.Dialer{Timeout: 1500 * time.Millisecond}
	if conn, err := dialer.Dial("tcp", info.Direct); err == nil {
		_ = conn.Close()
		t.Fatalf("direct dial to %s succeeded; the NAT drop is not in place", info.Direct)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	h := privateHost(t, "/ip4/10.202.0.2/tcp/0")
	discovery.SetAPIHost(h)
	logFile, err := os.Create(filepath.Join(dir, "right.log"))
	if err != nil {
		t.Fatal(err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	root := filepath.Join(dir, "right-folder")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "right", "node.jsonc")
	peer := config.PeerConfig{
		ID:        "left-peer",
		APIKey:    "secret",
		DHTPeerID: info.Peer,
		Relays:    []string{info.Circuit},
		Endpoints: []config.EndpointConfig{{
			Kind: "manual", Address: "http://203.0.113.50:22420", NetworkHint: "wan",
		}},
	}
	used, err := link.Dial(ctx, peer, func(ctx context.Context, base string, client *http.Client) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/sync-journal?folder=shared", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("journal %s: %s", resp.Status, body)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("relay dial: %v", err)
	}
	if used != discovery.StreamBaseURL {
		t.Fatalf("journal dial used %s, want the libp2p stream", used)
	}
	foldershare.Reconcile(config.Config{
		NodeName: "right",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendReceive,
		}},
		Peers: []config.PeerConfig{peer},
	}, configPath, nil)
	_ = logFile.Close()
	got, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		logged, _ := os.ReadFile(filepath.Join(dir, "right.log"))
		t.Fatalf("synced file missing (%v); log %s", err, logged)
	}
	if string(got) != "nat-sync-ok" {
		t.Fatalf("synced %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "synced.txt"), got, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "right.done"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func privateHost(t *testing.T, listen string) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(listen),
		libp2p.ForceReachabilityPrivate(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func tcpMultiaddr(t *testing.T, h host.Host) string {
	t.Helper()
	for _, addr := range h.Addrs() {
		if addr == nil {
			continue
		}
		text := addr.String()
		if strings.Contains(text, "/tcp/") && !strings.Contains(text, "quic") && !strings.Contains(text, "webtransport") {
			return text
		}
	}
	t.Fatal("relay has no TCP address")
	return ""
}

func waitJSON[T any](t *testing.T, path string, timeout time.Duration) T {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var zero T
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var decoded T
			if err := json.Unmarshal(data, &decoded); err == nil {
				return decoded
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return zero
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func runCmd(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}
