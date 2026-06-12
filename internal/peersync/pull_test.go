package peersync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/scanner"
)

func TestPullFolderDownloadsRemoteFilesAndDeletesStaleLocalFiles(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("from peer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "stale.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if result.Writes != 1 || result.Deletes != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.BlocksFetched == 0 {
		t.Fatalf("expected block transfer count, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(local, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "from peer" {
		t.Fatalf("local doc = %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(local, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file still exists or stat failed: %v", err)
	}
}

func TestPullFolderUsesBlockEndpointForChangedFiles(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	payload := []byte("abcdefghijklmnopqrstuvwxyz0123456789")
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if result.BlocksFetched < 1 {
		t.Fatalf("expected block fetches, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(local, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("local doc = %q", string(data))
	}
}

func TestPullFolderWithOptionsPrefersDirectBlockSourceOverRelay(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	payload := []byte("direct peer should serve the block data")
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	remoteIndex, err := scanner.ScanFolder(remote, scanner.Options{BlockSize: 128 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	relayBlockRequests := 0
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/folder-index":
			_ = json.NewEncoder(w).Encode(remoteIndex)
		case "/v1/folder-block":
			relayBlockRequests++
			http.Error(w, "relay should not be used when direct block source is available", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer relayServer.Close()
	directServer := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	directHTTP := httptest.NewServer(directServer.Router())
	defer directHTTP.Close()

	result, err := PullFolderWithOptions(relayServer.URL, "secret", "docs", local, PullOptions{BlockSources: []BlockSource{
		{BaseURL: relayServer.URL, APIKey: "secret", Path: routing.RelayPath, Network: routing.WANNetwork, Reachable: true},
		{BaseURL: directHTTP.URL, APIKey: "secret", Path: routing.DirectPath, Network: routing.LocalNetwork, Reachable: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if relayBlockRequests != 0 {
		t.Fatalf("relay handled %d block requests despite direct source", relayBlockRequests)
	}
	if result.BlocksFetched != 1 {
		t.Fatalf("expected one fetched block from direct source, got %+v", result)
	}
	assertPeerFile(t, filepath.Join(local, "doc.txt"), string(payload))
}

func TestPullFolderWithOptionsEnforcesReceiveCapOnFetchedBlocks(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	payload := []byte("abcdefghijklmnopqrstuvwxyz0123456789")
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	started := time.Now()
	result, err := PullFolderWithOptions(httpServer.URL, "secret", "docs", local, PullOptions{ReceiveBytesPerSecond: int64(len(payload))})
	if err != nil {
		t.Fatal(err)
	}
	if result.BlocksFetched != 1 {
		t.Fatalf("expected one block fetch, got %+v", result)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Fatalf("manual HTTP pull completed too quickly for receive cap: %s", elapsed)
	}
}

func TestPullFolderReusesMatchingLocalBlocksBeforeFetching(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	shared := []byte("this block is already present locally")
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), shared, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "seed.txt"), shared, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "seed.txt"), shared, 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if result.BlocksFetched != 0 || result.BlocksReused == 0 {
		t.Fatalf("expected local reuse without network block fetch, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(local, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(shared) {
		t.Fatalf("local doc = %q", string(data))
	}
}

func TestPullFolderRenamesMatchingStaleLocalFileBeforeBlockDownloads(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	payload := []byte("same content at the peer's new path")
	if err := os.MkdirAll(filepath.Join(remote, "renamed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "renamed", "doc.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "old-doc.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesMoved != 1 || result.Writes != 0 || result.Deletes != 0 || result.BlocksFetched != 0 || result.BlocksReused != 0 {
		t.Fatalf("expected exact move reuse without writes/deletes/block transfer, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(local, "renamed", "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("renamed local file = %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(local, "old-doc.txt")); !os.IsNotExist(err) {
		t.Fatalf("old moved path still exists or stat failed: %v", err)
	}
}

func TestPullFolderFetchesMissingInShareIgnoreIncludeBeforePlanning(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(remote, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(local, ".sync"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, ".sync", "ignore"), []byte("#include rules/ignore-extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("from peer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "rules", "ignore-extra"), []byte("secret/**\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(remote, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "secret", "peer.tmp"), []byte("must not pull after include arrives"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if result.Writes != 1 {
		t.Fatalf("only non-ignored doc should be written after include fetch: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(local, "rules", "ignore-extra")); err != nil {
		t.Fatalf("missing in-share include was not fetched first: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "doc.txt")); err != nil {
		t.Fatalf("expected non-ignored remote doc to be pulled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "secret", "peer.tmp")); !os.IsNotExist(err) {
		t.Fatalf("path ignored by fetched include should not be pulled, stat err=%v", err)
	}
}

func TestPullFolderReportsUnavailableMissingInShareIgnoreIncludes(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, ".sync"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, ".sync", "ignore"), []byte("#include rules/missing-ignore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("from peer"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MissingIgnoreIncludes) != 1 || result.MissingIgnoreIncludes[0] != "rules/missing-ignore" {
		t.Fatalf("missing include status not reported: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(local, "doc.txt")); err != nil {
		t.Fatalf("sync should continue when peer cannot provide include: %v", err)
	}
}

func TestPullFolderSkipsLocallyIgnoredRemotePaths(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(remote, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(local, ".sync"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("from peer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "cache", "peer.tmp"), []byte("ignore this remote file locally"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, ".sync", "ignore"), []byte("cache/**\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if result.Writes != 1 {
		t.Fatalf("ignored remote file should not be written: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(local, "doc.txt")); err != nil {
		t.Fatalf("expected non-ignored remote doc to be pulled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "cache", "peer.tmp")); !os.IsNotExist(err) {
		t.Fatalf("locally ignored remote path should not be pulled, stat err=%v", err)
	}
}

func TestPullFolderCleansInterruptedTempFilesBeforePlanningDeletes(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("from peer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "old.txt.fse-peer-tmp"), []byte("interrupted temp"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deletes != 0 {
		t.Fatalf("interrupted temp cleanup should not be counted as stale delete: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(local, "old.txt.fse-peer-tmp")); !os.IsNotExist(err) {
		t.Fatalf("interrupted peer temp was not cleaned up: %v", err)
	}
}

func TestPullFolderRetainsOverwrittenLocalBytesForBackupIntake(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("from peer after overwrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "doc.txt"), []byte("old local snapshot bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}

	if result.Writes != 1 {
		t.Fatalf("expected one peer overwrite, got %+v", result)
	}
	assertPeerFile(t, filepath.Join(local, "doc.txt"), "from peer after overwrite")
	assertPeerBackupIntakeFile(t, local, "doc.txt", "old local snapshot bytes")
}

func TestPullFolderRetainsStaleDeletedBytesForBackupIntake(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("from peer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(local, "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "stale", "old.txt"), []byte("deleted peer snapshot bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Path: remote, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	result, err := PullFolder(httpServer.URL, "secret", "docs", local)
	if err != nil {
		t.Fatal(err)
	}

	if result.Deletes != 1 {
		t.Fatalf("expected one stale delete, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(local, "stale", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale file deleted, stat err=%v", err)
	}
	assertPeerBackupIntakeFile(t, local, "stale/old.txt", "deleted peer snapshot bytes")
}

func TestPullFolderLeavesExistingAndStaleFilesWhenBlockVerificationFails(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "doc.txt"), []byte("trusted peer data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "doc.txt"), []byte("keep existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "stale.txt"), []byte("do not delete until writes pass"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteIndex, err := scanner.ScanFolder(remote, scanner.Options{BlockSize: 128 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/folder-index":
			_ = json.NewEncoder(w).Encode(remoteIndex)
		case "/v1/folder-block":
			_, _ = w.Write([]byte("corrupted block bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, err := PullFolder(server.URL, "secret", "docs", local); err == nil {
		t.Fatal("expected hash verification error")
	}
	data, err := os.ReadFile(filepath.Join(local, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep existing" {
		t.Fatalf("existing file was replaced after failed verification: %q", data)
	}
	if _, err := os.Stat(filepath.Join(local, "stale.txt")); err != nil {
		t.Fatalf("stale file should not be deleted until all writes succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "doc.txt.fse-peer-tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary peer file was not cleaned up: %v", err)
	}
}

func assertPeerFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

func assertPeerBackupIntakeFile(t *testing.T, root, rel, want string) {
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
	assertPeerFile(t, matches[0], want)
}
