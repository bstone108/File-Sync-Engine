package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoModulesPinPatchedSecurityDependencyVersions(t *testing.T) {
	root := repoRoot(t)
	checks := []struct {
		rel  string
		want string
	}{
		{"go.mod", "golang.org/x/crypto v0.47.0"},
		{filepath.Join("desktop-gui", "go.mod"), "golang.org/x/crypto v0.47.0"},
		{"go.mod", "github.com/quic-go/quic-go v0.59.1"},
		{"go.mod", "github.com/quic-go/webtransport-go v0.10.0"},
		{"go.mod", "github.com/golang/glog v1.2.4"},
	}
	for _, check := range checks {
		content := readTextFile(t, filepath.Join(root, check.rel))
		if !strings.Contains(content, check.want) {
			t.Fatalf("%s must pin patched security dependency %q", check.rel, check.want)
		}
	}
}

func TestDockerBuilderUsesGoToolchainThatSatisfiesPatchedDependencies(t *testing.T) {
	root := repoRoot(t)
	content := readTextFile(t, filepath.Join(root, "Dockerfile"))
	if !strings.Contains(content, "FROM golang:1.24-alpine AS builder") {
		t.Fatalf("Dockerfile builder must use Go 1.24 for patched golang.org/x/crypto dependencies")
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("repo root with go.mod not found")
		}
		wd = parent
	}
}
