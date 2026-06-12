package lockedapply

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

func TestPersistPendingApplyStoresVerifiedBlocksInHiddenCache(t *testing.T) {
	root := t.TempDir()
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	data := []byte("locked target replacement data")
	hash := sha256.Sum256(data)
	manifest := block.Manifest{
		Path:      "locked.txt",
		Size:      int64(len(data)),
		BlockSize: len(data),
		Blocks: []block.Block{{
			Index:  0,
			Offset: 0,
			Size:   len(data),
			Hash:   hash[:],
		}},
	}

	cached, err := Persist(PersistOptions{
		Root:     root,
		Store:    store,
		FolderID: "docs",
		Path:     "locked.txt",
		Manifest: manifest,
		Blocks: []BlockData{{
			Index: 0,
			Data:  data,
		}},
		Reason: "target locked",
	})
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if cached.Path == "" || !bytes.HasPrefix([]byte(cached.Path), []byte(filepath.Join(root, ".sync", "locked-apply"))) {
		t.Fatalf("cache path %q is not under hidden locked-apply cache", cached.Path)
	}
	if cached.Blocks != 1 {
		t.Fatalf("cached blocks = %d", cached.Blocks)
	}
	stored, err := os.ReadFile(filepath.Join(cached.Path, "block-000000"))
	if err != nil {
		t.Fatalf("read cached block: %v", err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatalf("cached block bytes = %q", stored)
	}

	write, ok, err := state.NewJSONStore(filepath.Join(root, "state.json")).PendingWrite("docs", "locked.txt")
	if err != nil {
		t.Fatalf("PendingWrite reload: %v", err)
	}
	if !ok {
		t.Fatalf("pending write was not persisted")
	}
	if write.Committed {
		t.Fatalf("pending locked apply should not be marked committed")
	}
	if write.Reason != "target locked" {
		t.Fatalf("reason marker = %q", write.Reason)
	}
	if len(write.VerifiedBlocks) != 1 || write.VerifiedBlocks[0].Index != 0 || write.VerifiedBlocks[0].Size != len(data) || !bytes.Equal(write.VerifiedBlocks[0].Hash, hash[:]) {
		t.Fatalf("verified blocks = %+v", write.VerifiedBlocks)
	}
}

func TestPersistPendingApplyRejectsUnsafePathsAndHashMismatches(t *testing.T) {
	root := t.TempDir()
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	manifest := block.Manifest{Path: "../escape.txt", Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte("wrong")}}}
	if _, err := Persist(PersistOptions{Root: root, Store: store, FolderID: "docs", Path: "../escape.txt", Manifest: manifest, Blocks: []BlockData{{Index: 0, Data: []byte("abc")}}}); err == nil {
		t.Fatalf("expected unsafe path error")
	}
	if _, err := Persist(PersistOptions{Root: root, Store: store, FolderID: "docs", Path: "safe.txt", Manifest: manifest, Blocks: []BlockData{{Index: 0, Data: []byte("abc")}}}); err == nil {
		t.Fatalf("expected hash mismatch error")
	}
	if _, err := os.Stat(filepath.Join(root, ".sync", "locked-apply")); !os.IsNotExist(err) {
		t.Fatalf("cache directory should not be left behind after rejected inputs: %v", err)
	}
}

func TestRetryPendingApplyAfterRestartCreatesConflictWhenTargetChanged(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "state.json")
	targetPath := filepath.Join(root, "locked.txt")
	if err := os.WriteFile(targetPath, []byte("old base"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseManifest, err := block.BuildManifest(targetPath, 8)
	if err != nil {
		t.Fatalf("base manifest: %v", err)
	}
	baseManifest.Path = "locked.txt"
	replacement := []byte("replacement bytes")
	if _, err := Persist(PersistOptions{
		Root:                 root,
		Store:                state.NewJSONStore(storePath),
		FolderID:             "docs",
		Path:                 "locked.txt",
		Manifest:             manifestForData("locked.txt", replacement),
		ExpectedBaseManifest: &baseManifest,
		Blocks:               []BlockData{{Index: 0, Data: replacement}},
		Reason:               "target locked",
	}); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("user changed while locked"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := RetryPending(RetryOptions{Root: root, Store: state.NewJSONStore(storePath), FolderID: "docs", Path: "locked.txt"})
	if err != nil {
		t.Fatalf("RetryPending: %v", err)
	}
	if !result.Applied || !result.ConflictCreated || result.BaseMismatch {
		t.Fatalf("retry result = %+v", result)
	}
	assertBytes(t, targetPath, "replacement bytes")
	assertBytes(t, filepath.Join(root, "locked.sync-conflict-locked-apply.txt"), "user changed while locked")
	write, ok, err := state.NewJSONStore(storePath).PendingWrite("docs", "locked.txt")
	if err != nil || !ok {
		t.Fatalf("pending write after retry: ok=%v err=%v", ok, err)
	}
	if !write.Committed {
		t.Fatalf("pending write was not marked committed after conflict retry")
	}
}

func TestRetryPendingApplyAfterRestartAppliesOnlyWhenExpectedBaseMatches(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "state.json")
	store := state.NewJSONStore(storePath)
	targetPath := filepath.Join(root, "locked.txt")
	if err := os.WriteFile(targetPath, []byte("old base"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseManifest, err := block.BuildManifest(targetPath, 8)
	if err != nil {
		t.Fatalf("base manifest: %v", err)
	}
	baseManifest.Path = "locked.txt"
	replacement := []byte("replacement bytes")
	replacementManifest := manifestForData("locked.txt", replacement)
	if _, err := Persist(PersistOptions{
		Root:                 root,
		Store:                store,
		FolderID:             "docs",
		Path:                 "locked.txt",
		Manifest:             replacementManifest,
		ExpectedBaseManifest: &baseManifest,
		Blocks:               []BlockData{{Index: 0, Data: replacement}},
		Reason:               "target locked",
	}); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	result, err := RetryPending(RetryOptions{Root: root, Store: state.NewJSONStore(storePath), FolderID: "docs", Path: "locked.txt"})
	if err != nil {
		t.Fatalf("RetryPending: %v", err)
	}
	if !result.Applied || result.BaseMismatch {
		t.Fatalf("retry result = %+v", result)
	}
	bytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != string(replacement) {
		t.Fatalf("target bytes = %q", bytes)
	}
	write, ok, err := state.NewJSONStore(storePath).PendingWrite("docs", "locked.txt")
	if err != nil || !ok {
		t.Fatalf("pending write after retry: ok=%v err=%v", ok, err)
	}
	if !write.Committed {
		t.Fatalf("pending write was not marked committed after successful retry")
	}

}

func assertBytes(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s bytes = %q, want %q", path, got, want)
	}
}

func manifestForData(path string, data []byte) block.Manifest {
	hash := sha256.Sum256(data)
	return block.Manifest{Path: path, Size: int64(len(data)), BlockSize: len(data), HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: len(data), Hash: hash[:]}}}
}
