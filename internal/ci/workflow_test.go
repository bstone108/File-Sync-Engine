package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const macosDeveloperIDIdentity = "Developer ID Application: BRANDON BROWNING STONE (K6N4J68LTY)"

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

func TestReleaseWorkflowUsesAutomaticChicagoDateBuildVersions(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	script := readRequiredFile(t, filepath.Join("..", "..", "scripts", "resolve-release-version.sh"))
	ciWorkflow := readWorkflow(t, "ci.yml")

	// Published releases (Release artifacts, git tags, GitHub Releases, GHCR)
	// use America/Chicago date.build YYYY.M.D.N. Humans do not invent N.
	for _, want := range []string{
		"required: false",
		"YYYY.M.D.N",
		"scripts/resolve-release-version.sh \"${{ inputs.version }}\"",
		"GH_TOKEN: ${{ github.token }}",
		"fetch-tags: true",
		"TZ=America/Chicago",
		"TZ=America/Chicago date +%Y-%m-%d",
		"git tag --list",
		"git ls-remote --tags origin",
		"gh release list",
		"GITHUB_REF_TYPE",
		"GITHUB_REF_NAME#v",
	} {
		if !strings.Contains(workflow, want) && !strings.Contains(script, want) {
			t.Fatalf("automatic Chicago date.build release versioning contract missing %q", want)
		}
	}
	if strings.Contains(workflow, "required: true") {
		t.Fatal("workflow_dispatch version must be optional so a click-to-run release auto-stamps the next Chicago date.build")
	}
	if !strings.Contains(script, "N = 1 + max existing N") {
		t.Fatal("resolver must auto-increment N from existing tags/releases for the Chicago calendar day")
	}

	// Date.build is not a CI/test stamp. PR CI keeps dummy/dev versions.
	if strings.Contains(ciWorkflow, "resolve-release-version.sh") {
		t.Fatal("PR CI must not call the published-release version resolver")
	}
	if strings.Contains(ciWorkflow, "TZ=America/Chicago") {
		t.Fatal("PR CI must not generate America/Chicago date.build versions")
	}
	if !strings.Contains(ciWorkflow, `scripts/build-all.sh "ci-${GITHUB_SHA::12}"`) {
		t.Fatal("PR CI must keep its dummy ci-<sha> version for smoke builds")
	}
	if !strings.Contains(ciWorkflow, `-X main.version=${GITHUB_SHA}`) {
		t.Fatal("PR CI daemon builds must keep the SHA version stamp, not date.build")
	}

	for _, forbidden := range []string{
		`version="0.0.0-ci.${GITHUB_RUN_NUMBER}"`,
		`version="ci-${GITHUB_SHA::12}"`,
		"TZ=America/Chicago date +%Y.%m.%d",
		"manual release version",
		"required: true",
		"^[0-9]{4}\\.[0-9]{2}\\.[0-9]{2}\\.[0-9]{2}$",
		"printf -v version '%s." + "t" + "%02d'",
		"YYYY.MM.DD." + "t" + "NN",
		"\\." + "t" + "[0-9]",
	} {
		if strings.Contains(workflow, forbidden) || strings.Contains(script, forbidden) {
			t.Fatalf("release versioning still encodes the old padded/manual policy or a CI default %q", forbidden)
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
		"padded_tag=",
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
		"pattern: \"!*.dockerbuild\"",
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

func TestReleaseWorkflowPublishesVerifiedOptionalWebGUIPackageAsset(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	docs := readRequiredFile(t, filepath.Join("..", "..", "docs", "DOCKER.md"))

	for _, want := range []string{
		"web-gui-package:",
		"needs: version-preflight",
		"web-gui/dist/fse-web-container-default.zip",
		"name: fse-web-gui-package-${{ needs.version-preflight.outputs.version }}-${{ github.sha }}",
		"web-gui-package",
		"RELEASE_ASSET_SHA256SUMS",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow must publish the separately delivered optional web GUI package, missing %q", want)
		}
	}
	for _, want := range []string{
		"fse-web-gui-package-<version>.zip",
		"RELEASE_ASSET_SHA256SUMS",
		"separately delivered trusted package",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs must explain the optional web GUI release asset trust path, missing %q", want)
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
		"Historical failure: macOS Apple Silicon packages could report `app is damaged and can't be opened`",
		"Developer ID",
		"notarize",
		"staple",
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
		"container Wails script":    containerScript,
		"native macOS Wails script": nativeMacScript,
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

func TestWindowsWailsBuildDownloadsWebView2PrerequisiteWhenAbsent(t *testing.T) {
	script := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-desktop-gui-wails.sh"))
	for _, want := range []string{
		"FSE_DESKTOP_WAILS_PLATFORM\" = \"windows/amd64\"",
		"FSE_DESKTOP_WAILS_PLATFORM\" = \"windows/arm64\"",
		"-webview2 download",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Windows Wails build must offer WebView2 installation when absent; missing %q", want)
		}
	}
}

func TestMacOSInfoPlistAllowsLocalNetworkingForWailsAssetServer(t *testing.T) {
	plist := readRequiredFile(t, filepath.Join("..", "..", "desktop-gui", "build", "darwin", "Info.plist"))
	for _, want := range []string{
		"NSAppTransportSecurity",
		"NSAllowsLocalNetworking",
		"<true/>",
		"SUFeedURL",
		"SUPublicEDKey",
		"dV+k5IynR3jrGAA7dbDmr66A2rrOH3vPbc45CVcuGUE=",
		"SUScheduledCheckInterval",
		"172800",
		"appcast-darwin-arm64.xml",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("macOS Info.plist missing Wails/Sparkle requirement %q", want)
		}
	}
	if strings.Contains(plist, "SPARKLE_EDDSA_PRIVATE_KEY") || strings.Contains(plist, "p12") {
		t.Fatal("Info.plist must not contain Sparkle private key material or certificates")
	}
}

func TestMacOSDesktopEntitlementsEnableHardenedRuntimeWithoutSandbox(t *testing.T) {
	entitlements := readRequiredFile(t, filepath.Join("..", "..", "desktop-gui", "build", "darwin", "entitlements.plist"))
	signScript := readRequiredFile(t, filepath.Join("..", "..", "scripts", "sign-and-notarize-macos-desktop.sh"))
	// Apple's codesign / AMFI entitlements parser rejects XML comments
	// (AMFIUnserializeXML). Keep the plist comment-free; rationale lives in the
	// sign script header.
	if strings.Contains(entitlements, "<!--") || strings.Contains(entitlements, "-->") {
		t.Fatal("macOS entitlements.plist must not contain XML comments; codesign AMFIUnserializeXML rejects them")
	}
	for _, want := range []string{
		"com.apple.security.cs.allow-jit",
		"com.apple.security.cs.allow-unsigned-executable-memory",
		"com.apple.security.cs.disable-library-validation",
	} {
		if !strings.Contains(entitlements, want) {
			t.Fatalf("macOS entitlements missing hardened-runtime key %q", want)
		}
	}
	for _, want := range []string{
		"WKWebView",
		"Go runtime",
		"App Sandbox entitlements",
		"network.client",
		"AMFIUnserializeXML",
	} {
		if !strings.Contains(signScript, want) {
			t.Fatalf("macOS sign script missing hardened-runtime justification %q", want)
		}
	}
	if strings.Contains(entitlements, "<key>com.apple.security.app-sandbox</key>") {
		t.Fatal("macOS desktop entitlements must not enable App Sandbox; the GUI launches a bundled daemon and uses user-selected sync folders")
	}
	if strings.Contains(entitlements, "<key>com.apple.security.network.client</key>") || strings.Contains(entitlements, "<key>com.apple.security.network.server</key>") {
		t.Fatal("macOS desktop entitlements must not add sandbox network keys; this app is not sandboxed")
	}
}

func TestMacOSSignAndNotarizeScriptRefusesNonDarwinHosts(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-Darwin guard is exercised on Linux CI")
	}
	root := filepath.Join("..", "..")
	cmd := exec.Command("bash", "scripts/sign-and-notarize-macos-desktop.sh", "fse-desktop.app", "2026.08.11.02", "arm64")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("sign/notarize script unexpectedly succeeded off macOS:\n%s", output)
	}
	if got := string(output); !strings.Contains(got, "native macOS runner required") {
		t.Fatalf("sign/notarize script did not refuse non-Darwin host; output:\n%s", got)
	}
}

func TestReleaseWorkflowBuildsMacOSDesktopInstallerArtifactsOnNativeRunners(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	script := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-desktop-gui-wails-native-macos.sh"))
	signScript := readRequiredFile(t, filepath.Join("..", "..", "scripts", "sign-and-notarize-macos-desktop.sh"))

	for _, want := range []string{
		"macos-desktop-artifacts",
		"macOS desktop installer artifacts",
		"runs-on: ${{ matrix.runner }}",
		"macos-15-intel",
		"runner: macos-15\n",
		"darwin-amd64",
		"darwin-arm64",
		"FSE_DESKTOP_MACOS_ARCH: ${{ matrix.arch }}",
		"scripts/build-desktop-gui-wails-native-macos.sh",
		"scripts/sign-and-notarize-macos-desktop.sh",
		"secrets.APPLE_CERTIFICATE_BASE64",
		"secrets.APPLE_CERTIFICATE_PASSWORD",
		"secrets.APPLE_ID",
		"secrets.APPLE_APP_SPECIFIC_PASSWORD",
		"secrets.APPLE_TEAM_ID",
		"FSE_MACOS_SIGN_IDENTITY:",
		"FSE_DESKTOP_GUI_RELEASE_TARGETS: darwin-${{ matrix.arch }}",
		"fse-desktop-${{ matrix.target }}-installer",
		"build/${{ steps.version.outputs.version }}/desktop-gui/fse-desktop-${{ steps.version.outputs.version }}-darwin-${{ matrix.arch }}.zip",
		"build/${{ steps.version.outputs.version }}/desktop-gui/fse-desktop-${{ steps.version.outputs.version }}-darwin-${{ matrix.arch }}.dmg",
		"upload-artifact",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing macOS artifact contract %q", want)
		}
	}
	requireQuotedYAMLMacOSSignIdentity(t, workflow)
	requireHostedMacOSRunnerLabels(t, workflow, "release workflow")
	if !strings.Contains(workflow, `          - arch: amd64
            target: darwin-amd64
            runner: macos-15-intel
          - arch: arm64
            target: darwin-arm64
            runner: macos-15
`) {
		t.Fatal("release workflow must keep separate macos-15-intel (amd64) and macos-15 (arm64) jobs; Wails cannot lipo a universal app here")
	}
	buildIndex := strings.Index(workflow, "scripts/build-desktop-gui-wails-native-macos.sh")
	signIndex := strings.Index(workflow, `scripts/sign-and-notarize-macos-desktop.sh "desktop-gui/wails-output/darwin-${{ matrix.arch }}/fse-desktop.app"`)
	packageIndex := strings.Index(workflow, "FSE_DESKTOP_GUI_RELEASE_TARGETS: darwin-${{ matrix.arch }}")
	if buildIndex == -1 || signIndex == -1 || packageIndex == -1 {
		t.Fatal("release workflow missing macOS build, Developer ID sign/notarize, or package step")
	}
	if buildIndex > signIndex || signIndex > packageIndex {
		t.Fatal("macOS release job must build the .app, then Developer ID-sign/notarize/staple, then package zip+dmg")
	}
	for _, want := range []string{
		"native macOS runner",
		"GOOS=darwin",
		"GOARCH=\"$ARCH\"",
		"wails build -platform darwin/$ARCH",
		"desktop-gui/wails-output/darwin-$ARCH",
		"fse-desktop.app/Contents/MacOS/fse-desktop",
		"Contents/Resources/engine",
		"Contents/Resources/docs-snapshot/README.md",
		"scripts/sign-and-notarize-macos-desktop.sh",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("native macOS Wails script missing %q", want)
		}
	}
	for _, forbidden := range []string{"codesign --force --deep --sign -", "codesign --sign -"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("native macOS Wails script must not ad-hoc sign with %q", forbidden)
		}
	}
	for _, want := range []string{
		"APPLE_CERTIFICATE_BASE64",
		"APPLE_CERTIFICATE_PASSWORD",
		"APPLE_ID",
		"APPLE_APP_SPECIFIC_PASSWORD",
		"APPLE_TEAM_ID",
		macosDeveloperIDIdentity,
		"--options runtime",
		"--timestamp",
		"--entitlements",
		"xcrun notarytool submit",
		"xcrun stapler staple",
		"hdiutil create",
		"ditto -c -k --keepParent",
		"codesign --verify --deep --strict",
	} {
		if !strings.Contains(signScript, want) {
			t.Fatalf("macOS sign/notarize script missing %q", want)
		}
	}
	for _, forbidden := range []string{"codesign --force --deep --sign -", "Dockerfile.darwin-osxcross", "osxcross", "FSE_API_KEY", "FSE_IDENTITY_PRIVATE_KEY", "identity.privateKey", "lipo"} {
		if strings.Contains(workflow, forbidden) || strings.Contains(script, forbidden) || strings.Contains(signScript, forbidden) {
			t.Fatalf("macOS native artifact path must not depend on forbidden text %q", forbidden)
		}
	}
}

func TestPRCICompilesUnsignedNativeMacOSDesktopOnHostedRunners(t *testing.T) {
	ci := readWorkflow(t, "ci.yml")
	release := readWorkflow(t, "release.yml")
	signScript := readRequiredFile(t, filepath.Join("..", "..", "scripts", "sign-and-notarize-macos-desktop.sh"))
	packager := readRequiredFile(t, filepath.Join("..", "..", "scripts", "package-desktop-gui-release.sh"))
	harness := readRequiredFile(t, filepath.Join("..", "..", "scripts", "run-serious-harness.sh"))

	start := strings.Index(ci, "macos-desktop-wails-build:")
	end := strings.Index(ci, "windows-desktop-wails-build:")
	if start == -1 || end == -1 || end <= start {
		t.Fatal("PR CI missing unsigned macOS native Wails desktop compile job")
	}
	macJob := ci[start:end]

	for _, want := range []string{
		"python3 scripts/test_macos_wails_ci_contract.py",
		"macos-desktop-wails-build:",
	} {
		if !strings.Contains(ci, want) {
			t.Fatalf("PR CI missing unsigned macOS Wails compile contract %q", want)
		}
	}
	for _, want := range []string{
		"macos-desktop-wails-build:",
		"runs-on: ${{ matrix.runner }}",
		"go-version-file: desktop-gui/go.mod",
		"node-version: 22",
		"github.com/wailsapp/wails/v2/cmd/wails@v2.10.2",
		"wails generate module",
		"npm ci",
		"npm run typecheck",
		"npm run build",
		`wails build -clean -platform "${{ matrix.platform }}" -o fse-desktop`,
		`FSE_DESKTOP_VERSION="ci-${GITHUB_SHA::12}"`,
		"Build unsigned native macOS desktop application",
		"scripts/fetch-sparkle-framework.sh",
		"Sparkle.framework",
		"CGO_LDFLAGS",
		"DYLD_FRAMEWORK_PATH",
		"scripts/add-macos-sparkle-rpath.sh",
	} {
		if !strings.Contains(macJob, want) {
			t.Fatalf("unsigned macOS Wails compile job missing %q", want)
		}
	}
	if !strings.Contains(macJob, `          - arch: amd64
            platform: darwin/amd64
            runner: macos-15-intel
          - arch: arm64
            platform: darwin/arm64
            runner: macos-15
`) {
		t.Fatal("PR CI must compile darwin/amd64 on macos-15-intel and darwin/arm64 on macos-15 as separate unsigned jobs")
	}
	if !strings.Contains(harness, "python3 scripts/test_macos_wails_ci_contract.py") {
		t.Fatal("serious harness must run the unsigned macOS Wails CI contract")
	}

	for _, forbidden := range []string{
		"macos-14",
		"macos-latest",
		"notarytool",
		"stapler",
		"codesign",
		"APPLE_CERTIFICATE",
		"APPLE_ID",
		"FSE_MACOS_SIGN_IDENTITY",
		"sign-and-notarize-macos-desktop.sh",
		"resolve-release-version.sh",
		"lipo",
		"SPARKLE_EDDSA_PRIVATE_KEY",
		"sign-sparkle-appcast.sh",
		"notarytool",
	} {
		if strings.Contains(macJob, forbidden) {
			t.Fatalf("unsigned macOS PR compile job must not include %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"macos-14",
		"macos-latest",
		"notarytool",
		"stapler",
		"APPLE_CERTIFICATE_BASE64",
		"FSE_MACOS_SIGN_IDENTITY",
		"sign-and-notarize-macos-desktop.sh",
		"SPARKLE_EDDSA_PRIVATE_KEY",
		"sign-sparkle-appcast.sh",
	} {
		if strings.Contains(ci, forbidden) {
			t.Fatalf("PR CI must stay unsigned and must not contain %q", forbidden)
		}
	}
	requireHostedMacOSRunnerLabels(t, ci, "PR CI")

	// Sign, notarize, staple, and zip-after-staple remain Release artifacts only.
	requireQuotedYAMLMacOSSignIdentity(t, release)
	requireHostedMacOSRunnerLabels(t, release, "release workflow")
	releaseMacStart := strings.Index(release, "macos-desktop-artifacts:")
	if releaseMacStart == -1 {
		t.Fatal("release workflow missing macOS desktop artifact job")
	}
	releaseMac := release[releaseMacStart:]
	for _, want := range []string{
		macosDeveloperIDIdentity,
		"K6N4J68LTY",
		"scripts/sign-and-notarize-macos-desktop.sh",
		`scripts/sign-and-notarize-macos-desktop.sh "desktop-gui/wails-output/darwin-${{ matrix.arch }}/fse-desktop.app"`,
		"secrets.APPLE_CERTIFICATE_BASE64",
		"secrets.APPLE_TEAM_ID",
	} {
		if !strings.Contains(releaseMac, want) {
			t.Fatalf("release workflow must keep Developer ID sign/notarize/staple, missing %q", want)
		}
	}
	for _, want := range []string{
		"--options runtime",
		"--timestamp",
		"ditto -c -k --keepParent",
		"xcrun notarytool submit",
		"xcrun stapler staple",
		"codesign --force --timestamp --sign",
	} {
		if !strings.Contains(signScript, want) {
			t.Fatalf("macOS sign/notarize script missing %q", want)
		}
	}
	if !strings.Contains(packager, "zip -qr \"$OUT_DIR/$zip_name\" fse-desktop.app") {
		t.Fatal("release packager must zip the stapled .app after Developer ID sign/notarize/staple")
	}
	buildIndex := strings.Index(releaseMac, "scripts/build-desktop-gui-wails-native-macos.sh")
	signIndex := strings.Index(releaseMac, `scripts/sign-and-notarize-macos-desktop.sh "desktop-gui/wails-output/darwin-${{ matrix.arch }}/fse-desktop.app"`)
	packageIndex := strings.Index(releaseMac, "FSE_DESKTOP_GUI_RELEASE_TARGETS: darwin-${{ matrix.arch }}")
	appcastIndex := strings.Index(releaseMac, "scripts/sign-sparkle-appcast.sh")
	if buildIndex == -1 || signIndex == -1 || packageIndex == -1 || buildIndex > signIndex || signIndex > packageIndex {
		t.Fatal("macOS release job must build, Developer ID-sign/notarize/staple, then zip the stapled .app")
	}
	if appcastIndex == -1 || appcastIndex < packageIndex {
		t.Fatal("Sparkle appcast signing must run after the notarized zip is packaged")
	}
}

func TestReleaseWorkflowSignsSparkleAppcastWithSignUpdate(t *testing.T) {
	release := readWorkflow(t, "release.yml")
	appcast := readRequiredFile(t, filepath.Join("..", "..", "scripts", "sign-sparkle-appcast.sh"))
	fetch := readRequiredFile(t, filepath.Join("..", "..", "scripts", "fetch-sparkle-framework.sh"))
	native := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-desktop-gui-wails-native-macos.sh"))
	signScript := readRequiredFile(t, filepath.Join("..", "..", "scripts", "sign-and-notarize-macos-desktop.sh"))
	ci := readWorkflow(t, "ci.yml")

	for _, want := range []string{
		"secrets.SPARKLE_EDDSA_PRIVATE_KEY",
		"scripts/sign-sparkle-appcast.sh",
		"appcast-darwin-${{ matrix.arch }}.xml",
		"name: appcast-darwin-${{ matrix.arch }}-${{ github.sha }}",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release workflow missing Sparkle appcast contract %q", want)
		}
	}
	for _, want := range []string{
		"SPARKLE_EDDSA_PRIVATE_KEY is required to sign the Sparkle appcast; refusing to skip signing.",
		"sign_update",
		"--ed-key-file -",
		"SPARKLE_EDDSA_PRIVATE_KEY has unexpected length",
		"\"$SIGN_UPDATE\" --help",
		"xattr -l",
		"fse-desktop-darwin-${ARCH}-installer-${VERSION}.zip",
		"SUPublicEDKey",
		"dV+k5IynR3jrGAA7dbDmr66A2rrOH3vPbc45CVcuGUE=",
	} {
		if !strings.Contains(appcast, want) {
			t.Fatalf("Sparkle appcast signer missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"echo \"$SPARKLE_EDDSA_PRIVATE_KEY\"",
		"echo $SPARKLE_EDDSA_PRIVATE_KEY",
		"APPLE_CERTIFICATE_BASE64",
		"signature_line=\"$(",
		"$(\"$SIGN_UPDATE\"",
		"$( \"$SIGN_UPDATE\"",
		"`$SIGN_UPDATE",
		"`\"$SIGN_UPDATE\"",
		"xcrun notarytool",
		"awk '/^[A-Za-z0-9+/=]",
		"awk '/^[A-Za-z0-9+/",
	} {
		if strings.Contains(appcast, forbidden) {
			t.Fatalf("Sparkle appcast signer must not print or reuse forbidden material %q", forbidden)
		}
	}
	for _, want := range []string{
		"Sparkle.framework",
		"xattr -dr com.apple.quarantine",
		"Sparkle sign_update is missing or not executable",
		"! -path '*/old_dsa_scripts/*'",
	} {
		if !strings.Contains(fetch, want) {
			t.Fatalf("Sparkle fetch script missing %q", want)
		}
	}
	if strings.Contains(fetch, "notarytool") || strings.Contains(fetch, "codesign") || strings.Contains(fetch, "stapler") {
		t.Fatal("Sparkle fetch script must download Sparkle.framework without signing or notarizing")
	}
	for _, want := range []string{"fetch-sparkle-framework.sh", "Sparkle.framework", "stamp-desktop-gui-version.sh", "CGO_LDFLAGS", "DYLD_FRAMEWORK_PATH", "add-macos-sparkle-rpath.sh"} {
		if !strings.Contains(native, want) {
			t.Fatalf("native macOS Wails build missing Sparkle embed %q", want)
		}
	}
	if !strings.Contains(signScript, "Sparkle.framework") {
		t.Fatal("Developer ID sign script must sign Sparkle.framework inside the .app")
	}
	if strings.Contains(ci, "SPARKLE_EDDSA_PRIVATE_KEY") || strings.Contains(ci, "sign-sparkle-appcast.sh") {
		t.Fatal("PR CI must not sign Sparkle appcasts or receive the EdDSA private key")
	}
	harness := readRequiredFile(t, filepath.Join("..", "..", "scripts", "run-serious-harness.sh"))
	if !strings.Contains(harness, "SignSparkleAppcast") || !strings.Contains(harness, "FetchSparkleFramework") {
		t.Fatal("serious harness must run Sparkle appcast signing and fetch contract tests")
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
		"GITHUB_REPOSITORY,,",
		"repository=ghcr.io/%s",
		"${{ steps.image-ref.outputs.repository }}:${{ needs.version-preflight.outputs.version }}",
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

func requireQuotedYAMLMacOSSignIdentity(t *testing.T, workflow string) {
	t.Helper()
	if !strings.Contains(workflow, macosDeveloperIDIdentity) {
		t.Fatalf("release workflow missing Developer ID identity %q", macosDeveloperIDIdentity)
	}
	unquoted := "FSE_MACOS_SIGN_IDENTITY: " + macosDeveloperIDIdentity
	doubleQuoted := `FSE_MACOS_SIGN_IDENTITY: "` + macosDeveloperIDIdentity + `"`
	singleQuoted := "FSE_MACOS_SIGN_IDENTITY: '" + macosDeveloperIDIdentity + "'"
	if strings.Contains(workflow, unquoted) {
		t.Fatal("FSE_MACOS_SIGN_IDENTITY must be a quoted YAML scalar; an unquoted colon makes GitHub Actions reject the workflow file")
	}
	if !strings.Contains(workflow, doubleQuoted) && !strings.Contains(workflow, singleQuoted) {
		t.Fatal("release workflow must set FSE_MACOS_SIGN_IDENTITY to the Developer ID identity as a quoted YAML scalar")
	}
}

func requireHostedMacOSRunnerLabels(t *testing.T, workflow, label string) {
	t.Helper()
	for _, forbidden := range []string{"macos-latest", "macos-13", "macos-14"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("%s must not use runner label %q; policy is GitHub-hosted macos-15 (arm64) and macos-15-intel (x86_64)", label, forbidden)
		}
	}
	if !strings.Contains(workflow, "macos-15-intel") || !strings.Contains(workflow, "runner: macos-15\n") {
		t.Fatalf("%s must use GitHub-hosted macos-15 (arm64) and macos-15-intel (x86_64)", label)
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
