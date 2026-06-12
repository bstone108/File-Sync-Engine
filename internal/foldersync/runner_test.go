package foldersync

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestRunnerSyncsSendOnlyFolderToReceiveOnlyFolderInSameGroup(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "doc.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := New([]Folder{
		{ID: "source", Path: source, SyncGroup: "docs", Mode: config.ModeSendOnly, BlockSize: 4096},
		{ID: "target", Path: target, SyncGroup: "docs", Mode: config.ModeReceiveOnly, BlockSize: 4096},
	})
	result, err := runner.ScanDue("source")
	if err != nil {
		t.Fatal(err)
	}
	if result.Writes != 1 || result.Deletes != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertFile(t, filepath.Join(target, "doc.txt"), "hello")

	if err := os.WriteFile(filepath.Join(source, "doc.txt"), []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "stale.txt"), []byte("remove me"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = runner.ScanDue("source")
	if err != nil {
		t.Fatal(err)
	}
	if result.Writes != 1 || result.Deletes != 1 {
		t.Fatalf("unexpected update/delete result: %+v", result)
	}
	assertFile(t, filepath.Join(target, "doc.txt"), "updated")
	if _, err := os.Stat(filepath.Join(target, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file still present or stat failed: %v", err)
	}
}

func TestRunnerDoesNotPushReceiveOnlyChangesBackToSendOnlySource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "doc.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "doc.txt"), []byte("local drift"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := New([]Folder{
		{ID: "source", Path: source, SyncGroup: "docs", Mode: config.ModeSendOnly, BlockSize: 4096},
		{ID: "target", Path: target, SyncGroup: "docs", Mode: config.ModeReceiveOnly, BlockSize: 4096},
	})
	result, err := runner.ScanDue("target")
	if err != nil {
		t.Fatal(err)
	}
	if result.Writes != 0 || result.Deletes != 0 {
		t.Fatalf("receive-only folder should not push changes: %+v", result)
	}
	assertFile(t, filepath.Join(source, "doc.txt"), "source")
}

func TestRunnerPreservesSendRecvDivergentTargetBeforeApplyingSource(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "doc.txt"), []byte("edit from a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "doc.txt"), []byte("edit from b"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := New([]Folder{
		{ID: "node-a", Path: a, SyncGroup: "docs", Mode: config.ModeSendReceive, BlockSize: 4096},
		{ID: "node-b", Path: b, SyncGroup: "docs", Mode: config.ModeSendReceive, BlockSize: 4096},
	})
	result, err := runner.ScanDue("node-a")
	if err != nil {
		t.Fatal(err)
	}
	if result.Writes != 1 || result.Conflicts != 1 {
		t.Fatalf("unexpected sendrecv conflict result: %+v", result)
	}
	assertFile(t, filepath.Join(b, "doc.txt"), "edit from a")
	assertFile(t, filepath.Join(b, "doc.sync-conflict-node-b.txt"), "edit from b")
}

func TestRunnerReportsInaccessibleSourceAndTargetWarnings(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "doc.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-source", filepath.Join(source, "locked-source.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(target, "locked-target.txt")); err != nil {
		t.Fatal(err)
	}

	runner := New([]Folder{
		{ID: "source", Path: source, SyncGroup: "docs", Mode: config.ModeSendOnly, BlockSize: 4096},
		{ID: "target", Path: target, SyncGroup: "docs", Mode: config.ModeReceiveOnly, BlockSize: 4096},
	})

	result, err := runner.ScanDue("source")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Inaccessible) != 2 {
		t.Fatalf("inaccessible warnings = %+v, want source and target warnings", result.Inaccessible)
	}
	if result.Inaccessible[0].FolderID != "source" || result.Inaccessible[0].Role != "source" || result.Inaccessible[0].Path != "locked-source.txt" {
		t.Fatalf("source warning not reported: %+v", result.Inaccessible)
	}
	if result.Inaccessible[1].FolderID != "target" || result.Inaccessible[1].Role != "target" || result.Inaccessible[1].Path != "locked-target.txt" {
		t.Fatalf("target warning not reported: %+v", result.Inaccessible)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}
