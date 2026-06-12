package config

import (
	"os"
	"testing"
)

func TestResolvePathUsesExplicitPathBeforeCommonLocations(t *testing.T) {
	path, err := ResolvePath("/custom/config.json", []string{"/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if path != "/custom/config.json" {
		t.Fatalf("path = %q", path)
	}
}

func TestResolvePathFindsFirstExistingCommonLocation(t *testing.T) {
	dir := t.TempDir()
	missing := dir + "/missing.json"
	existing := dir + "/config.json"
	if err := os.WriteFile(existing, []byte(`{"nodeName":"node-a"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := ResolvePath("", []string{missing, existing})
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if path != existing {
		t.Fatalf("path = %q", path)
	}
}
