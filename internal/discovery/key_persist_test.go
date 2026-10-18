package discovery

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLibp2pKeyFileSurvivesRestart(t *testing.T) {
	t.Setenv("FSE_LIBP2P_PRIVATE_KEY", "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv(EnvLibp2pPort, strconv.Itoa(port))
	t.Setenv(EnvPublishedLibp2pPort, strconv.Itoa(port))
	path := filepath.Join(t.TempDir(), "appdata", "libp2p.key")
	SetLibp2pKeyPath(path)
	t.Cleanup(func() {
		ResetSharedRouter()
		SetLibp2pKeyPath("")
	})
	ResetSharedRouter()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, err := ProductionRouter(ctx, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	id := first.PeerID()
	ResetSharedRouter()
	second, err := ProductionRouter(ctx, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if second.PeerID() != id {
		t.Fatalf("peer id changed from %s to %s", id, second.PeerID())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o", info.Mode().Perm())
	}
}
