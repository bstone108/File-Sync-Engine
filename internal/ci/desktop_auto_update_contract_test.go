package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopGUIInAppAutoUpdateCoversWindowsAppImageAndSparkle(t *testing.T) {
	root := filepath.Join("..", "..")
	updateGo := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update.go"))
	windowsGo := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update_nsis.go"))
	appImageGo := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update_appimage.go"))
	selectGo := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update_select.go"))
	tests := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update_test.go"))
	sparkleDarwin := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update_sparkle_darwin.go"))
	sparkleObjC := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update_sparkle_darwin.m"))
	sparkleKeys := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_update_sparkle_keys.go"))
	plist := readRequiredFile(t, filepath.Join(root, "desktop-gui", "build", "darwin", "Info.plist"))
	bridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "wailsNativeShell.ts"))
	appSvelte := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))
	release := readRequiredFile(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	ci := readRequiredFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"))

	for _, want := range []string{
		"CheckDesktopAppUpdate",
		"RestartDesktopAppUpdate",
		"updatePlatformWindows",
		"updatePlatformAppImage",
		"does not fall back to a download link",
	} {
		if !strings.Contains(updateGo, want) {
			t.Fatalf("desktop auto-update service missing %q", want)
		}
	}
	if !strings.Contains(windowsGo, "/S") || !strings.Contains(windowsGo, "/RELAUNCH") || strings.Contains(windowsGo, "https://github.com") {
		t.Fatal("Windows updater must launch a silent staged installer and must not open a GitHub URL")
	}
	if !strings.Contains(appImageGo, "APPIMAGE") || !strings.Contains(appImageGo, "appImageFileWritable") {
		t.Fatal("AppImage updater must detect APPIMAGE and writability")
	}
	for _, want := range []string{
		"fse-desktop-windows-",
		"fse-desktop-linux-",
		"webkit41",
		"webkit40",
		"fse-desktop-darwin-",
		"macSparkleZipKind",
	} {
		if !strings.Contains(selectGo, want) {
			t.Fatalf("asset selection missing %q", want)
		}
	}
	for _, want := range []string{
		"TestSelectUpdateAssetsMatchArchAndPreferSparkleZip",
		"TestWindowsUpdateNeverFallsBackToDownloadLink",
		"TestAppImageNotWritableNotifiesOncePerVersion",
		"TestAppImageWritableDetection",
	} {
		if !strings.Contains(tests, want) {
			t.Fatalf("auto-update tests missing %q", want)
		}
	}
	if !strings.Contains(sparkleDarwin, "framework Sparkle") || !strings.Contains(sparkleObjC, "SPUUpdater") || !strings.Contains(sparkleObjC, "SPUUserDriver") {
		t.Fatal("macOS updater must be Sparkle, not a generic GitHub downloader")
	}
	if idx := strings.Index(sparkleDarwin, "#cgo LDFLAGS:"); idx != -1 {
		line := sparkleDarwin[idx:]
		if nl := strings.IndexByte(line, '\n'); nl != -1 {
			line = line[:nl]
		}
		if strings.Contains(line, "-Wl,-rpath,@executable_path") {
			t.Fatal("Sparkle #cgo LDFLAGS must not use -Wl,-rpath,@executable_path; Go rejects that flag")
		}
		if !strings.Contains(line, "-Wl,-rpath,${SRCDIR}/third_party/sparkle") {
			t.Fatal("Sparkle #cgo LDFLAGS must rpath the fetched Sparkle.framework so Wails wailsbindings can load it")
		}
	}
	if strings.Contains(sparkleDarwin, "selectWindowsInstallerAsset") || strings.Contains(sparkleObjC, "browser_download_url") {
		t.Fatal("Sparkle path must not download installers itself")
	}
	if !strings.Contains(sparkleKeys, "dV+k5IynR3jrGAA7dbDmr66A2rrOH3vPbc45CVcuGUE=") || !strings.Contains(plist, "dV+k5IynR3jrGAA7dbDmr66A2rrOH3vPbc45CVcuGUE=") {
		t.Fatal("Sparkle SUPublicEDKey must match the generated public EdDSA key")
	}
	if !strings.Contains(plist, "172800") || !strings.Contains(plist, "SUFeedURL") {
		t.Fatal("Sparkle must check GitHub Releases about every two days via SUFeedURL")
	}
	for _, want := range []string{"CheckDesktopAppUpdate", "RestartDesktopAppUpdate", "PostponeDesktopAppUpdate"} {
		if !strings.Contains(bridge, want) {
			t.Fatalf("native shell bridge missing %q", want)
		}
	}
	for _, want := range []string{"Restart now", "Later", "allowDownloadLink", "Windows auto-update never uses a download-link CTA"} {
		if !strings.Contains(appSvelte, want) {
			t.Fatalf("update banner missing %q", want)
		}
	}
	if !strings.Contains(appSvelte, "desktopAppUpdate.platform !== 'windows'") {
		t.Fatal("frontend must not render a primary Windows download-link CTA")
	}
	for _, want := range []string{
		"Windows in-app auto-update",
		"writable AppImage",
		"once-per-version",
		"Sparkle",
		"SUPublicEDKey",
		"Restart now",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop architecture doc missing auto-update note %q", want)
		}
	}
	appcastScript := readRequiredFile(t, filepath.Join(root, "scripts", "sign-sparkle-appcast.sh"))
	fetchScript := readRequiredFile(t, filepath.Join(root, "scripts", "fetch-sparkle-framework.sh"))
	if !strings.Contains(release, "secrets.SPARKLE_EDDSA_PRIVATE_KEY") {
		t.Fatal("release workflow must pass SPARKLE_EDDSA_PRIVATE_KEY into Sparkle appcast signing")
	}
	if !strings.Contains(appcastScript, "sign_update") {
		t.Fatal("release appcast signing must call Sparkle sign_update")
	}
	if !strings.Contains(appcastScript, "--ed-key-file -") {
		t.Fatal("appcast signing must prefer sign_update --ed-key-file - stdin")
	}
	if strings.Contains(appcastScript, "signature_line=\"$(") || strings.Contains(appcastScript, "$(\"$SIGN_UPDATE\"") {
		t.Fatal("appcast signing must not capture sign_update via command substitution")
	}
	if !strings.Contains(fetchScript, "Sparkle sign_update is missing or not executable") {
		t.Fatal("Sparkle fetch must refuse to succeed without an executable sign_update")
	}
	if strings.Contains(appcastScript, "awk '/^[A-Za-z0-9+/=]") || strings.Contains(appcastScript, "awk '/^[A-Za-z0-9+/") {
		t.Fatal("appcast signing must not use a BSD-awk-incompatible /regex/ with / inside []")
	}
	if strings.Contains(ci, "SPARKLE_EDDSA_PRIVATE_KEY") || strings.Contains(ci, "notarytool") || strings.Contains(ci, "sign-sparkle-appcast.sh") {
		t.Fatal("PR CI must stay unsigned and must not receive the Sparkle private key")
	}

	privateHits := []string{"SPARKLE_EDDSA_PRIVATE_KEY="}
	for _, rel := range []string{
		"desktop-gui/app_update_sparkle_keys.go",
		"desktop-gui/build/darwin/Info.plist",
		"docs/DESKTOP_GUI_ARCHITECTURE.md",
	} {
		body := readRequiredFile(t, filepath.Join(root, rel))
		if strings.Contains(body, "BEGIN") && strings.Contains(body, "PRIVATE KEY") {
			t.Fatalf("%s looks like it contains private key material", rel)
		}
		for _, hit := range privateHits {
			if strings.Contains(body, hit) && !strings.Contains(rel, "release.yml") {
				t.Fatalf("%s must not embed the Sparkle private key", rel)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "desktop-gui", "third_party", "sparkle", "README.md")); err != nil {
		t.Fatal("Sparkle third_party README is required so the framework is fetched, not committed")
	}
}
