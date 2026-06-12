package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrossPlatformWorkflowCoversTestHarnessAndSixTargets(t *testing.T) {
	workflow := readWorkflow(t, "ci.yml")

	required := []string{
		"go test ./...",
		"scripts/run-serious-harness.sh",
		"GOOS: linux",
		"GOOS: darwin",
		"GOOS: windows",
		"GOARCH: amd64",
		"GOARCH: arm64",
		"scripts/build-all.sh",
	}
	for _, text := range required {
		if !strings.Contains(workflow, text) {
			t.Fatalf("workflow missing %q", text)
		}
	}

	expectedTargets := []string{
		"linux-amd64",
		"linux-arm64",
		"darwin-amd64",
		"darwin-arm64",
		"windows-amd64",
		"windows-arm64",
	}
	for _, target := range expectedTargets {
		if !strings.Contains(workflow, target) {
			t.Fatalf("workflow missing cross-platform build target %q", target)
		}
	}
}

func TestDockerPublishWorkflowDocumentsVersioningAndUpdateVerification(t *testing.T) {
	workflow := readWorkflow(t, "container.yml")
	docs := readRequiredFile(t, filepath.Join("..", "..", "docs", "DOCKER.md"))

	for _, want := range []string{
		"docker/build-push-action",
		"ghcr.io",
		"type=semver,pattern={{version}}",
		"type=semver,pattern={{major}}.{{minor}}",
		"type=sha",
		"cosign sign",
		"cosign verify",
		"docker/metadata-action",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("container workflow missing %q", want)
		}
	}
	for _, forbidden := range []string{"FSE_API_KEY", "FSE_IDENTITY_PRIVATE_KEY", "identity.privateKey"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("container workflow must not publish or log runtime secrets %q", forbidden)
		}
	}
	for _, want := range []string{
		"GHCR",
		"semantic version tag",
		"immutable SHA tag",
		"cosign",
		"verify the image signature",
		"/config",
		"bundled default optional web GUI",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs missing %q", want)
		}
	}
}

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow %s: %v", name, err)
	}
	return string(data)
}
