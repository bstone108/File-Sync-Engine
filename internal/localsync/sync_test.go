package localsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestSyncOneWayCopiesUpdatesBeforeDeletingStaleFiles(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "docs", "a.txt"), "aaaabbbbcccc")
	mustWrite(t, filepath.Join(source, "new.txt"), "new-file")
	mustWrite(t, filepath.Join(target, "docs", "a.txt"), "old-data")
	mustWrite(t, filepath.Join(target, "stale.txt"), "delete-me")

	result, err := SyncOneWay(source, target, Options{BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(target, "docs", "a.txt"), "aaaabbbbcccc")
	assertFile(t, filepath.Join(target, "new.txt"), "new-file")
	if _, err := os.Stat(filepath.Join(target, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale file deleted, stat err=%v", err)
	}
	if result.Writes != 2 || result.Deletes != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Steps) != 3 || result.Steps[0].Kind != StepWrite || result.Steps[1].Kind != StepWrite || result.Steps[2].Kind != StepDelete {
		t.Fatalf("expected writes before delete, got %+v", result.Steps)
	}
}

func TestSyncOneWayReusesExistingShiftedTargetBlocks(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "data.bin"), "ccccaaaabbbb")
	mustWrite(t, filepath.Join(target, "library.bin"), "aaaabbbbcccc")

	result, err := SyncOneWay(source, target, Options{BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(target, "data.bin"), "ccccaaaabbbb")
	if result.ReusedBlocks < 3 {
		t.Fatalf("expected shifted/cross-file block reuse, got %+v", result)
	}
}

func TestSyncOneWayRenamesMatchingStaleTargetBeforeDeleting(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "renamed", "report.txt"), "same content after rename")
	mustWrite(t, filepath.Join(target, "old", "report.txt"), "same content after rename")

	result, err := SyncOneWay(source, target, Options{BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(target, "renamed", "report.txt"), "same content after rename")
	if _, err := os.Stat(filepath.Join(target, "old", "report.txt")); !os.IsNotExist(err) {
		t.Fatalf("old renamed path should be gone after move detection, stat err=%v", err)
	}
	if result.Moves != 1 || result.Writes != 0 || result.Deletes != 0 {
		t.Fatalf("expected one move without write/delete churn, got %+v", result)
	}
	if len(result.Steps) != 1 || result.Steps[0].Kind != StepMove || result.Steps[0].Path != "renamed/report.txt" {
		t.Fatalf("expected move step for destination path, got %+v", result.Steps)
	}
}

func TestSyncOneWaySkipsTargetIgnoredWritesAndReuseCandidates(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "cache", "remote.tmp"), "source ignored by target")
	mustWrite(t, filepath.Join(source, "data.bin"), "aaaabbbb")
	mustWrite(t, filepath.Join(target, ".sync", "ignore"), "cache/**\n")
	mustWrite(t, filepath.Join(target, "cache", "local.tmp"), "preserve ignored local data")
	mustWrite(t, filepath.Join(target, "cache", "seed.bin"), "aaaabbbb")

	result, err := SyncOneWay(source, target, Options{BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(target, "data.bin"), "aaaabbbb")
	assertFile(t, filepath.Join(target, "cache", "local.tmp"), "preserve ignored local data")
	assertFile(t, filepath.Join(target, "cache", "seed.bin"), "aaaabbbb")
	if _, err := os.Stat(filepath.Join(target, "cache", "remote.tmp")); !os.IsNotExist(err) {
		t.Fatalf("target-ignored source path should not be written, stat err=%v", err)
	}
	if result.Writes != 1 || result.Deletes != 0 || result.ReusedBlocks != 0 {
		t.Fatalf("ignored target paths should not be write/delete/reuse candidates: %+v", result)
	}
}

func TestSyncOneWayDoesNotDeleteStaleFilesWhenAWriteFails(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "blocked"), "new content")
	mustWrite(t, filepath.Join(target, "stale.txt"), "must stay until writes succeed")
	if err := os.MkdirAll(filepath.Join(target, "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncOneWay(source, target, Options{BlockSize: 4}); err == nil {
		t.Fatal("expected write failure when target path is an existing directory")
	}
	assertFile(t, filepath.Join(target, "stale.txt"), "must stay until writes succeed")
	if info, err := os.Stat(filepath.Join(target, "blocked")); err != nil || !info.IsDir() {
		t.Fatalf("existing directory at failed write path was not preserved: info=%v err=%v", info, err)
	}
}

func TestSyncOneWayCleansInterruptedStagingFilesBeforeScanning(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "doc.txt"), "source data")
	mustWrite(t, filepath.Join(source, ".doc.txt.12345.staging"), "interrupted temp")

	result, err := SyncOneWay(source, target, Options{BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}

	if result.Writes != 1 {
		t.Fatalf("expected only real source file written, got %+v", result)
	}
	assertFile(t, filepath.Join(target, "doc.txt"), "source data")
	if _, err := os.Stat(filepath.Join(source, ".doc.txt.12345.staging")); !os.IsNotExist(err) {
		t.Fatalf("interrupted source staging file was not cleaned up: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".doc.txt.12345.staging")); !os.IsNotExist(err) {
		t.Fatalf("interrupted staging file was copied to target: %v", err)
	}
}

func TestSyncOneWayRetainsOverwrittenTargetBytesForBackupIntake(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "docs", "report.txt"), "new snapshot bytes")
	mustWrite(t, filepath.Join(target, "docs", "report.txt"), "old snapshot bytes")

	result, err := SyncOneWay(source, target, Options{BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}

	if result.Writes != 1 {
		t.Fatalf("expected one overwrite, got %+v", result)
	}
	assertFile(t, filepath.Join(target, "docs", "report.txt"), "new snapshot bytes")
	assertBackupIntakeFile(t, target, "docs/report.txt", "old snapshot bytes")
}

func TestSyncOneWayRetainsStaleDeletedBytesForBackupIntake(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "keep.txt"), "current bytes")
	mustWrite(t, filepath.Join(target, "stale", "old.txt"), "deleted snapshot bytes")

	result, err := SyncOneWay(source, target, Options{BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}

	if result.Deletes != 1 {
		t.Fatalf("expected one stale delete, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(target, "stale", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale file deleted, stat err=%v", err)
	}
	assertBackupIntakeFile(t, target, "stale/old.txt", "deleted snapshot bytes")
}

func TestSyncOneWayCreatesUniqueExtensionPreservingConflictCopy(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "docs", "report.txt"), "source edit")
	mustWrite(t, filepath.Join(target, "docs", "report.txt"), "target edit")
	mustWrite(t, filepath.Join(target, "docs", "report.sync-conflict-node-b.txt"), "older conflict")

	result, err := SyncOneWay(source, target, Options{
		BlockSize:               4,
		PreserveTargetConflicts: true,
		ConflictSuffix:          ".sync-conflict-node-b",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Conflicts != 1 || result.Writes != 1 {
		t.Fatalf("unexpected conflict result: %+v", result)
	}
	assertFile(t, filepath.Join(target, "docs", "report.txt"), "source edit")
	assertFile(t, filepath.Join(target, "docs", "report.sync-conflict-node-b.txt"), "older conflict")
	assertFile(t, filepath.Join(target, "docs", "report.sync-conflict-node-b-1.txt"), "target edit")
}

func TestSyncOneWayAppliesSyncedSourceFileMode(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	sourcePath := filepath.Join(source, "docs", "report.txt")
	mustWrite(t, sourcePath, "source edit")
	if err := os.Chmod(sourcePath, 0o640); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(target, "docs", "report.txt"), "target edit")

	_, err := SyncOneWay(source, target, Options{
		BlockSize: 4,
		Permissions: config.PermissionPolicy{
			Mode: config.PermissionSync,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertMode(t, filepath.Join(target, "docs", "report.txt"), 0o640)
}

func TestSyncOneWayAppliesFixedFileAndDirectoryModes(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	mustWrite(t, filepath.Join(source, "docs", "report.txt"), "source edit")

	_, err := SyncOneWay(source, target, Options{
		BlockSize: 4,
		Permissions: config.PermissionPolicy{
			Mode:     config.PermissionFixed,
			FileMode: "0600",
			DirMode:  "0700",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertMode(t, filepath.Join(target, "docs"), 0o700)
	assertMode(t, filepath.Join(target, "docs", "report.txt"), 0o600)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

func assertBackupIntakeFile(t *testing.T, root, rel, want string) {
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
	assertFile(t, matches[0], want)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
