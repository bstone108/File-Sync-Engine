package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFolderBuildsRelativePathManifestsAndSkipsIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "skip.tmp"), []byte("ignore"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ScanFolder(root, Options{BlockSize: 4, IgnoreSuffixes: []string{".tmp"}})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files = %+v", result.Files)
	}
	if result.Files[0].RelativePath != "keep.txt" {
		t.Fatalf("relative path = %q", result.Files[0].RelativePath)
	}
	if len(result.Files[0].Manifest.Blocks) != 2 {
		t.Fatalf("manifest block count = %d", len(result.Files[0].Manifest.Blocks))
	}
}

func TestScanFolderLoadsSyncIgnoreAndNeverIndexesSyncMetadata(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".sync", "state"))
	mustMkdir(t, filepath.Join(root, "ignored"))
	mustWriteFile(t, filepath.Join(root, ".sync", "ignore"), "ignored/*.log\n!ignored/keep.log\n")
	mustWriteFile(t, filepath.Join(root, ".sync", "state", "db.json"), "metadata")
	mustWriteFile(t, filepath.Join(root, "ignored", "drop.log"), "drop")
	mustWriteFile(t, filepath.Join(root, "ignored", "keep.log"), "keep")
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "visible")

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"ignored/keep.log", "visible.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}

func TestScanFolderLoadsLocalSyncIgnoreIncludes(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".sync"))
	mustMkdir(t, filepath.Join(root, "rules"))
	mustMkdir(t, filepath.Join(root, "cache", "nested"))
	mustWriteFile(t, filepath.Join(root, ".sync", "ignore"), "# primary comment\n#include rules/extra.ignore\n")
	mustWriteFile(t, filepath.Join(root, "rules", "extra.ignore"), "cache/**\n!cache/keep.txt\n")
	mustWriteFile(t, filepath.Join(root, "cache", "drop.txt"), "drop")
	mustWriteFile(t, filepath.Join(root, "cache", "nested", "drop.txt"), "drop")
	mustWriteFile(t, filepath.Join(root, "cache", "keep.txt"), "keep")
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "visible")

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"cache/keep.txt", "rules/extra.ignore", "visible.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}

func TestScanFolderSyncIgnoreHonorsAnchoredAndUnanchoredDirectoryPatterns(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".sync"))
	mustMkdir(t, filepath.Join(root, "build"))
	mustMkdir(t, filepath.Join(root, "src", "build"))
	mustMkdir(t, filepath.Join(root, "rootonly"))
	mustMkdir(t, filepath.Join(root, "src", "rootonly"))
	mustWriteFile(t, filepath.Join(root, ".sync", "ignore"), "build/\n/rootonly/\n")
	mustWriteFile(t, filepath.Join(root, "build", "drop.txt"), "drop")
	mustWriteFile(t, filepath.Join(root, "src", "build", "drop.txt"), "drop")
	mustWriteFile(t, filepath.Join(root, "rootonly", "drop.txt"), "drop")
	mustWriteFile(t, filepath.Join(root, "src", "rootonly", "keep.txt"), "keep")
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "visible")

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"src/rootonly/keep.txt", "visible.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}

func TestScanFolderSyncIgnoreSupportsCaseInsensitivePrefix(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".sync"))
	mustMkdir(t, filepath.Join(root, "logs"))
	mustWriteFile(t, filepath.Join(root, ".sync", "ignore"), "(?i)secret.txt\n(?i)logs/*\n(?i)!logs/KEEP.txt\n")
	mustWriteFile(t, filepath.Join(root, "Secret.TXT"), "drop")
	mustWriteFile(t, filepath.Join(root, "logs", "DEBUG.TMP"), "drop")
	mustWriteFile(t, filepath.Join(root, "logs", "keep.txt"), "keep")
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "visible")

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"logs/keep.txt", "visible.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}

func TestScanFolderSyncIgnoreSupportsBracketAndEscapePatterns(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".sync"))
	mustMkdir(t, filepath.Join(root, "nested"))
	mustWriteFile(t, filepath.Join(root, ".sync", "ignore"), "**/file[0-9].txt\n**/literal\\*.txt\n!nested/file7.txt\n")
	mustWriteFile(t, filepath.Join(root, "file3.txt"), "drop")
	mustWriteFile(t, filepath.Join(root, "filex.txt"), "keep")
	mustWriteFile(t, filepath.Join(root, "nested", "file7.txt"), "keep")
	mustWriteFile(t, filepath.Join(root, "nested", "literal*.txt"), "drop")
	mustWriteFile(t, filepath.Join(root, "nested", "literalX.txt"), "keep")

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"filex.txt", "nested/file7.txt", "nested/literalX.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}

func TestScanFolderSyncIgnoreRequiresExactIncludeDirective(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".sync"))
	mustWriteFile(t, filepath.Join(root, ".sync", "ignore"), "#included files are documented here\n*.tmp\n")
	mustWriteFile(t, filepath.Join(root, "drop.tmp"), "drop")
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "visible")

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"visible.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}

func TestScanFolderRecordsInaccessibleFilesAndContinues(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "visible")
	if err := os.Symlink("missing-target", filepath.Join(root, "locked.txt")); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"visible.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
	if len(result.Inaccessible) != 1 {
		t.Fatalf("inaccessible files = %+v", result.Inaccessible)
	}
	if result.Inaccessible[0].RelativePath != "locked.txt" || result.Inaccessible[0].Error == "" {
		t.Fatalf("inaccessible entry = %+v", result.Inaccessible[0])
	}
}

func TestScanFolderSyncIgnoreAppliesLastMatchAcrossIncludes(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".sync"))
	mustMkdir(t, filepath.Join(root, "rules"))
	mustWriteFile(t, filepath.Join(root, ".sync", "ignore"), "*.log\n#include rules/override.ignore\nkeep.log\n")
	mustWriteFile(t, filepath.Join(root, "rules", "override.ignore"), "!keep.log\n!include-only.log\n")
	mustWriteFile(t, filepath.Join(root, "drop.log"), "drop")
	mustWriteFile(t, filepath.Join(root, "include-only.log"), "keep")
	mustWriteFile(t, filepath.Join(root, "keep.log"), "drop after later primary rule")
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "visible")

	result, err := ScanFolder(root, Options{BlockSize: 4})
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}

	got := relativePaths(result.Files)
	want := []string{"include-only.log", "rules/override.ignore", "visible.txt"}
	if !sameStrings(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func relativePaths(files []File) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.RelativePath)
	}
	return paths
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
