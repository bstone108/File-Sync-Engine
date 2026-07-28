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
		{"go.mod", "golang.org/x/crypto v0.50.0"},
		{filepath.Join("desktop-gui", "go.mod"), "golang.org/x/crypto v0.50.0"},
		{"go.mod", "github.com/quic-go/quic-go v0.59.1"},
		{"go.mod", "github.com/quic-go/webtransport-go v0.10.0"},
		{"go.mod", "github.com/golang/glog v1.2.4"},
		{"go.mod", "go.opentelemetry.io/otel v1.42.0"},
		{"go.mod", "github.com/ipld/go-ipld-prime v0.23.0"},
		{"go.mod", "github.com/pion/dtls/v3 v3.1.2"},
		{filepath.Join("desktop-gui", "package.json"), "\"svelte\": \"^5.56.3\""},
		{filepath.Join("desktop-gui", "package.json"), "\"vite\": \"^6.4.2\""},
		{filepath.Join("desktop-gui", "package.json"), "\"@sveltejs/vite-plugin-svelte\": \"^6.2.4\""},
	}
	for _, check := range checks {
		content := readTextFile(t, filepath.Join(root, check.rel))
		if !strings.Contains(content, check.want) {
			t.Fatalf("%s must pin patched security dependency %q", check.rel, check.want)
		}
	}
}

func TestGoModulesDoNotRetainUnsupportedVulnerableDependencies(t *testing.T) {
	root := repoRoot(t)
	checks := []struct {
		rel       string
		forbidden string
	}{
		{"go.mod", "github.com/pion/dtls/v2"},
		{"go.sum", "github.com/pion/dtls/v2"},
	}
	for _, check := range checks {
		content := readTextFile(t, filepath.Join(root, check.rel))
		if strings.Contains(content, check.forbidden) {
			t.Fatalf("%s must not retain unsupported vulnerable dependency %q", check.rel, check.forbidden)
		}
	}
}

func TestDockerImageConsumesReleaseDaemonBuiltWithPatchedToolchain(t *testing.T) {
	root := repoRoot(t)
	dockerfile := readTextFile(t, filepath.Join(root, "Dockerfile"))
	workflow := readTextFile(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	if strings.Contains(dockerfile, "FROM golang:") || strings.Contains(dockerfile, "go build") {
		t.Fatal("Docker image must consume the checksum-verified same-run release daemon rather than compiling a second binary")
	}
	for _, want := range []string{"Download same-run Linux daemon artifacts for Docker image", "go-version-file: go.mod"} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow must build the daemon using the declared patched Go toolchain and stage it for Docker, missing %q", want)
		}
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
