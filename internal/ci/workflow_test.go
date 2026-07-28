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

func TestContainerReleaseAndCIExplicitlySupportLinuxARMv7(t *testing.T) {
	ciWorkflow := readWorkflow(t, "ci.yml")
	releaseWorkflow := readWorkflow(t, "release.yml")
	buildScript := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-all.sh"))
	dockerDocs := readRequiredFile(t, filepath.Join("..", "..", "docs", "DOCKER.md"))

	for _, want := range []string{
		"target: linux-armv7",
		"GOARCH: arm",
		"GOARM: 7",
		"name: file-sync-engine-daemon-${{ matrix.target }}-${{ github.sha }}",
	} {
		if !strings.Contains(ciWorkflow, want) {
			t.Fatalf("CI workflow must build a GOARM=7 daemon artifact, missing %q", want)
		}
	}
	for _, want := range []string{
		"fse-linux-armv7",
		"file-sync-engine-daemon-linux-armv7-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"linux/amd64,linux/arm64,linux/arm/v7",
	} {
		if !strings.Contains(releaseWorkflow, want) {
			t.Fatalf("release workflow must publish a Linux ARMv7 daemon/image contract, missing %q", want)
		}
	}
	if !strings.Contains(buildScript, "GOOS=linux GOARCH=arm GOARM=7") {
		t.Fatalf("build-all script must cross-compile the Linux ARMv7 daemon")
	}
	for _, want := range []string{"linux/arm/v7", "not runtime proof"} {
		if !strings.Contains(dockerDocs, want) {
			t.Fatalf("Docker documentation must distinguish ARMv7 release support from runtime evidence, missing %q", want)
		}
	}
}

func TestRootReadmeDockerExamplesKeepTheCoreHeadless(t *testing.T) {
	readme := readRequiredFile(t, filepath.Join("..", "..", "README.md"))

	for _, want := range []string{
		"FSE_WEB_GUI_ENABLED: \"false\"",
		"FSE_WEB_GUI_ENABLED=false",
		"The optional web GUI is not yet a working deployment interface.",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("root Docker example must preserve headless-default boundary, missing %q", want)
		}
	}
	for _, forbidden := range []string{"8385:8385", "8943:8943", "FSE_WEB_GUI_ENABLED: \"true\"", "FSE_WEB_GUI_ENABLED=true", "FSE_WEB_GUI_PACKAGE: Web GUI package path"} {
		if strings.Contains(readme, forbidden) {
			t.Fatalf("root Docker example advertises an unavailable bundled GUI setting %q", forbidden)
		}
	}
}

func TestWorkflowsUseNode24ReadyGitHubActions(t *testing.T) {
	workflows := map[string]string{
		"ci.yml":      readWorkflow(t, "ci.yml"),
		"release.yml": readWorkflow(t, "release.yml"),
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

	combined := strings.Join([]string{workflows["ci.yml"], workflows["release.yml"]}, "\n")
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
		"scripts/build-package-desktop-linux-webkit-variants.sh",
		"FSE_DESKTOP_WAILS_TARGETS",
		"FSE_DESKTOP_LINUX_WEBKIT_TARGETS",
		"FSE_DESKTOP_LINUX_WEBKIT_VARIANTS",
		"appimagetool-aarch64.AppImage",
		"APPIMAGE_EXTRACT_AND_RUN",
		"upload-artifact",
		"file-sync-engine-daemon-linux-amd64",
		"file-sync-engine-daemon-linux-arm64",
		"fse-desktop-${{ matrix.target }}-${{ matrix.webkit_slug }}-installers",
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
		"sudo apt-get install -y --no-install-recommends nsis",
		"fse-desktop-${{ steps.version.outputs.version }}-windows-amd64.zip",
		"fse-desktop-${{ steps.version.outputs.version }}-windows-arm64.zip",
		"fse-desktop-${{ steps.version.outputs.version }}-windows-amd64-installer.exe",
		"fse-desktop-${{ steps.version.outputs.version }}-windows-arm64-installer.exe",
		"fse-desktop-windows-amd64-zip-package",
		"fse-desktop-windows-arm64-zip-package",
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
		"name: fse-desktop-${{ matrix.target }}-${{ matrix.webkit_slug }}-installers-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-windows-amd64-zip-package-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-windows-amd64-installer-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"name: fse-desktop-windows-arm64-zip-package-${{ steps.version.outputs.version }}-${{ github.sha }}",
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
		"FSE_DESKTOP_LINUX_WEBKIT_TARGETS: linux/${{ matrix.goarch }}",
		"FSE_DESKTOP_LINUX_WEBKIT_VARIANTS: ${{ matrix.webkit_api }}",
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

func TestLinuxWebKitVariantScriptPassesSelectedBuilderAsDefaultWailsImage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-package-desktop-linux-webkit-variants.sh"))
	if err != nil {
		t.Fatalf("read Linux WebKit variant script: %v", err)
	}
	script := string(data)

	for _, want := range []string{
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE=\"$image\"",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX=\"$image\"",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64=\"$(builder_image_for_variant_target \"$api\" linux/amd64 \"$image\")\"",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64=\"$(builder_image_for_variant_target \"$api\" linux/arm64 \"$image\")\"",
		"\"$ROOT/scripts/build-desktop-gui-wails.sh\" \"$VERSION\" \"$wails_out\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Linux WebKit variant script missing Wails builder image handoff %q", want)
		}
	}
}

func TestReleaseWorkflowBuildsBothLinuxWebKitABIVariantArtifacts(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	for _, want := range []string{
		"webkit_api: '4.1'",
		"webkit_slug: webkit41",
		"webkit_api: '4.0'",
		"webkit_slug: webkit40",
		"development/desktop-wails-builder/Dockerfile.linux-webkit40",
		"FSE_DESKTOP_LINUX_WEBKIT_VARIANTS: ${{ matrix.webkit_api }}",
		"FSE_DESKTOP_LINUX_WEBKIT_TARGETS: linux/${{ matrix.goarch }}",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT41",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT40",
		"scripts/build-package-desktop-linux-webkit-variants.sh",
		"linux-installers-${{ matrix.webkit_slug }}",
		"fse-desktop-${{ matrix.target }}-${{ matrix.webkit_slug }}-installers-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"fse-desktop-${{ steps.version.outputs.version }}-${{ matrix.target }}.deb",
		"fse-desktop-${{ steps.version.outputs.version }}-${{ matrix.target }}.rpm",
		"fse-desktop-${{ steps.version.outputs.version }}-${{ matrix.target }}.AppImage",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing Linux WebKit ABI variant artifact contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"name: fse-desktop-${{ matrix.target }}-installers-${{ steps.version.outputs.version }}-${{ github.sha }}",
		"run: scripts/package-desktop-linux-installers.sh \"${{ steps.version.outputs.version }}\"",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow still uses single-ABI Linux desktop artifact path %q", forbidden)
		}
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

func TestReleaseWorkflowUsesManualDateReleaseVersions(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	script := readRequiredFile(t, filepath.Join("..", "..", "scripts", "resolve-release-version.sh"))

	for _, want := range []string{
		"version:",
		"required: true",
		"scripts/resolve-release-version.sh \"${{ inputs.version }}\"",
		"YYYY.MM.DD.NN",
		"manual release version",
		"GITHUB_REF_TYPE",
		"GITHUB_REF_NAME#v",
	} {
		if !strings.Contains(workflow, want) && !strings.Contains(script, want) {
			t.Fatalf("manual release versioning contract missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`version="0.0.0-ci.${GITHUB_RUN_NUMBER}"`,
		`version="ci-${GITHUB_SHA::12}"`,
		"TZ=America/Chicago date +%Y.%m.%d",
		"git tag --list",
		"printf -v version '%s." + "t" + "%02d'",
		"YYYY.MM.DD." + "t" + "NN",
		"\\." + "t" + "[0-9]",
	} {
		if strings.Contains(workflow, forbidden) || strings.Contains(script, forbidden) {
			t.Fatalf("release workflow must not auto-increment or use old CI version default %q", forbidden)
		}
	}
}

func TestReleaseWorkflowPreflightsAndRefusesDuplicateVersions(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	for _, want := range []string{
		"version-preflight:",
		"outputs:",
		"version: ${{ steps.version.outputs.version }}",
		"tag: ${{ steps.version.outputs.tag }}",
		"gh release view \"$tag\"",
		"git ls-remote --exit-code --tags origin \"refs/tags/$tag\"",
		"refusing to build duplicate release version",
		"needs: version-preflight",
		"${{ needs.version-preflight.outputs.version }}",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing duplicate-version preflight contract %q", want)
		}
	}
}

func TestReleaseWorkflowPublishesGitHubRelease(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")

	for _, want := range []string{
		"permissions:\n  contents: write",
		"publish-github-release:",
		"needs:",
		"version-preflight",
		"linux-release-artifacts",
		"linux-desktop-artifacts",
		"windows-desktop-artifacts",
		"macos-desktop-artifacts",
		"publish-container",
		"actions/download-artifact@v7",
		"release-assets",
		"RELEASE_ASSET_SHA256SUMS",
		"gh release create",
		"gh release upload",
		"if [[ \"$base\" == \"SHA256SUMS\" ]]",
		"stem=\"${artifact_dir%-${GITHUB_SHA}}\"",
		"release asset name collision",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing publish contract %q", want)
		}
	}
	for _, forbidden := range []string{"gh release edit", "--clobber", "release-assets/${artifact_dir}-${base}"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow must not overwrite existing release assets or use noisy names with %q", forbidden)
		}
	}
}

func TestDesktopStabilizationDocsRequirePlatformSmokeEvidenceBeforeTestReady(t *testing.T) {
	desktopDocs := readRequiredFile(t, filepath.Join("..", "..", "docs", "DESKTOP_GUI_ARCHITECTURE.md"))
	externalMatrix := readRequiredFile(t, filepath.Join("..", "..", "docs", "EXTERNAL_TESTING_MATRIX.md"))

	for _, want := range []string{
		"Current desktop artifacts are not test-ready until platform smoke evidence exists",
		"macOS ARM damaged-app launch check",
		"Windows native desktop shell availability check",
		"bundled daemon launch/adoption",
		"GUI-to-daemon API control",
		"first-use setup reaches sync-ready state",
	} {
		if !strings.Contains(desktopDocs, want) && !strings.Contains(externalMatrix, want) {
			t.Fatalf("desktop stabilization docs missing smoke-evidence contract %q", want)
		}
	}
}

func TestDesktopStabilizationDocsCarryPlatformFailureInventory(t *testing.T) {
	desktopDocs := readRequiredFile(t, filepath.Join("..", "..", "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"## Current platform failure inventory",
		"| Windows amd64/arm64 |",
		"| macOS arm64 |",
		"| macOS amd64 |",
		"| Linux amd64 |",
		"| Linux arm64 |",
		"app launches",
		"daemon launches/adopts",
		"API connects and GUI can control daemon",
		"logs/errors are visible",
		"first-use setup reaches sync-ready state",
		"Historical Windows `native desktop shell is not available` binding failure is fixed in source; the hosted gate compiles the Wails application and, because GitHub runners have no interactive desktop/WebView2 session, directly exercises the same native App bridge against a real staged Windows daemon: launch, authenticated HTTPS `/v1/status`, and `/v1/stop`",
		"Known failure: macOS Apple Silicon packages can report `app is damaged and can't be opened`",
		"Inventory status: unproven until smoke-tested on host or VM",
	} {
		if !strings.Contains(desktopDocs, want) {
			t.Fatalf("desktop stabilization docs missing platform failure inventory contract %q", want)
		}
	}
}

func TestDesktopWailsBuildScriptsStageOnlyTargetEngineResource(t *testing.T) {
	containerScript := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-desktop-gui-wails.sh"))
	nativeMacScript := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-desktop-gui-wails-native-macos.sh"))

	for name, script := range map[string]string{
		"container Wails script":     containerScript,
		"native macOS Wails script":  nativeMacScript,
	} {
		for _, want := range []string{
			"stage_target_engine_resource_subset()",
			"linux/amd64:linux/amd64/fse",
			"linux/arm64:linux/arm64/fse",
			"darwin/amd64:darwin/amd64/fse",
			"darwin/arm64:darwin/arm64/fse",
			"windows/amd64:windows/amd64/fse.exe",
			"windows/arm64:windows/arm64/fse.exe",
		} {
			if !strings.Contains(script, want) {
				t.Fatalf("%s missing target-only engine resource staging contract %q", name, want)
			}
		}
	}
	for _, want := range []string{
		"stage_target_engine_resource_subset \"$FSE_DESKTOP_WAILS_PLATFORM\" /tmp/work/resources/engine",
	} {
		if !strings.Contains(containerScript, want) {
			t.Fatalf("container Wails script must prune copied GUI resources before Wails build; missing %q", want)
		}
	}
	for _, want := range []string{
		"stage_target_engine_resource_subset \"darwin/$ARCH\" \"$WORK_DIR/resources/engine\"",
	} {
		if !strings.Contains(nativeMacScript, want) {
			t.Fatalf("native macOS Wails script must prune copied GUI resources before Wails build; missing %q", want)
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
		"codesign --force --deep --sign -",
		"codesign --verify --deep --strict",
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

func TestReleaseWorkflowPublishesSynchronizedContainerImage(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	docs := readRequiredFile(t, filepath.Join("..", "..", "docs", "DOCKER.md"))

	for _, want := range []string{
		"publish-container:",
		"needs: version-preflight",
		"Build, publish, and verify GHCR image",
		"docker/setup-buildx-action@v3",
		"docker/login-action@v3",
		"docker/build-push-action@v6",
		"ghcr.io/${{ github.repository }}:${{ needs.version-preflight.outputs.version }}",
		"linux/amd64,linux/arm64",
		"cosign sign",
		"cosign verify",
		"publish-container",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("unified release workflow missing container contract %q", want)
		}
	}
	for _, forbidden := range []string{"FSE_API_KEY", "FSE_IDENTITY_PRIVATE_KEY", "identity.privateKey", "container.yml"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("unified release workflow must not contain %q", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", ".github", "workflows", "container.yml")); !os.IsNotExist(err) {
		t.Fatalf("independent tag-triggered container workflow must be removed, stat err=%v", err)
	}
	for _, want := range []string{
		"same release version",
		"Release artifacts",
		"GHCR",
		"cosign",
		"verify the image signature",
		"/config",
		"Headless by default",
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
