package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanFolderMetadataOnlyRecordsFileMetadataWithoutBlockHashes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "nested", "seed.bin")
	if err := os.WriteFile(file, []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_700_000_000, 123_000_000)
	if err := os.Chtimes(file, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.tmp"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ScanFolderMetadataOnly(root, Options{BlockSize: 4096, IgnoreSuffixes: []string{".tmp"}})
	if err != nil {
		t.Fatalf("ScanFolderMetadataOnly: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files = %+v", result.Files)
	}
	got := result.Files[0]
	if got.RelativePath != "nested/seed.bin" {
		t.Fatalf("relative path = %q", got.RelativePath)
	}
	if got.Manifest.Size != int64(len("seed-data")) || got.Manifest.BlockSize != 4096 {
		t.Fatalf("metadata size/block size mismatch: %+v", got.Manifest)
	}
	if len(got.Manifest.Blocks) != 0 {
		t.Fatalf("metadata-only scan should not record block hashes: %+v", got.Manifest.Blocks)
	}
	if got.Manifest.HashState != "unknown" {
		t.Fatalf("hash state = %q", got.Manifest.HashState)
	}
	if got.Manifest.ModTimeUnixNano != mtime.UnixNano() {
		t.Fatalf("mtime = %d want %d", got.Manifest.ModTimeUnixNano, mtime.UnixNano())
	}
	if got.Manifest.ChangeTimeUnixNano == 0 {
		t.Fatalf("change/birth time should be recorded when available or fall back to mtime")
	}
}
