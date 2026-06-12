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

func TestWorkflowsUseNode24ReadyGitHubActions(t *testing.T) {
	workflows := map[string]string{
		"ci.yml":        readWorkflow(t, "ci.yml"),
		"container.yml": readWorkflow(t, "container.yml"),
		"release.yml":   readWorkflow(t, "release.yml"),
	}

	for name, workflow := range workflows {
		for _, forbidden := range []string{
			"actions/checkout@v4",
			"actions/setup-go@v5",
			"actions/setup-node@v4",
			"actions/upload-artifact@v4",
		} {
			if strings.Contains(workflow, forbidden) {
				t.Fatalf("%s still uses Node 20-era action %q", name, forbidden)
			}
		}
	}

	combined := strings.Join([]string{workflows["ci.yml"], workflows["container.yml"], workflows["release.yml"]}, "\n")
	for _, want := range []string{
		"actions/checkout@v6",
		"actions/setup-go@v6",
		"actions/setup-node@v6",
		"actions/upload-artifact@v7",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("workflows missing Node 24-ready action %q", want)
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
		"linux-desktop-artifacts",
		"ubuntu-24.04-arm",
		"scripts/build-all.sh",
		"scripts/package-desktop-engine-resources.sh",
		"scripts/build-desktop-gui-wails.sh",
		"scripts/package-desktop-linux-installers.sh",
		"FSE_DESKTOP_WAILS_TARGETS",
		"FSE_DESKTOP_LINUX_INSTALLER_TARGETS",
		"appimagetool-aarch64.AppImage",
		"APPIMAGE_EXTRACT_AND_RUN",
		"upload-artifact",
		"file-sync-engine-daemon-linux-amd64",
		"file-sync-engine-daemon-linux-arm64",
		"fse-desktop-${{ matrix.target }}-installers",
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
		"fse-desktop-windows-amd64-installer",
		"fse-desktop-windows-arm64-installer",
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

func TestReleaseWorkflowUploadArtifactNamesAreUnambiguousByTarget(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	for _, want := range []string{
		"name: file-sync-engine-daemon-linux-amd64-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: file-sync-engine-daemon-linux-arm64-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-${{ matrix.target }}-installers-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-windows-amd64-installer-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-windows-arm64-installer-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-${{ matrix.target }}-installer-${{ steps.version.outputs.version }}-${{ github.sha }}",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing unambiguous upload artifact name %q", want)
		}
	}
	for _, ambiguous := range []string{
		"name: fse-desktop-linux-installers-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-windows-installers-${{ steps.version.outputs.version }}-${{ github.sha }}",
	} {
		if strings.Contains(workflow, ambiguous) {
			t.Fatalf("release workflow still uses ambiguous multi-architecture artifact name %q", ambiguous)
		}
	}
}

func TestReleaseWorkflowUsesNativeLinuxArm64DesktopRunner(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	for _, want := range []string{
		"linux-desktop-artifacts:",
		"runs-on: ${{ matrix.runner }}",
		"target: linux-amd64",
		"target: linux-arm64",
		"runner: ubuntu-24.04-arm",
		"FSE_DESKTOP_WAILS_TARGETS: linux/${{ matrix.goarch }}",
		"FSE_DESKTOP_LINUX_INSTALLER_TARGETS: ${{ matrix.target }}",
		"appimagetool-aarch64.AppImage",
		"APPIMAGE_EXTRACT_AND_RUN: 1",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing native Linux desktop artifact contract %q", want)
		}
	}
	if strings.Contains(workflow, "Dockerfile.linux-arm64-cross") || strings.Contains(workflow, "FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64") {
		t.Fatalf("release workflow must not build Linux arm64 desktop artifacts through the amd64 cross WebKit image")
	}
}

func TestReleaseWorkflowBuildsMacOSDaemonBeforeDesktopGoSetup(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	macJobIndex := strings.Index(workflow, "macos-desktop-artifacts:")
	if macJobIndex == -1 {
		t.Fatalf("release workflow missing macOS desktop artifact job")
	}
	macJob := workflow[macJobIndex:]
	daemonIndex := strings.Index(macJob, "Build cross-platform daemon binaries")
	checksumIndex := strings.Index(macJob, "Install macOS packaging checksum tools")
	desktopGoIndex := strings.Index(macJob, "Set up Go for Wails desktop build")
	installIndex := strings.Index(macJob, "Install Wails CLI and packaging tools")
	if daemonIndex == -1 || checksumIndex == -1 || desktopGoIndex == -1 || installIndex == -1 {
		t.Fatalf("release workflow missing macOS checksum setup, daemon build, desktop Go setup, or Wails install step")
	}
	if checksumIndex > daemonIndex {
		t.Fatalf("macOS release job must install coreutils before daemon/package tests need sha256sum")
	}
	if daemonIndex > desktopGoIndex {
		t.Fatalf("macOS release job must build daemon with root go.mod before setup-go switches to desktop-gui/go.mod")
	}
	if desktopGoIndex > installIndex {
		t.Fatalf("macOS release job must install Wails after setup-go switches to desktop-gui/go.mod")
	}
}

func TestReleaseWorkflowDefaultsToPackageManagerSafeCIVersion(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	if !strings.Contains(workflow, `version="0.0.0-ci.${GITHUB_RUN_NUMBER}"`) {
		t.Fatalf("release workflow must default CI versions to a digit-prefixed package-manager-safe value")
	}
	if strings.Contains(workflow, `version="ci-${GITHUB_SHA::12}"`) {
		t.Fatalf("release workflow must not default package builds to Debian-invalid ci-<sha> versions")
	}
}

func TestReleaseWorkflowBuildsMacOSDesktopInstallerArtifactsOnNativeRunners(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	script := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-desktop-gui-wails-native-macos.sh"))

	for _, want := range []string{
		"macos-desktop-artifacts",
		"macOS desktop installer artifacts",
		"runs-on: ${{ matrix.runner }}",
		"macos-15-intel",
		"macos-14",
		"darwin-amd64",
		"darwin-arm64",
		"FSE_DESKTOP_MACOS_ARCH: ${{ matrix.arch }}",
		"scripts/build-desktop-gui-wails-native-macos.sh",
		"FSE_DESKTOP_GUI_RELEASE_TARGETS: darwin-${{ matrix.arch }}",
		"fse-desktop-${{ matrix.target }}-installer",
		"build/${{ steps.version.outputs.version }}/desktop-gui/fse-desktop-${{ steps.version.outputs.version }}-darwin-${{ matrix.arch }}.zip",
		"upload-artifact",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing macOS artifact contract %q", want)
		}
	}
	if strings.Contains(workflow, "macos-13") {
		t.Fatalf("release workflow must not use retired/absent macos-13 runner labels")
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
