package backup

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestExecuteMirrorUpdateCopiesCurrentFilesAndDeletesStaleMirrorFiles(t *testing.T) {
	sourceRoot := t.TempDir()
	mirrorRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "docs/current.txt", "current bytes")
	writeTestFile(t, sourceRoot, "nested/keep.bin", "keep bytes")
	writeTestFile(t, mirrorRoot, "stale.txt", "old bytes")
	writeTestFile(t, mirrorRoot, ".sync/metadata.json", "engine metadata")

	plan := SnapshotProtectionPlan{Mode: config.BackupModeMirrorPlusArchive, MirrorFiles: []string{"docs/current.txt", "nested/keep.bin"}}
	result, err := ExecuteMirrorUpdate(sourceRoot, mirrorRoot, plan)
	if err != nil {
		t.Fatalf("execute mirror update: %v", err)
	}

	if result.Copied != 2 || result.Deleted != 1 {
		t.Fatalf("unexpected mirror result: %+v", result)
	}
	assertFileContent(t, mirrorRoot, "docs/current.txt", "current bytes")
	assertFileContent(t, mirrorRoot, "nested/keep.bin", "keep bytes")
	if _, err := os.Stat(filepath.Join(mirrorRoot, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale mirror file should be deleted, err=%v", err)
	}
	assertFileContent(t, mirrorRoot, ".sync/metadata.json", "engine metadata")
}

func TestExecuteMirrorUpdateDoesNothingForBlockArchiveOnlyPlans(t *testing.T) {
	sourceRoot := t.TempDir()
	mirrorRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "current.txt", "current bytes")
	writeTestFile(t, mirrorRoot, "existing.txt", "existing bytes")

	result, err := ExecuteMirrorUpdate(sourceRoot, mirrorRoot, SnapshotProtectionPlan{Mode: config.BackupModeBlockArchiveOnly})
	if err != nil {
		t.Fatalf("execute mirror update: %v", err)
	}
	if result.Copied != 0 || result.Deleted != 0 {
		t.Fatalf("block-archive-only should not mutate mirror state: %+v", result)
	}
	assertFileContent(t, mirrorRoot, "existing.txt", "existing bytes")
	if _, err := os.Stat(filepath.Join(mirrorRoot, "current.txt")); !os.IsNotExist(err) {
		t.Fatalf("block-archive-only should not copy current file, err=%v", err)
	}
}

func TestExecuteMirrorUpdateRejectsMirrorRootInsideSourceRoot(t *testing.T) {
	sourceRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "current.txt", "current bytes")
	mirrorRoot := filepath.Join(sourceRoot, "mirror")
	if _, err := ExecuteMirrorUpdate(sourceRoot, mirrorRoot, SnapshotProtectionPlan{Mode: config.BackupModeMirrorPlusArchive, MirrorFiles: []string{"current.txt"}}); err == nil {
		t.Fatalf("expected mirror root inside source root to be rejected")
	}
	assertFileContent(t, sourceRoot, "current.txt", "current bytes")
}

func writeTestFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, root string, rel string, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", rel, got, want)
	}
}
