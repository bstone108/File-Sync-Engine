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

func TestReleaseWorkflowBuildsLinuxDaemonAndDesktopArtifacts(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	for _, want := range []string{
		"workflow_dispatch:",
		"push:",
		"tags:",
		"v*",
		"linux-amd64",
		"linux-arm64",
		"scripts/build-all.sh",
		"scripts/package-desktop-engine-resources.sh",
		"scripts/build-desktop-gui-wails.sh",
		"scripts/package-desktop-linux-installers.sh",
		"FSE_DESKTOP_WAILS_TARGETS",
		"FSE_DESKTOP_LINUX_INSTALLER_TARGETS",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64",
		"FSE_DESKTOP_APPIMAGE_RUNTIME_AMD64",
		"FSE_DESKTOP_APPIMAGE_RUNTIME_ARM64",
		"upload-artifact",
		"file-sync-engine-daemon-linux-amd64",
		"file-sync-engine-daemon-linux-arm64",
		"fse-desktop-linux-installers",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}
	for _, forbidden := range []string{"FSE_API_KEY", "FSE_IDENTITY_PRIVATE_KEY", "identity.privateKey"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow must not publish or log runtime secrets %q", forbidden)
		}
	}
}

func TestReleaseWorkflowBuildsWindowsDesktopInstallerArtifacts(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	for _, want := range []string{
		"windows-desktop-artifacts",
		"Windows desktop installer artifacts",
		"windows-amd64",
		"windows-arm64",
		"Dockerfile.windows-arm64-llvm-mingw",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS_AMD64",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS_ARM64",
		"FSE_DESKTOP_WAILS_TARGETS: windows/amd64 windows/arm64",
		"FSE_DESKTOP_GUI_RELEASE_TARGETS: windows-amd64,windows-arm64",
		"scripts/package-desktop-engine-resources.sh",
		"scripts/build-desktop-gui-wails.sh",
		"scripts/package-desktop-gui-release.sh",
		"fse-desktop-windows-installers",
		"upload-artifact",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing Windows artifact contract %q", want)
		}
	}
	for _, forbidden := range []string{"FSE_API_KEY", "FSE_IDENTITY_PRIVATE_KEY", "identity.privateKey"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow must not publish or log runtime secrets %q", forbidden)
		}
	}
}

func TestReleaseWorkflowBuildsMacOSDesktopInstallerArtifactsOnNativeRunners(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	script := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-desktop-gui-wails-native-macos.sh"))

	for _, want := range []string{
		"macos-desktop-artifacts",
		"macOS desktop installer artifacts",
		"runs-on: ${{ matrix.runner }}",
		"macos-13",
		"macos-14",
		"darwin-amd64",
		"darwin-arm64",
		"FSE_DESKTOP_MACOS_ARCH: ${{ matrix.arch }}",
		"scripts/build-desktop-gui-wails-native-macos.sh",
		"FSE_DESKTOP_GUI_RELEASE_TARGETS: darwin-${{ matrix.arch }}",
		"fse-desktop-macos-${{ matrix.arch }}-installer",
		"build/${{ steps.version.outputs.version }}/desktop-gui/fse-desktop-${{ steps.version.outputs.version }}-darwin-${{ matrix.arch }}.zip",
		"upload-artifact",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing macOS artifact contract %q", want)
		}
	}
	for _, want := range []string{
		"native macOS runner",
		"GOOS=darwin",
		"GOARCH=\"$ARCH\"",
		"wails build -platform darwin/$ARCH",
		"desktop-gui/wails-output/darwin-$ARCH",
		"fse-desktop.app/Contents/MacOS/fse-desktop",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("native macOS Wails script missing %q", want)
		}
	}
	for _, forbidden := range []string{"Dockerfile.darwin-osxcross", "osxcross", "FSE_API_KEY", "FSE_IDENTITY_PRIVATE_KEY", "identity.privateKey"} {
		if strings.Contains(workflow, forbidden) || strings.Contains(script, forbidden) {
			t.Fatalf("macOS native artifact path must not depend on forbidden text %q", forbidden)
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
