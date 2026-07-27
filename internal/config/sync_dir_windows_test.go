//go:build windows

package config

import (
	"os"
	"testing"
)

func TestSyncDirWindowsIsSupportedNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"\\state", []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncDir(dir); err != nil {
		t.Fatalf("syncDir must not fail on Windows directory handles: %v", err)
	}
}
