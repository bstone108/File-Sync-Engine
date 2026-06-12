package configbootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOrCreateUsesExplicitPathAndCreatesSkeleton(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "chosen.jsonc")

	resolved, err := ResolveOrCreate(explicit, filepath.Join(dir, "bin", "fse"))
	if err != nil {
		t.Fatalf("ResolveOrCreate returned error: %v", err)
	}
	if resolved != explicit {
		t.Fatalf("resolved path = %q, want explicit %q", resolved, explicit)
	}
	if _, err := os.Stat(explicit); err != nil {
		t.Fatalf("expected generated config at explicit path: %v", err)
	}
}

func TestResolveOrCreatePrefersExistingExecutableAdjacentConfig(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(binDir, "file-sync-engine.json")
	if err := os.WriteFile(existing, []byte(`{"nodeName":"node","api":{"key":"existing"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveOrCreate("", filepath.Join(binDir, "fse"))
	if err != nil {
		t.Fatalf("ResolveOrCreate returned error: %v", err)
	}
	if resolved != existing {
		t.Fatalf("resolved path = %q, want existing adjacent config %q", resolved, existing)
	}
}

func TestResolveOrCreateCreatesExecutableAdjacentConfigWhenNoCommonConfigExists(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(binDir, "file-sync-engine.json")

	resolved, err := ResolveOrCreate("", filepath.Join(binDir, "fse"))
	if err != nil {
		t.Fatalf("ResolveOrCreate returned error: %v", err)
	}
	if resolved != want {
		t.Fatalf("resolved path = %q, want first common path %q", resolved, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected generated config at first common path: %v", err)
	}
}
