package streamsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/peeridentity"
	"filesyncengine/internal/protocol"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/scanner"
	"filesyncengine/internal/state"
)

func TestPullFolderCopiesRemoteFilesOverStreamProtocol(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(remoteRoot, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "nested", "doc.txt"), []byte("hello through a pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "stale.txt"), []byte("remove me after successful writes"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	data, err := os.ReadFile(filepath.Join(localRoot, "nested", "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello through a pipe" {
		t.Fatalf("synced content = %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(localRoot, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file should be deleted after successful pull, err=%v", err)
	}
	if result.FilesWritten != 1 || result.BlocksFetched == 0 || result.FilesDeleted != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPullFolderPreservesIgnoredLocalFilesDuringStaleDelete(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), []byte("remote data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localRoot, ".sync"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localRoot, "cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, ".sync", "ignore"), []byte("cache/**\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "cache", "local.tmp"), []byte("preserve ignored local data"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	if _, err := os.Stat(filepath.Join(localRoot, ".sync", "ignore")); err != nil {
		t.Fatalf("local sync metadata should be preserved: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(localRoot, "cache", "local.tmp")); err != nil || string(data) != "preserve ignored local data" {
		t.Fatalf("ignored local file was not preserved, data=%q err=%v", string(data), err)
	}
	if result.FilesDeleted != 0 {
		t.Fatalf("ignored local files should not count as stale deletes: %+v", result)
	}
}

func TestPullFolderFetchesMissingInShareIgnoreIncludeBeforeStreamPlanning(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	for _, dir := range []string{filepath.Join(remoteRoot, ".sync"), filepath.Join(remoteRoot, "secret"), filepath.Join(localRoot, ".sync"), filepath.Join(localRoot, "secret")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, ".sync", "ignore"), []byte("#include shared-ignore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "shared-ignore"), []byte("secret/**\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "visible.txt"), []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "secret", "remote.txt"), []byte("remote secret ignored by include"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, ".sync", "ignore"), []byte("#include shared-ignore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "secret", "local.tmp"), []byte("preserve local ignored data"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	includeData, err := os.ReadFile(filepath.Join(localRoot, "shared-ignore"))
	if err != nil {
		t.Fatalf("missing in-share include was not fetched: %v", err)
	}
	if string(includeData) != "secret/**\n" {
		t.Fatalf("fetched include = %q", string(includeData))
	}
	if data, err := os.ReadFile(filepath.Join(localRoot, "secret", "local.tmp")); err != nil || string(data) != "preserve local ignored data" {
		t.Fatalf("local file ignored by fetched include was not preserved, data=%q err=%v", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "secret", "remote.txt")); !os.IsNotExist(err) {
		t.Fatalf("remote path ignored by fetched include should not be pulled, err=%v", err)
	}
	if result.FilesDeleted != 0 || result.FilesWritten != 1 {
		t.Fatalf("expected only visible file write and no ignored stale delete: %+v", result)
	}
}

func TestPullFolderReportsUnavailableMissingInShareIgnoreIncludeOverStream(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(localRoot, ".sync"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, ".sync", "ignore"), []byte("#include shared-missing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "visible.txt"), []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	if len(result.MissingIgnoreIncludes) != 1 || result.MissingIgnoreIncludes[0] != "shared-missing" {
		t.Fatalf("missing include status not reported: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "visible.txt")); err != nil {
		t.Fatalf("sync should continue when peer cannot provide stream include: %v", err)
	}
}

func TestPullFolderReusesLocalBlocksBeforeRequestingStreamBlocks(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	content := []byte("already-local-block")
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "seed.bin"), append(append([]byte{}, content...), []byte(" extra")...), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, len(content))

	data, err := os.ReadFile(filepath.Join(localRoot, "remote.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Fatalf("synced content = %q", string(data))
	}
	if result.BlocksFetched != 0 || result.BlocksReused != 1 || result.FilesWritten != 1 {
		t.Fatalf("expected one reused block and no fetched blocks: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "seed.bin")); !os.IsNotExist(err) {
		t.Fatalf("seed should be removed as stale after reuse, err=%v", err)
	}
}

func TestPullFolderPrefersDirectStreamBlockSourceOverRelay(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("direct stream block bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	relayClient, relayServer := net.Pipe()
	defer relayClient.Close()
	relayBlockRequests := make(chan struct{}, 1)
	relayErr := make(chan error, 1)
	go func() {
		relayErr <- serveRelayIndexOnlyStream(relayServer, remoteRoot, relayBlockRequests)
	}()

	dialDirect := func(ctx context.Context) (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go func() {
			_ = NewServer(ServerConfig{NodeID: "direct-peer", BlockSize: 64, Folders: map[string]string{"docs": remoteRoot}}).Serve(ctx, server)
		}()
		return client, nil
	}

	result, err := PullFolder(context.Background(), relayClient, PullOptions{
		NodeID:    "node-b",
		FolderID:  "docs",
		LocalRoot: localRoot,
		BlockSources: []StreamBlockSource{
			{PeerID: "relay-peer", Path: routing.RelayPath, Network: routing.WANNetwork, Reachable: true},
			{PeerID: "direct-peer", Dial: dialDirect, Path: routing.DirectPath, Network: routing.LocalNetwork, Reachable: true},
		},
	})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	select {
	case <-relayBlockRequests:
		t.Fatalf("relay stream should not receive block requests when direct source has the block")
	default:
	}
	if result.BlocksFetched != 1 || result.FilesWritten != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertStreamFile(t, filepath.Join(localRoot, "doc.txt"), "direct stream block bytes")
	relayClient.Close()
	if err := <-relayErr; err != nil {
		t.Fatalf("relay server: %v", err)
	}
}

func TestPullFolderRenamesMatchingStaleLocalFileBeforeRequestingBlocks(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	content := []byte("moved over stream without transfer")
	if err := os.MkdirAll(filepath.Join(remoteRoot, "new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localRoot, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "new", "doc.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "old", "doc.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, len(content))

	data, err := os.ReadFile(filepath.Join(localRoot, "new", "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Fatalf("moved content = %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(localRoot, "old", "doc.txt")); !os.IsNotExist(err) {
		t.Fatalf("old moved path should be gone after stream move reuse, err=%v", err)
	}
	if result.FilesWritten != 0 || result.BlocksFetched != 0 || result.BlocksReused != 0 || result.FilesDeleted != 0 || result.FilesMoved != 1 {
		t.Fatalf("expected one move without transfer/write/delete churn: %+v", result)
	}
}

func TestPullFolderDoesNotMoveOverDivergentExistingLocalPath(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	remoteContent := []byte("remote moved content")
	if err := os.MkdirAll(filepath.Join(remoteRoot, "new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localRoot, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localRoot, "new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "new", "doc.txt"), remoteContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "old", "doc.txt"), remoteContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "new", "doc.txt"), []byte("divergent local content"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, len(remoteContent))

	data, err := os.ReadFile(filepath.Join(localRoot, "new", "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(remoteContent) {
		t.Fatalf("remote content should be applied at final path, got %q", string(data))
	}
	if result.FilesMoved != 0 || result.FilesWritten != 1 {
		t.Fatalf("divergent existing path should be written, not moved over: %+v", result)
	}
}

func TestPullFolderSkipsStagingWhenLocalManifestAlreadyMatches(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	content := []byte("already current")
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "doc.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	if result.FilesWritten != 0 || result.BlocksFetched != 0 || result.BlocksReused != 0 || result.FilesDeleted != 0 {
		t.Fatalf("matching local file should be a no-op, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(localRoot, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Fatalf("local content changed: %q", string(data))
	}
}

func TestPullFolderResumesValidStreamTempBlocksBeforeFetching(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	content := []byte("helloworld")
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "doc.txt.fse-stream-tmp"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	data, err := os.ReadFile(filepath.Join(localRoot, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Fatalf("synced content = %q", string(data))
	}
	if result.BlocksFetched != 1 || result.BlocksReused != 0 || result.FilesWritten != 1 {
		t.Fatalf("expected one resumed temp block and one fetched block: %+v", result)
	}
}

func TestPullFolderCleansInterruptedStreamTempFilesBeforePlanningDeletes(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), []byte("stream data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "old.txt.fse-stream-tmp"), []byte("interrupted temp"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	if result.FilesDeleted != 0 {
		t.Fatalf("interrupted stream temp cleanup should not be counted as stale delete: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "old.txt.fse-stream-tmp")); !os.IsNotExist(err) {
		t.Fatalf("interrupted stream temp was not cleaned up: %v", err)
	}
}

func TestPullFolderNegotiatesHighestPeerEncryptionLevel(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("negotiated data"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", BlockSize: 8, Folders: map[string]string{"docs": remoteRoot}, Identity: serverIdentity, EncryptionLevel: 4, TrustedPeerPublicKeys: map[string]string{"node-b": clientIdentity.PublicKey}})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot, Identity: clientIdentity, EncryptionLevel: 8, PeerPublicKey: serverIdentity.PublicKey})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.NegotiatedEncryptionLevel != 8 {
		t.Fatalf("negotiated encryption level = %d", result.NegotiatedEncryptionLevel)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderEnforcesNegotiatedReceiveCapOnFetchedBlocks(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	payload := strings.Repeat("x", 128)
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", BlockSize: 128, Folders: map[string]string{"docs": remoteRoot}})
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(context.Background(), serverConn) }()

	started := time.Now()
	result, err := PullFolder(context.Background(), clientConn, PullOptions{
		NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot,
		Transfer: config.TransferConfig{ReceiveBytesPerSecond: 128},
	})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.BlocksFetched != 1 {
		t.Fatalf("expected one fetched block, got %+v", result)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Fatalf("pull completed too quickly for 128 B/s receive cap: %s", elapsed)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderExchangesTransferCapsDuringHandshake(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("rate limited content"), 0o600); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{
		NodeID: "node-a", BlockSize: 8, Folders: map[string]string{"docs": remoteRoot},
		Transfer: config.TransferConfig{SendBytesPerSecond: 10_000, ReceiveBytesPerSecond: 20_000},
		Peer:     config.PeerConfig{ID: "node-b", SendBytesPerSecond: 7_000, ReceiveBytesPerSecond: 9_000},
	})
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(context.Background(), serverConn) }()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{
		NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot,
		Transfer: config.TransferConfig{SendBytesPerSecond: 6_000, ReceiveBytesPerSecond: 8_000},
		Peer:     config.PeerConfig{ID: "node-a", SendBytesPerSecond: 5_000},
	})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.NegotiatedTransfer.SendBytesPerSecond != 5_000 || result.NegotiatedTransfer.ReceiveBytesPerSecond != 7_000 {
		t.Fatalf("negotiated transfer = %+v", result.NegotiatedTransfer)
	}
	if result.NegotiatedTransferSendCause != "local_peer" || result.NegotiatedTransferReceiveCause != "remote_send" {
		t.Fatalf("negotiated transfer causes = send %q receive %q", result.NegotiatedTransferSendCause, result.NegotiatedTransferReceiveCause)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderRejectsWeakerNegotiatedEncryptionWithoutOverride(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("weak negotiation"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", BlockSize: 8, Folders: map[string]string{"docs": remoteRoot}, Identity: serverIdentity, EncryptionLevel: 1, AllowWeakerEncryptionLevel: true, TrustedPeerPublicKeys: map[string]string{"node-b": clientIdentity.PublicKey}})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	_, err = PullFolder(context.Background(), clientConn, PullOptions{NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot, Identity: clientIdentity, EncryptionLevel: 8, PeerPublicKey: serverIdentity.PublicKey})
	if err == nil || !strings.Contains(err.Error(), "weaker encryption level") {
		t.Fatalf("expected weaker encryption rejection, got %v", err)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server should stop after client rejects weaker level: %v", err)
	}
}

func TestPullFolderAcceptsExplicitWeakerEncryptionOverride(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("lawful compatibility"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", BlockSize: 8, Folders: map[string]string{"docs": remoteRoot}, Identity: serverIdentity, EncryptionLevel: 1, AllowWeakerEncryptionLevel: true, TrustedPeerPublicKeys: map[string]string{"node-b": clientIdentity.PublicKey}})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot, Identity: clientIdentity, EncryptionLevel: 8, PeerPublicKey: serverIdentity.PublicKey, AllowWeakerEncryptionLevel: true})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.NegotiatedEncryptionLevel != 1 {
		t.Fatalf("negotiated encryption level = %d", result.NegotiatedEncryptionLevel)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderRejectsUnexpectedSignedPeerIdentity(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("signed peer data"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	unexpectedIdentity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", BlockSize: 8, Folders: map[string]string{"docs": remoteRoot}, Identity: serverIdentity, EncryptionLevel: 5, TrustedPeerPublicKeys: map[string]string{"node-b": clientIdentity.PublicKey}})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	_, err = PullFolder(context.Background(), clientConn, PullOptions{NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot, Identity: clientIdentity, EncryptionLevel: 5, PeerPublicKey: unexpectedIdentity.PublicKey})
	if err == nil || !strings.Contains(err.Error(), "peer identity public key mismatch") {
		t.Fatalf("expected peer identity mismatch, got %v", err)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server should accept trusted client identity before client rejects server: %v", err)
	}
}

type meshSettingsStoreStub struct {
	changes map[string][]state.PendingSettingsChange
}

func (s *meshSettingsStoreStub) ListPendingSettingsChanges(targetNodeID string) ([]state.PendingSettingsChange, error) {
	out := append([]state.PendingSettingsChange(nil), s.changes[targetNodeID]...)
	return out, nil
}

func (s *meshSettingsStoreStub) UpdatePendingSettingsChangeStatus(targetNodeID string, changeID string, status string, updatedAt string, lastError string) error {
	for i := range s.changes[targetNodeID] {
		if s.changes[targetNodeID][i].ID == changeID {
			s.changes[targetNodeID][i].Status = status
			s.changes[targetNodeID][i].UpdatedAt = updatedAt
			s.changes[targetNodeID][i].LastError = lastError
			return nil
		}
	}
	return fmt.Errorf("change %q not found", changeID)
}

func TestServerDeliversQueuedMeshSettingsChangesOnlyToTargetPeer(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	store := &meshSettingsStoreStub{changes: map[string][]state.PendingSettingsChange{
		"node-b": {
			{ID: "change-1", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-b:1", Revision: 7, Status: "queued", SettingsPatch: map[string]any{"logging.level": "warn"}},
			{ID: "change-acked", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-b:acked", Revision: 6, Status: "acked", SettingsPatch: map[string]any{"logging.level": "info"}},
		},
		"node-c": {
			{ID: "change-other", TargetNodeID: "node-c", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-c:1", Revision: 1, Status: "queued", SettingsPatch: map[string]any{"logging.level": "debug"}},
		},
	}}
	server := NewServer(ServerConfig{NodeID: "node-a", PendingSettingsStore: store})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	codec := protocol.NewCodec(clientConn, clientConn)
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: &protocol.Hello{ProtocolVersion: 1, NodeID: "node-b", EncryptionLevel: 0}}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if msg, err := codec.Read(); err != nil || msg.Type != protocol.MessageHello {
		t.Fatalf("server hello = %+v err=%v", msg, err)
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageMeshSettingsChanges, MeshSettingsChanges: &protocol.MeshSettingsChanges{TargetNodeID: "node-b"}}); err != nil {
		t.Fatalf("write mesh settings request: %v", err)
	}
	msg, err := codec.Read()
	if err != nil {
		t.Fatalf("read mesh settings response: %v", err)
	}
	if msg.Type != protocol.MessageMeshSettingsChanges || msg.MeshSettingsChanges == nil {
		t.Fatalf("unexpected response: %+v", msg)
	}
	if msg.MeshSettingsChanges.TargetNodeID != "node-b" {
		t.Fatalf("target node = %q, want node-b", msg.MeshSettingsChanges.TargetNodeID)
	}
	if len(msg.MeshSettingsChanges.Changes) != 1 {
		t.Fatalf("changes = %+v, want only queued node-b change", msg.MeshSettingsChanges.Changes)
	}
	change := msg.MeshSettingsChanges.Changes[0]
	if change.ID != "change-1" || change.TargetNodeID != "node-b" || change.SettingsPatch["logging.level"] != "warn" {
		t.Fatalf("unexpected delivered change: %+v", change)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderAppliesDeliveredMeshSettingsChangesToLocalNode(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	settingsStore := state.NewJSONStore(filepath.Join(t.TempDir(), "settings.json"))
	remoteSettings := state.NewJSONStore(filepath.Join(t.TempDir(), "remote-settings.json"))
	if err := remoteSettings.SavePendingSettingsChange("node-b", state.PendingSettingsChange{ID: "mesh-change-1", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-b:mesh-change-1", Revision: 11, Status: "queued", SettingsPatch: map[string]any{"logging.level": "warn"}}); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", Folders: map[string]string{"docs": remoteRoot}, PendingSettingsStore: remoteSettings})
	go func() { _ = server.Serve(context.Background(), serverConn) }()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot, MeshSettingsStore: settingsStore})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.MeshSettingsChangesApplied != 1 {
		t.Fatalf("MeshSettingsChangesApplied = %d, want 1", result.MeshSettingsChangesApplied)
	}
	doc, ok, err := settingsStore.LoadNodeSettingsDocument("node-b")
	if err != nil || !ok {
		t.Fatalf("LoadNodeSettingsDocument: doc=%+v ok=%v err=%v", doc, ok, err)
	}
	if doc.Settings["logging.level"] != "warn" || doc.Revision != 11 || doc.Source != "mesh" {
		t.Fatalf("delivered mesh settings change was not owner-applied: %+v", doc)
	}
	change, ok, err := settingsStore.LoadPendingSettingsChange("node-b", "mesh-change-1")
	if err != nil || !ok || change.Status != "applied" {
		t.Fatalf("delivered mesh settings change not stored/applied: change=%+v ok=%v err=%v", change, ok, err)
	}
}

func TestPullFolderPersistsMeshSettingsAcknowledgementOnOrigin(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	localSettings := state.NewJSONStore(filepath.Join(t.TempDir(), "local-settings.json"))
	remoteSettings := state.NewJSONStore(filepath.Join(t.TempDir(), "remote-settings.json"))
	if err := remoteSettings.SavePendingSettingsChange("node-b", state.PendingSettingsChange{ID: "mesh-change-ack", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-b:mesh-change-ack", Revision: 12, Status: "queued", SettingsPatch: map[string]any{"logging.level": "error"}}); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", Folders: map[string]string{"docs": remoteRoot}, PendingSettingsStore: remoteSettings})
	go func() { _ = server.Serve(context.Background(), serverConn) }()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot, MeshSettingsStore: localSettings})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.MeshSettingsChangesApplied != 1 {
		t.Fatalf("MeshSettingsChangesApplied = %d, want 1", result.MeshSettingsChangesApplied)
	}
	change, ok, err := remoteSettings.LoadPendingSettingsChange("node-b", "mesh-change-ack")
	if err != nil || !ok {
		t.Fatalf("LoadPendingSettingsChange: change=%+v ok=%v err=%v", change, ok, err)
	}
	if change.Status != "acked" || change.UpdatedAt == "" || change.LastError != "" {
		t.Fatalf("origin pending setting change was not acked cleanly: %+v", change)
	}
}

func TestExchangeMeshSettingsOnlyAppliesOfflineQueuedChangeWithoutFolderTransfer(t *testing.T) {
	localSettings := state.NewJSONStore(filepath.Join(t.TempDir(), "local-settings.json"))
	remoteSettings := state.NewJSONStore(filepath.Join(t.TempDir(), "remote-settings.json"))
	if err := remoteSettings.SavePendingSettingsChange("node-b", state.PendingSettingsChange{ID: "offline-edit-1", TargetNodeID: "node-b", OriginNodeID: "node-a", IdempotencyKey: "node-a:node-b:offline-edit-1", Revision: 15, Status: "queued", SettingsPatch: map[string]any{"logging.level": "debug"}}); err != nil {
		t.Fatalf("SavePendingSettingsChange: %v", err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", PendingSettingsStore: remoteSettings})
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(context.Background(), serverConn) }()

	applied, err := ExchangeMeshSettings(context.Background(), clientConn, PullOptions{NodeID: "node-b", MeshSettingsStore: localSettings})
	if err != nil {
		t.Fatalf("ExchangeMeshSettings: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	doc, ok, err := localSettings.LoadNodeSettingsDocument("node-b")
	if err != nil || !ok {
		t.Fatalf("LoadNodeSettingsDocument: doc=%+v ok=%v err=%v", doc, ok, err)
	}
	if doc.Settings["logging.level"] != "debug" || doc.Revision != 15 || doc.Source != "mesh" {
		t.Fatalf("offline mesh settings change was not owner-applied: %+v", doc)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
	change, ok, err := remoteSettings.LoadPendingSettingsChange("node-b", "offline-edit-1")
	if err != nil || !ok || change.Status != "acked" {
		t.Fatalf("origin pending setting change was not acknowledged: change=%+v ok=%v err=%v", change, ok, err)
	}
}

func TestPullFolderAppliesPeerMetadataChangesBeforeFileTransfer(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	hash := sha256.Sum256([]byte("server block"))
	if err := serverStore.SaveManifest("docs", "server.txt", block.Manifest{Path: "server.txt", Size: 12, BlockSize: 12, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 12, Hash: hash[:]}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "server.txt"), []byte("server block"), 0o600); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", BlockSize: 12, Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "peer-b", FolderID: "docs", LocalRoot: localRoot, MetadataStore: clientStore})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.MetadataChangesApplied != 1 {
		t.Fatalf("MetadataChangesApplied = %d, want 1", result.MetadataChangesApplied)
	}
	manifest, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", "server.txt")
	if err != nil || !ok {
		t.Fatalf("peer manifest was not cached: ok=%v err=%v", ok, err)
	}
	if manifest.Path != "server.txt" || manifest.Size != 12 {
		t.Fatalf("unexpected cached peer manifest: %+v", manifest)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderAppliesChunkedPeerMetadataChangesBeforeFileTransfer(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		content := []byte("metadata " + name)
		hash := sha256.Sum256(content)
		if err := os.WriteFile(filepath.Join(remoteRoot, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := serverStore.SaveManifest("docs", name, block.Manifest{Path: name, Size: int64(len(content)), BlockSize: len(content), HashState: "complete", Blocks: []block.Block{{Index: 0, Size: len(content), Hash: hash[:]}}}); err != nil {
			t.Fatal(err)
		}
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", BlockSize: 16, Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore, MaxMetadataChangesPerMessage: 2})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "peer-b", FolderID: "docs", LocalRoot: localRoot, MetadataStore: clientStore})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.MetadataChangesApplied != 3 {
		t.Fatalf("MetadataChangesApplied = %d, want 3", result.MetadataChangesApplied)
	}
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		if _, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", name); err != nil || !ok {
			t.Fatalf("peer manifest %s was not cached: ok=%v err=%v", name, ok, err)
		}
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderStopsMetadataCatchupAfterBatchBudgetAndTransfersFiles(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		content := []byte("metadata " + name)
		hash := sha256.Sum256(content)
		if err := os.WriteFile(filepath.Join(remoteRoot, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := serverStore.SaveManifest("docs", name, block.Manifest{Path: name, Size: int64(len(content)), BlockSize: len(content), HashState: "complete", Blocks: []block.Block{{Index: 0, Size: len(content), Hash: hash[:]}}}); err != nil {
			t.Fatal(err)
		}
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", BlockSize: 16, Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore, MaxMetadataChangesPerMessage: 1})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "peer-b", FolderID: "docs", LocalRoot: localRoot, MetadataStore: clientStore, MaxMetadataBatchesBeforeFileTransfer: 1})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.MetadataChangesApplied != 1 {
		t.Fatalf("MetadataChangesApplied = %d, want only the budgeted first batch", result.MetadataChangesApplied)
	}
	if result.FilesWritten != 3 || result.BlocksFetched == 0 {
		t.Fatalf("file transfer should proceed after metadata budget is reached: %+v", result)
	}
	if _, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", "one.txt"); err != nil || !ok {
		t.Fatalf("budgeted first peer manifest was not cached: ok=%v err=%v", ok, err)
	}
	if _, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", "three.txt"); err != nil || ok {
		t.Fatalf("unbudgeted peer manifest should remain pending for a later catch-up: ok=%v err=%v", ok, err)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderDefersStaleDeletesWhenMetadataCatchupStopsBeforeCurrent(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	for _, name := range []string{"one.txt", "two.txt"} {
		content := []byte("metadata " + name)
		hash := sha256.Sum256(content)
		if err := os.WriteFile(filepath.Join(remoteRoot, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := serverStore.SaveManifest("docs", name, block.Manifest{Path: name, Size: int64(len(content)), BlockSize: len(content), HashState: "complete", Blocks: []block.Block{{Index: 0, Size: len(content), Hash: hash[:]}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(localRoot, "stale.txt"), []byte("keep until metadata catches up"), 0o600); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", BlockSize: 16, Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore, MaxMetadataChangesPerMessage: 1})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "peer-b", FolderID: "docs", LocalRoot: localRoot, MetadataStore: clientStore, MaxMetadataBatchesBeforeFileTransfer: 1})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "stale.txt")); err != nil {
		t.Fatalf("stale file should remain until metadata catch-up is current: %v", err)
	}
	if result.FilesDeleted != 0 || result.MetadataChangesApplied != 1 {
		t.Fatalf("destructive delete should be deferred after stopped metadata catch-up: %+v", result)
	}
	localSummary, err := clientStore.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := clientStore.ReadySkippedDeletes("docs", localSummary)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 0 {
		t.Fatalf("deferred delete should not be ready before local metadata reaches required cursor: %+v", ready)
	}
	remoteSummary, err := serverStore.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	ready, err = clientStore.ReadySkippedDeletes("docs", remoteSummary)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].Path != "stale.txt" {
		t.Fatalf("deferred delete gate was not persisted with metadata prerequisites: %+v", ready)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderStartsAsyncMetadataCatchupWithoutBlockingFileTransfer(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	for _, name := range []string{"one.txt", "two.txt"} {
		content := []byte("metadata " + name)
		hash := sha256.Sum256(content)
		if err := os.WriteFile(filepath.Join(remoteRoot, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := serverStore.SaveManifest("docs", name, block.Manifest{Path: name, Size: int64(len(content)), BlockSize: len(content), HashState: "complete", Blocks: []block.Block{{Index: 0, Size: len(content), Hash: hash[:]}}}); err != nil {
			t.Fatal(err)
		}
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", BlockSize: 16, Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore, MaxMetadataChangesPerMessage: 1})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	asyncStarted := make(chan struct{})
	asyncRelease := make(chan struct{})
	asyncDone := make(chan error, 1)
	result, err := PullFolder(context.Background(), clientConn, PullOptions{
		NodeID: "peer-b", FolderID: "docs", LocalRoot: localRoot,
		MetadataStore: clientStore, MaxMetadataBatchesBeforeFileTransfer: 1,
		AsyncMetadataCatchupDial: func(context.Context) (io.ReadWriteCloser, error) {
			client, server := net.Pipe()
			go func() {
				defer server.Close()
				close(asyncStarted)
				<-asyncRelease
				asyncDone <- NewServer(ServerConfig{NodeID: "peer-a", BlockSize: 16, Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore, MaxMetadataChangesPerMessage: 1}).Serve(context.Background(), server)
			}()
			return client, nil
		},
	})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	select {
	case <-asyncStarted:
	default:
		t.Fatalf("async metadata catch-up was not started")
	}
	if !result.MetadataCatchupStarted {
		t.Fatalf("MetadataCatchupStarted = false, want true")
	}
	if result.FilesWritten != 2 || result.MetadataChangesApplied != 1 {
		t.Fatalf("foreground file transfer should finish after first metadata batch: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "two.txt")); err != nil {
		t.Fatalf("foreground file transfer did not write second file while async catch-up was blocked: %v", err)
	}
	close(asyncRelease)
	select {
	case err := <-asyncDone:
		if err != nil {
			t.Fatalf("async server: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("async metadata catch-up did not finish")
	}
	if _, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", "two.txt"); err != nil || !ok {
		t.Fatalf("async catch-up did not cache remaining peer manifest: ok=%v err=%v", ok, err)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestMetadataCatchupOnlyAppliesPeerMetadataWithoutFolderTransfer(t *testing.T) {
	remoteRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	content := []byte("metadata only")
	hash := sha256.Sum256(content)
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := serverStore.SaveManifest("docs", "remote.txt", block.Manifest{Path: "remote.txt", Size: int64(len(content)), BlockSize: len(content), HashState: "complete", Blocks: []block.Block{{Index: 0, Size: len(content), Hash: hash[:]}}}); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := MetadataCatchupOnly(context.Background(), clientConn, PullOptions{NodeID: "peer-b", FolderID: "docs", MetadataStore: clientStore}, "peer-a")
	if err != nil {
		t.Fatalf("MetadataCatchupOnly: %v", err)
	}
	if result.MetadataChangesApplied != 1 {
		t.Fatalf("MetadataChangesApplied = %d, want 1", result.MetadataChangesApplied)
	}
	if _, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", "remote.txt"); err != nil || !ok {
		t.Fatalf("metadata-only catch-up did not cache peer manifest: ok=%v err=%v", ok, err)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestServerMetadataStateWaitsForAckBeforeSendingNextMetadataBatch(t *testing.T) {
	remoteRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	for _, name := range []string{"one.txt", "two.txt"} {
		content := []byte("server " + name)
		hash := sha256.Sum256(content)
		if err := serverStore.SaveManifest("docs", name, block.Manifest{Path: name, Size: int64(len(content)), BlockSize: len(content), HashState: "complete", Blocks: []block.Block{{Index: 0, Size: len(content), Hash: hash[:]}}}); err != nil {
			t.Fatal(err)
		}
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore, MaxMetadataChangesPerMessage: 1})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()
	codec := protocol.NewCodec(clientConn, clientConn)
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: &protocol.Hello{ProtocolVersion: 1, NodeID: "peer-b", EncryptionLevel: 0}}); err != nil {
		t.Fatal(err)
	}
	if msg, err := codec.Read(); err != nil {
		t.Fatal(err)
	} else if msg.Type != protocol.MessageHello {
		t.Fatalf("expected hello, got %s", msg.Type)
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageMetadataState, MetadataState: &protocol.MetadataState{PeerID: "peer-b", Folders: []protocol.MetadataFolderSummary{{FolderID: "docs", Cursor: 0, Files: 0, Tombstones: 0, StateHash: "remote-empty"}}}}); err != nil {
		t.Fatal(err)
	}
	first, err := codec.Read()
	if err != nil {
		t.Fatal(err)
	}
	if first.Type != protocol.MessageMetadataChanges || first.MetadataChanges == nil || !first.MetadataChanges.More || len(first.MetadataChanges.Changes) != 1 {
		t.Fatalf("expected first chunked metadata batch with more=true, got %+v", first)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if second, err := codec.Read(); err == nil {
		t.Fatalf("server sent second metadata batch before ack: %+v", second)
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("expected read timeout while server waits for ack, got %v", err)
	}
	if err := clientConn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageMetadataAck, MetadataAck: &protocol.MetadataAck{FolderID: first.MetadataChanges.FolderID, FromCursor: first.MetadataChanges.FromCursor, ToCursor: first.MetadataChanges.ToCursor, StateHash: first.MetadataChanges.StateHash}}); err != nil {
		t.Fatal(err)
	}
	second, err := codec.Read()
	if err != nil {
		t.Fatal(err)
	}
	if second.Type != protocol.MessageMetadataChanges || second.MetadataChanges == nil || second.MetadataChanges.More || len(second.MetadataChanges.Changes) != 1 {
		t.Fatalf("expected final metadata batch after ack, got %+v", second)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestServerMetadataStateRecordsRemoteAndRepliesWithLocalChanges(t *testing.T) {
	remoteRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	hash := sha256.Sum256([]byte("server block"))
	if err := serverStore.SaveManifest("docs", "server.txt", block.Manifest{Path: "server.txt", Size: 12, BlockSize: 12, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 12, Hash: hash[:]}}}); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()
	codec := protocol.NewCodec(clientConn, clientConn)
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: &protocol.Hello{ProtocolVersion: 1, NodeID: "peer-b", EncryptionLevel: 0}}); err != nil {
		t.Fatal(err)
	}
	if msg, err := codec.Read(); err != nil {
		t.Fatal(err)
	} else if msg.Type != protocol.MessageHello {
		t.Fatalf("expected hello, got %s", msg.Type)
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageMetadataState, MetadataState: &protocol.MetadataState{PeerID: "peer-b", Folders: []protocol.MetadataFolderSummary{{FolderID: "docs", Cursor: 0, Files: 0, Tombstones: 0, StateHash: "remote-empty"}}}}); err != nil {
		t.Fatal(err)
	}
	msg, err := codec.Read()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != protocol.MessageMetadataChanges || msg.MetadataChanges == nil {
		t.Fatalf("expected metadata changes response, got %+v", msg)
	}
	if msg.MetadataChanges.FolderID != "docs" || msg.MetadataChanges.FromCursor != 0 || len(msg.MetadataChanges.Changes) != 1 || msg.MetadataChanges.Changes[0].Kind != protocol.MetadataChangeUpsert {
		t.Fatalf("unexpected metadata changes: %+v", msg.MetadataChanges)
	}
	statuses, err := serverStore.PeerFolderStatuses("peer-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].PeerCursor != 0 || statuses[0].PeerStateHash != "remote-empty" || statuses[0].InSync {
		t.Fatalf("remote metadata state was not recorded as out-of-sync: %+v", statuses)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestServerMetadataStateReportsFullRefreshWhenPeerCursorWasCompacted(t *testing.T) {
	remoteRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	alpha := block.Manifest{Path: "alpha.txt", Size: 3, BlockSize: 3, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{1}}}}
	if err := serverStore.SaveManifest("docs", "alpha.txt", alpha); err != nil {
		t.Fatal(err)
	}
	if err := serverStore.DeleteManifest("docs", "alpha.txt"); err != nil {
		t.Fatal(err)
	}
	current, err := serverStore.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := serverStore.SavePeerFolderState("peer-current", current); err != nil {
		t.Fatal(err)
	}
	if _, err := serverStore.CompactFolderMetadata("docs", state.MetadataCompactionPolicy{PeerIDs: []string{"peer-current"}}); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()
	codec := protocol.NewCodec(clientConn, clientConn)
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: &protocol.Hello{ProtocolVersion: 1, NodeID: "peer-b", EncryptionLevel: 0}}); err != nil {
		t.Fatal(err)
	}
	if msg, err := codec.Read(); err != nil {
		t.Fatal(err)
	} else if msg.Type != protocol.MessageHello {
		t.Fatalf("expected hello, got %s", msg.Type)
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageMetadataState, MetadataState: &protocol.MetadataState{PeerID: "peer-b", Folders: []protocol.MetadataFolderSummary{{FolderID: "docs", Cursor: 0, StateHash: "stale"}}}}); err != nil {
		t.Fatal(err)
	}
	msg, err := codec.Read()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != protocol.MessageError || msg.Error == nil || msg.Error.Code != "metadata_full_refresh_required" {
		t.Fatalf("expected metadata_full_refresh_required error, got %+v", msg)
	}
	if !strings.Contains(msg.Error.Message, "full refresh") || !strings.Contains(msg.Error.Message, "docs") {
		t.Fatalf("error should explain full refresh folder, got %+v", msg.Error)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderRepairsCompactedPeerMetadataWithFullRefreshIndex(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	if err := os.WriteFile(filepath.Join(remoteRoot, "current.txt"), []byte("current data"), 0o600); err != nil {
		t.Fatal(err)
	}
	currentHash := sha256.Sum256([]byte("current data"))
	if err := serverStore.SaveManifest("docs", "current.txt", block.Manifest{Path: "current.txt", Size: 12, BlockSize: 12, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 12, Hash: currentHash[:]}}}); err != nil {
		t.Fatal(err)
	}
	if err := serverStore.SaveManifest("docs", "gone.txt", block.Manifest{Path: "gone.txt", Size: 4, BlockSize: 4, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{1}}}}); err != nil {
		t.Fatal(err)
	}
	if err := serverStore.DeleteManifest("docs", "gone.txt"); err != nil {
		t.Fatal(err)
	}
	serverSummary, err := serverStore.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := serverStore.SavePeerFolderState("peer-current", serverSummary); err != nil {
		t.Fatal(err)
	}
	if _, err := serverStore.CompactFolderMetadata("docs", state.MetadataCompactionPolicy{PeerIDs: []string{"peer-current"}}); err != nil {
		t.Fatal(err)
	}
	refreshedSummary, err := serverStore.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := clientStore.SavePeerFolderState("peer-a", state.FolderSummary{FolderID: "docs", Cursor: 0, StateHash: "stale"}); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "peer-a", BlockSize: 12, Folders: map[string]string{"docs": remoteRoot}, MetadataStore: serverStore})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "peer-b", FolderID: "docs", LocalRoot: localRoot, MetadataStore: clientStore})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.MetadataFullRefreshes != 1 {
		t.Fatalf("MetadataFullRefreshes = %d, want 1", result.MetadataFullRefreshes)
	}
	manifest, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", "current.txt")
	if err != nil || !ok {
		t.Fatalf("full-refresh peer manifest missing: ok=%v err=%v", ok, err)
	}
	if manifest.Size != 12 {
		t.Fatalf("unexpected refreshed manifest: %+v", manifest)
	}
	vector, err := clientStore.PeerStateVector("peer-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(vector.Folders) != 1 || vector.Folders[0].Cursor != refreshedSummary.Cursor || vector.Folders[0].StateHash != refreshedSummary.StateHash {
		t.Fatalf("full refresh did not adopt remote summary: got %+v want %+v", vector, refreshedSummary)
	}
	if data, err := os.ReadFile(filepath.Join(localRoot, "current.txt")); err != nil || string(data) != "current data" {
		t.Fatalf("file transfer after full refresh failed: data=%q err=%v", string(data), err)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderExchangesTrustedPeerGraphAfterHandshake(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("peer graph exchange"), 0o600); err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{
		NodeID:    "peer-a",
		BlockSize: 8,
		Folders:   map[string]string{"docs": remoteRoot},
		KnownPeers: []discovery.Peer{
			{ID: "peer-c", Addresses: []string{"tcp://10.0.0.3:22420"}},
		},
		IdentityGroups: []discovery.IdentityGroupState{{
			GroupID:       "family-sync",
			SharedFolders: []discovery.FolderAdvertisement{{ID: "photos", Label: "Photo Library"}},
		}},
	})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{
		NodeID:    "peer-b",
		FolderID:  "docs",
		LocalRoot: localRoot,
		KnownPeers: []discovery.Peer{
			{ID: "peer-d", Addresses: []string{"tcp://10.0.0.4:22420"}},
		},
		IdentityGroups: []discovery.IdentityGroupState{{
			GroupID:       "family-sync",
			SharedFolders: []discovery.FolderAdvertisement{{ID: "docs", Label: "Documents"}},
		}},
	})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if len(result.LearnedPeers) != 1 || result.LearnedPeers[0].ID != "peer-c" {
		t.Fatalf("LearnedPeers = %+v, want peer-c from server graph", result.LearnedPeers)
	}
	if len(result.LearnedFolders) != 1 || result.LearnedFolders[0].ID != "photos" || result.LearnedFolders[0].Enabled {
		t.Fatalf("LearnedFolders = %+v, want disabled photos folder advertisement", result.LearnedFolders)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderExchangesSnapshotMarkersAcrossIdentityGroupWithoutArchiveJobs(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("snapshot marker sync"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	marker := state.SnapshotMarker{ID: "snap-remote", FolderID: "docs", Cursor: 7, StateHash: "remote-hash", CreatedAt: "2026-05-25T03:00:00Z", Description: "remote snapshot"}
	if err := serverStore.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}
	archiveRoot := filepath.Join(t.TempDir(), "archive")
	checkpointRoot := filepath.Join(t.TempDir(), "checkpoints")
	checkpointPath := filepath.Join(checkpointRoot, "docs", "snap-remote.json")
	if err := os.MkdirAll(filepath.Dir(checkpointPath), 0o755); err != nil {
		t.Fatalf("mkdir checkpoint: %v", err)
	}
	if err := os.WriteFile(checkpointPath, []byte(`{"snapshot":"snap-remote"}`), 0o644); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	archivedHash := sha256.Sum256([]byte("archived snapshot block"))
	archivedBlock := block.Block{Index: 0, Offset: 0, Size: len("archived snapshot block"), Hash: archivedHash[:]}
	if err := serverStore.SaveArchiveIntakeJobs("snap-remote", []state.ArchiveIntakeJob{{ID: "job-archived", SnapshotID: "snap-remote", FolderID: "docs", Path: "doc.txt", Block: archivedBlock, Status: "archived", CreatedAt: "2026-05-25T03:00:00Z"}}); err != nil {
		t.Fatalf("SaveArchiveIntakeJobs: %v", err)
	}
	hexHash := hex.EncodeToString(archivedHash[:])
	archivePath := filepath.Join(archiveRoot, "blocks", hexHash[:2], hexHash)
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir archive block: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("archived snapshot block"), 0o644); err != nil {
		t.Fatalf("write archive block: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{
		NodeID:                 "peer-a",
		BlockSize:              8,
		Folders:                map[string]string{"docs": remoteRoot},
		SnapshotStore:          serverStore,
		SnapshotArchiveRoot:    archiveRoot,
		SnapshotCheckpointRoot: checkpointRoot,
		IdentityGroups: []discovery.IdentityGroupState{{
			GroupID:       "family-sync",
			SharedFolders: []discovery.FolderAdvertisement{{ID: "docs", Label: "Documents"}},
		}},
	})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{
		NodeID:        "peer-b",
		FolderID:      "docs",
		LocalRoot:     localRoot,
		SnapshotStore: clientStore,
		IdentityGroups: []discovery.IdentityGroupState{{
			GroupID:       "family-sync",
			SharedFolders: []discovery.FolderAdvertisement{{ID: "docs", Label: "Documents"}},
		}},
	})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	if result.SnapshotMarkersLearned != 1 {
		t.Fatalf("SnapshotMarkersLearned=%d, want 1", result.SnapshotMarkersLearned)
	}
	loaded, ok, err := clientStore.LoadSnapshotMarker("snap-remote")
	if err != nil || !ok {
		t.Fatalf("LoadSnapshotMarker: marker=%+v ok=%v err=%v", loaded, ok, err)
	}
	if loaded.FolderID != "docs" || loaded.Cursor != 7 || loaded.Description != "remote snapshot" {
		t.Fatalf("loaded marker mismatch: %+v", loaded)
	}
	if !loaded.ArchiveFullyProtected {
		t.Fatalf("learned marker should preserve verified archive-block availability from backup peer: %+v", loaded)
	}
	if !loaded.DBCheckpointAvailable {
		t.Fatalf("learned marker should preserve verified offline DB-checkpoint availability from backup peer: %+v", loaded)
	}
	jobs, err := clientStore.ListArchiveIntakeJobs("snap-remote")
	if err != nil {
		t.Fatalf("ListArchiveIntakeJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("snapshot metadata exchange should not create archive jobs on non-backup peer: %+v", jobs)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestPullFolderRetainsOverwrittenStreamBytesForBackupIntake(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("stream replacement bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "doc.txt"), []byte("old stream snapshot bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	if result.FilesWritten != 1 {
		t.Fatalf("expected one stream overwrite, got %+v", result)
	}
	assertStreamFile(t, filepath.Join(localRoot, "doc.txt"), "stream replacement bytes")
	assertStreamBackupIntakeFile(t, localRoot, "doc.txt", "old stream snapshot bytes")
}

func TestPullFolderRetainsStaleDeletedStreamBytesForBackupIntake(t *testing.T) {
	remoteRoot := t.TempDir()
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "doc.txt"), []byte("stream live bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localRoot, "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "stale", "old.txt"), []byte("deleted stream snapshot bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runPipePull(t, remoteRoot, localRoot, 5)

	if result.FilesDeleted != 1 {
		t.Fatalf("expected one stream stale delete, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "stale", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale file deleted, stat err=%v", err)
	}
	assertStreamBackupIntakeFile(t, localRoot, "stale/old.txt", "deleted stream snapshot bytes")
}

func assertStreamFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

func assertStreamBackupIntakeFile(t *testing.T, root, rel, want string) {
	t.Helper()
	intakeRoot := filepath.Join(root, ".sync", "backup-intake")
	var matches []string
	err := filepath.WalkDir(intakeRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		fromIntake, err := filepath.Rel(intakeRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(fromIntake, string(filepath.Separator))
		if len(parts) >= 2 && filepath.ToSlash(filepath.Join(parts[1:]...)) == rel {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("backup intake matches for %s = %v, want one", rel, matches)
	}
	assertStreamFile(t, matches[0], want)
}

func serveRelayIndexOnlyStream(stream io.ReadWriteCloser, root string, blockRequests chan<- struct{}) error {
	defer stream.Close()
	codec := protocol.NewCodec(stream, stream)
	msg, err := codec.Read()
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageHello {
		return fmt.Errorf("expected hello, got %s", msg.Type)
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: &protocol.Hello{ProtocolVersion: 1, NodeID: "relay-peer", EncryptionLevel: msg.Hello.EncryptionLevel}}); err != nil {
		return err
	}
	msg, err = codec.Read()
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageFolderIndex || msg.FolderIndex == nil {
		return fmt.Errorf("expected folder index request, got %s", msg.Type)
	}
	index, err := scanner.ScanFolder(root, scanner.Options{BlockSize: 64})
	if err != nil {
		return err
	}
	files := make([]protocol.FolderIndexFile, 0, len(index.Files))
	for _, file := range index.Files {
		manifest := file.Manifest
		manifest.Path = file.RelativePath
		files = append(files, protocol.FolderIndexFile{RelativePath: file.RelativePath, Manifest: manifest})
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageFolderIndex, FolderIndex: &protocol.FolderIndex{FolderID: msg.FolderIndex.FolderID, Files: files}}); err != nil {
		return err
	}
	for {
		msg, err = codec.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if msg.Type == protocol.MessageBlockRequest {
			select {
			case blockRequests <- struct{}{}:
			default:
			}
			return codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "relay_block_used", Message: "relay block source should not be selected"}})
		}
	}
}

func runPipePull(t *testing.T, remoteRoot, localRoot string, blockSize int) PullResult {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	server := NewServer(ServerConfig{NodeID: "node-a", BlockSize: blockSize, Folders: map[string]string{"docs": remoteRoot}})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(context.Background(), serverConn)
	}()

	result, err := PullFolder(context.Background(), clientConn, PullOptions{NodeID: "node-b", FolderID: "docs", LocalRoot: localRoot})
	if err != nil {
		t.Fatalf("PullFolder: %v", err)
	}
	clientConn.Close()
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
	return result
}
