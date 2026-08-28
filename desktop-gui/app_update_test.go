package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompareDesktopVersionsUnpaddedAndPadded(t *testing.T) {
	if compareDesktopVersions("2026.8.27.2", "2026.08.27.02") != 0 {
		t.Fatal("unpadded YYYY.M.D.N must equal leftover padded YYYY.MM.DD.NN")
	}
	if compareDesktopVersions("2026.8.27.2", "2026.8.28.1") >= 0 {
		t.Fatal("2026.8.28.1 must be newer than 2026.8.27.2")
	}
	if compareDesktopVersions("v2026.8.27.2", "2026.9.1.1") >= 0 {
		t.Fatal("tag prefix must not break date.build compare")
	}
	if parse, ok := parseDesktopVersion("2026.08.11.02"); !ok || parse.String() != "2026.8.11.2" {
		t.Fatalf("padded parse = %#v ok=%v", parse, ok)
	}
}

func TestSelectUpdateAssetsMatchArchAndPreferSparkleZip(t *testing.T) {
	assets := []githubAsset{
		{Name: "fse-desktop-windows-amd64-installer-2026.8.28.1.exe", BrowserDownloadURL: "https://example.test/win-amd64", Digest: "sha256:" + strings.Repeat("a", 64)},
		{Name: "fse-desktop-windows-arm64-installer-2026.8.28.1.exe", BrowserDownloadURL: "https://example.test/win-arm64", Digest: "sha256:" + strings.Repeat("b", 64)},
		{Name: "fse-desktop-linux-amd64-webkit40-installers-2026.8.28.1.AppImage", BrowserDownloadURL: "https://example.test/linux-amd64-40"},
		{Name: "fse-desktop-linux-amd64-webkit41-installers-2026.8.28.1.AppImage", BrowserDownloadURL: "https://example.test/linux-amd64-41"},
		{Name: "fse-desktop-linux-arm64-webkit41-installers-2026.8.28.1.AppImage", BrowserDownloadURL: "https://example.test/linux-arm64-41"},
		{Name: "fse-desktop-darwin-arm64-installer-2026.8.28.1.zip", BrowserDownloadURL: "https://example.test/mac-arm64-zip"},
		{Name: "fse-desktop-darwin-arm64-installer-2026.8.28.1.dmg", BrowserDownloadURL: "https://example.test/mac-arm64-dmg"},
		{Name: "fse-desktop-darwin-amd64-installer-2026.8.28.1.dmg", BrowserDownloadURL: "https://example.test/mac-amd64-dmg"},
	}
	win, err := selectWindowsInstallerAsset(assets, "amd64", "2026.8.28.1")
	if err != nil || win.URL != "https://example.test/win-amd64" || win.Kind != windowsInstallerKind {
		t.Fatalf("windows amd64: %#v %v", win, err)
	}
	winArm, err := selectWindowsInstallerAsset(assets, "arm64", "2026.08.28.01")
	if err != nil || winArm.URL != "https://example.test/win-arm64" {
		t.Fatalf("windows arm64 padded version: %#v %v", winArm, err)
	}
	app41, err := selectLinuxAppImageAsset(assets, "amd64", "webkit41", "2026.8.28.1")
	if err != nil || app41.URL != "https://example.test/linux-amd64-41" {
		t.Fatalf("webkit41: %#v %v", app41, err)
	}
	app40, err := selectLinuxAppImageAsset(assets, "amd64", "webkit40", "2026.8.28.1")
	if err != nil || app40.URL != "https://example.test/linux-amd64-40" {
		t.Fatalf("webkit40: %#v %v", app40, err)
	}
	fallback, err := selectLinuxAppImageAsset(assets, "arm64", "webkit40", "2026.8.28.1")
	if err != nil || fallback.URL != "https://example.test/linux-arm64-41" {
		t.Fatalf("missing webkit40 should fall back to default webkit41 lane: %#v %v", fallback, err)
	}
	macArm, err := selectMacSparklePayload(assets, "arm64", "2026.8.28.1")
	if err != nil || macArm.Kind != macSparkleZipKind || macArm.URL != "https://example.test/mac-arm64-zip" {
		t.Fatalf("mac arm64 must prefer stapled zip: %#v %v", macArm, err)
	}
	macAmd, err := selectMacSparklePayload(assets, "amd64", "2026.8.28.1")
	if err != nil || macAmd.Kind != macSparkleDMGKind {
		t.Fatalf("mac amd64 may use dmg when zip is absent: %#v %v", macAmd, err)
	}
	if _, err := selectWindowsInstallerAsset(assets, "amd64", "2026.1.1.1"); err == nil {
		t.Fatal("missing windows installer must fail closed")
	}
}

func TestGitHubLatestReleaseClientParsesAssetsAndDigests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/bstone108/File-Sync-Engine/releases/latest" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != desktopUpdateUserAgent {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2026.8.28.1",
			"assets": []map[string]any{
				{
					"name":                 "fse-desktop-windows-amd64-installer-2026.8.28.1.exe",
					"browser_download_url": "http://" + r.Host + "/win.exe",
					"digest":               "sha256:" + strings.Repeat("c", 64),
					"size":                 12,
				},
				{
					"name":                 "RELEASE_ASSET_SHA256SUMS",
					"browser_download_url": "http://" + r.Host + "/SUMS",
					"size":                 80,
				},
			},
		})
	}))
	defer server.Close()
	client := &httpGitHubReleaseClient{baseURL: server.URL, owner: defaultGitHubOwner, repo: defaultGitHubRepo, httpClient: server.Client()}
	release, err := client.LatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.Version() != "2026.8.28.1" || len(release.Assets) != 2 || release.Assets[0].SHA256() != strings.Repeat("c", 64) {
		t.Fatalf("release = %#v", release)
	}
}

func TestAppImageWritableDetection(t *testing.T) {
	dir := t.TempDir()
	writable := filepath.Join(dir, "fse-desktop.AppImage")
	if err := os.WriteFile(writable, []byte("appimage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !appImageFileWritable(writable) {
		t.Fatal("user-owned AppImage in a writable directory must be writable")
	}
	lockedDir := filepath.Join(dir, "locked")
	if err := os.Mkdir(lockedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(lockedDir, "fse-desktop.AppImage")
	if err := os.WriteFile(locked, []byte("appimage"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockedDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o700) })
	if appImageFileWritable(locked) {
		t.Fatal("AppImage in a non-writable directory must not be treated as writable")
	}
}

func TestAppImageNotWritableNotifiesOncePerVersion(t *testing.T) {
	tmp := t.TempDir()
	payload := []byte("appimage-bytes")
	sum := sha256.Sum256(payload)
	server := newUpdateFixtureServer(t, payload, map[string]string{
		"fse-desktop-linux-amd64-webkit41-installers-2026.8.28.1.AppImage": hex.EncodeToString(sum[:]),
	})
	defer server.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	app := newUpdateTestApp(t, tmp, &desktopAppUpdateRuntime{
		client:           server.client,
		now:              func() time.Time { return now },
		minGap:           time.Nanosecond,
		goos:             "linux",
		goarch:           "amd64",
		currentVersion:   "2026.8.27.2",
		appImagePath:     func() string { return filepath.Join(tmp, "File-Sync-Engine.AppImage") },
		appImageWritable: func(string) bool { return false },
		webkitLane:       func() string { return "webkit41" },
	})
	first, err := app.CheckDesktopAppUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if first.Phase != "notify" || !first.AllowDownloadLink || first.DownloadURL == "" || first.AvailableVersion != "2026.8.28.1" {
		t.Fatalf("first notify = %#v", first)
	}
	now = now.Add(time.Second)
	second, err := app.CheckDesktopAppUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if second.Phase != "notify" || !strings.Contains(second.Message, "not nagging") {
		t.Fatalf("second check must keep the once-per-version notify: %#v", second)
	}
	state := app.loadDesktopAppUpdateState()
	if state.NotifiedAppImageVersion != "2026.8.28.1" {
		t.Fatalf("persisted notified version = %#v", state)
	}
}

func TestWritableAppImageStagesReplaceWithoutDownloadLink(t *testing.T) {
	tmp := t.TempDir()
	current := filepath.Join(tmp, "File-Sync-Engine.AppImage")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("new-appimage")
	sum := sha256.Sum256(payload)
	server := newUpdateFixtureServer(t, payload, map[string]string{
		"fse-desktop-linux-amd64-webkit41-installers-2026.8.28.1.AppImage": hex.EncodeToString(sum[:]),
	})
	defer server.Close()
	replaced := ""
	app := newUpdateTestApp(t, tmp, &desktopAppUpdateRuntime{
		client:           server.client,
		now:              func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
		minGap:           time.Nanosecond,
		goos:             "linux",
		goarch:           "amd64",
		currentVersion:   "2026.8.27.2",
		appImagePath:     func() string { return current },
		appImageWritable: func(string) bool { return true },
		webkitLane:       func() string { return "webkit41" },
		replaceAppImage: func(running, staged string) error {
			replaced = staged
			return replaceRunningAppImage(running, staged)
		},
		relaunchAppImage: func(string) error { return nil },
	})
	status, err := app.CheckDesktopAppUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != "staged" || status.AllowDownloadLink || status.DownloadURL != "" || !status.CanRestartNow {
		t.Fatalf("writable AppImage must stage in-place: %#v", status)
	}
	if _, err := app.RestartDesktopAppUpdate(); err != nil {
		t.Fatal(err)
	}
	if replaced == "" {
		t.Fatal("Restart now must replace the AppImage")
	}
	got, err := os.ReadFile(current)
	if err != nil || string(got) != "new-appimage" {
		t.Fatalf("replaced contents = %q err=%v", got, err)
	}
}

func TestWindowsUpdateNeverFallsBackToDownloadLink(t *testing.T) {
	tmp := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v2026.8.28.1",
				"assets": []map[string]any{{
					"name":                 "fse-desktop-windows-amd64-installer-2026.8.28.1.exe",
					"browser_download_url": "http://" + r.Host + "/missing.exe",
					"digest":               "sha256:" + strings.Repeat("d", 64),
				}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	app := newUpdateTestApp(t, tmp, &desktopAppUpdateRuntime{
		client:         &httpGitHubReleaseClient{baseURL: server.URL, owner: defaultGitHubOwner, repo: defaultGitHubRepo, httpClient: server.Client()},
		now:            func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
		minGap:         time.Nanosecond,
		goos:           "windows",
		goarch:         "amd64",
		currentVersion: "2026.8.27.2",
		launchWindows:  func(string) error { t.Fatal("must not launch on download failure"); return nil },
	})
	status, err := app.CheckDesktopAppUpdate()
	if err == nil {
		t.Fatal("download failure must be an error")
	}
	if status.AllowDownloadLink || status.DownloadURL != "" || status.CanRestartNow || status.Phase != "error" || !status.CanRetry {
		t.Fatalf("windows download failure must stay in-app retry, no link fallback: %#v", status)
	}
	if !strings.Contains(status.Message, "does not fall back to a download link") {
		t.Fatalf("windows error must state the no-link contract: %#v", status)
	}
	got := app.GetDesktopAppUpdateStatus()
	if got.AllowDownloadLink || got.DownloadURL != "" {
		t.Fatalf("GetDesktopAppUpdateStatus leaked a Windows download link: %#v", got)
	}
}

func TestWindowsRestartNowRunsStagedInstallerNotURL(t *testing.T) {
	tmp := t.TempDir()
	payload := []byte("nsis-installer")
	sum := sha256.Sum256(payload)
	server := newUpdateFixtureServer(t, payload, map[string]string{
		"fse-desktop-windows-arm64-installer-2026.8.28.1.exe": hex.EncodeToString(sum[:]),
	})
	defer server.Close()
	var launched atomic.Pointer[string]
	app := newUpdateTestApp(t, tmp, &desktopAppUpdateRuntime{
		client:         server.client,
		now:            func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
		minGap:         time.Nanosecond,
		goos:           "windows",
		goarch:         "arm64",
		currentVersion: "2026.8.27.2",
		launchWindows: func(path string) error {
			launched.Store(&path)
			return nil
		},
	})
	status, err := app.CheckDesktopAppUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if status.AllowDownloadLink || status.DownloadURL != "" || !status.CanRestartNow {
		t.Fatalf("windows staged status leaked a link: %#v", status)
	}
	if _, err := app.RestartDesktopAppUpdate(); err != nil {
		t.Fatal(err)
	}
	if launched.Load() == nil || !strings.Contains(*launched.Load(), "fse-desktop-windows-arm64-installer-2026.8.28.1.exe") {
		t.Fatalf("Restart now must run the staged installer, got %v", launched.Load())
	}
	cmd, args := windowsInstallerLaunchCommand(`C:\staged\installer.exe`)
	joined := cmd + " " + strings.Join(args, " ")
	if !strings.Contains(joined, "/S") || !strings.Contains(joined, "/RELAUNCH") {
		t.Fatalf("windows restart command must be silent/relaunch, got %q", joined)
	}
	if strings.Contains(joined, "https://") {
		t.Fatalf("windows restart command must not open a URL: %q", joined)
	}
}

func TestWindowsUpdateStatusNeverExposesDownloadLinkFallback(t *testing.T) {
	status := enforceWindowsNoLink(updatePlatformWindows, DesktopAppUpdateStatus{
		Platform:          updatePlatformWindows,
		Phase:             "error",
		AllowDownloadLink: true,
		DownloadURL:       "https://github.com/bstone108/File-Sync-Engine/releases/latest",
		CanRetry:          true,
	})
	if status.AllowDownloadLink || status.DownloadURL != "" {
		t.Fatalf("windows contract violated: %#v", status)
	}
	if windowsUpdateAllowsDownloadLink() {
		t.Fatal("windowsUpdateAllowsDownloadLink must be false")
	}
}

func TestSparklePublicKeyMatchesInfoPlistConstant(t *testing.T) {
	if sparklePublicEDKey != "dV+k5IynR3jrGAA7dbDmr66A2rrOH3vPbc45CVcuGUE=" {
		t.Fatalf("Sparkle public key constant drifted: %s", sparklePublicEDKey)
	}
}

func TestDebounceSkipsRepeatedGitHubChecks(t *testing.T) {
	tmp := t.TempDir()
	payload := []byte("nsis")
	sum := sha256.Sum256(payload)
	var latestHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/latest") {
			latestHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v2026.8.28.1",
				"assets": []map[string]any{{
					"name":                 "fse-desktop-windows-amd64-installer-2026.8.28.1.exe",
					"browser_download_url": "http://" + r.Host + "/payload",
					"digest":               "sha256:" + hex.EncodeToString(sum[:]),
				}},
			})
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	app := newUpdateTestApp(t, tmp, &desktopAppUpdateRuntime{
		client:         &httpGitHubReleaseClient{baseURL: server.URL, owner: defaultGitHubOwner, repo: defaultGitHubRepo, httpClient: server.Client()},
		now:            func() time.Time { return now },
		minGap:         time.Hour,
		goos:           "windows",
		goarch:         "amd64",
		currentVersion: "2026.8.27.2",
		launchWindows:  func(string) error { return nil },
	})
	if _, err := app.CheckDesktopAppUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CheckDesktopAppUpdate(); err != nil {
		t.Fatal(err)
	}
	if latestHits.Load() != 1 {
		t.Fatalf("debounce must not hammer GitHub, hits=%d", latestHits.Load())
	}
}

type updateFixtureServer struct {
	*httptest.Server
	client *httpGitHubReleaseClient
}

func newUpdateFixtureServer(t *testing.T, payload []byte, namedHashes map[string]string) *updateFixtureServer {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/latest") {
			assets := make([]map[string]any, 0, len(namedHashes)+1)
			var sums strings.Builder
			for name, hash := range namedHashes {
				assets = append(assets, map[string]any{
					"name":                 name,
					"browser_download_url": "http://" + r.Host + "/payload/" + name,
					"digest":               "sha256:" + hash,
					"size":                 len(payload),
				})
				fmt.Fprintf(&sums, "%s  %s\n", hash, name)
			}
			assets = append(assets, map[string]any{
				"name":                 "RELEASE_ASSET_SHA256SUMS",
				"browser_download_url": "http://" + r.Host + "/SUMS",
			})
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2026.8.28.1", "assets": assets})
			return
		}
		if r.URL.Path == "/SUMS" {
			var body strings.Builder
			for name, hash := range namedHashes {
				fmt.Fprintf(&body, "%s  %s\n", hash, name)
			}
			_, _ = io.WriteString(w, body.String())
			return
		}
		_, _ = w.Write(payload)
	}))
	return &updateFixtureServer{
		Server: server,
		client: &httpGitHubReleaseClient{baseURL: server.URL, owner: defaultGitHubOwner, repo: defaultGitHubRepo, httpClient: server.Client()},
	}
}

func newUpdateTestApp(t *testing.T, stateRoot string, update *desktopAppUpdateRuntime) *App {
	t.Helper()
	app := NewApp()
	app.ctx = context.Background()
	app.desktop = &desktopNativeRuntime{stateRoot: stateRoot, update: update}
	return app
}
