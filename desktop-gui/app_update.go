package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	desktopUpdateCheckInterval = 12 * time.Hour
	desktopUpdateMinGap        = 6 * time.Hour
	updatePlatformWindows      = "windows"
	updatePlatformAppImage     = "appimage"
	updatePlatformSparkle      = "sparkle"
	updatePlatformUnsupported  = "unsupported"
)

var (
	errUpdateNotStaged = errors.New("no staged desktop update is ready to restart")
	errWindowsNoLink   = errors.New("Windows auto-update does not fall back to a download link")
)

type DesktopAppUpdateStatus struct {
	Platform          string `json:"platform"`
	Phase             string `json:"phase"`
	CurrentVersion    string `json:"currentVersion"`
	AvailableVersion  string `json:"availableVersion,omitempty"`
	Message           string `json:"message"`
	DownloadURL       string `json:"downloadURL,omitempty"`
	AllowDownloadLink bool   `json:"allowDownloadLink"`
	CanRestartNow     bool   `json:"canRestartNow"`
	CanRetry          bool   `json:"canRetry"`
}

type desktopAppUpdateRuntime struct {
	mu               sync.Mutex
	status           DesktopAppUpdateStatus
	client           githubReleaseClient
	now              func() time.Time
	minGap           time.Duration
	goos             string
	goarch           string
	currentVersion   string
	appImagePath     func() string
	appImageWritable func(string) bool
	webkitLane       func() string
	launchWindows    func(string) error
	replaceAppImage  func(current, staged string) error
	relaunchAppImage func(string) error
	exit             func()
	stagedPath       string
	stagedVersion    string
}

func currentDesktopVersion() string {
	if parsed, ok := parseDesktopVersion(desktopAppVersion); ok {
		return parsed.String()
	}
	return strings.TrimSpace(desktopAppVersion)
}

func (a *App) updater() *desktopAppUpdateRuntime {
	rt := a.desktopRuntime()
	if rt.update == nil {
		rt.update = &desktopAppUpdateRuntime{
			client:           newHTTPGitHubReleaseClient(),
			now:              time.Now,
			minGap:           desktopUpdateMinGap,
			goos:             runtime.GOOS,
			goarch:           runtimeDesktopArch(),
			currentVersion:   currentDesktopVersion(),
			appImagePath:     runningAppImagePath,
			appImageWritable: appImageFileWritable,
			webkitLane:       detectLinuxWebKitLane,
			launchWindows:    launchStagedWindowsInstaller,
			replaceAppImage:  replaceRunningAppImage,
			relaunchAppImage: relaunchAppImage,
			exit:             func() { os.Exit(0) },
			status: DesktopAppUpdateStatus{
				Platform:          a.desktopUpdatePlatform(),
				Phase:             "idle",
				CurrentVersion:    currentDesktopVersion(),
				Message:           "Desktop auto-update has not checked yet.",
				AllowDownloadLink: false,
			},
		}
	}
	if rt.update.client == nil {
		rt.update.client = newHTTPGitHubReleaseClient()
	}
	if rt.update.now == nil {
		rt.update.now = time.Now
	}
	if rt.update.minGap == 0 {
		rt.update.minGap = desktopUpdateMinGap
	}
	if rt.update.goos == "" {
		rt.update.goos = runtime.GOOS
	}
	if rt.update.goarch == "" {
		rt.update.goarch = runtimeDesktopArch()
	}
	if rt.update.currentVersion == "" {
		rt.update.currentVersion = currentDesktopVersion()
	}
	return rt.update
}

func (a *App) desktopUpdatePlatform() string {
	switch runtime.GOOS {
	case "windows":
		return updatePlatformWindows
	case "darwin":
		return updatePlatformSparkle
	case "linux":
		if runningAppImagePath() != "" || os.Getenv("APPIMAGE") != "" {
			return updatePlatformAppImage
		}
		return updatePlatformUnsupported
	default:
		return updatePlatformUnsupported
	}
}

func (a *App) GetDesktopAppUpdateStatus() DesktopAppUpdateStatus {
	if sparklePlatformSupported() && a.updater().goos == "darwin" {
		status := snapshotSparkleStatus()
		status.AllowDownloadLink = false
		status.DownloadURL = ""
		return status
	}
	u := a.updater()
	u.mu.Lock()
	defer u.mu.Unlock()
	status := u.status
	if u.goos == "windows" {
		status.AllowDownloadLink = false
		status.DownloadURL = ""
	}
	return status
}

func (a *App) CheckDesktopAppUpdate() (DesktopAppUpdateStatus, error) {
	if a.updater().goos == "darwin" {
		_ = startSparkleUpdater()
		checkSparkleUpdates()
		return a.GetDesktopAppUpdateStatus(), nil
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.checkDesktopAppUpdate(ctx, false)
}

func (a *App) RestartDesktopAppUpdate() (DesktopAppUpdateStatus, error) {
	if a.updater().goos == "darwin" {
		if err := restartSparkleUpdate(); err != nil {
			return a.GetDesktopAppUpdateStatus(), err
		}
		return a.GetDesktopAppUpdateStatus(), nil
	}
	u := a.updater()
	u.mu.Lock()
	path := u.stagedPath
	version := u.stagedVersion
	platform := u.status.Platform
	u.mu.Unlock()
	if path == "" {
		status := a.failUpdate("No staged installer is ready. Retry the download.", true)
		return status, errUpdateNotStaged
	}
	switch platform {
	case updatePlatformWindows:
		if u.launchWindows == nil {
			status := a.failUpdate("Windows installer launcher is unavailable.", true)
			return status, errWindowsNoLink
		}
		if err := u.launchWindows(path); err != nil {
			status := a.failUpdate("Could not start the staged Windows installer: "+err.Error(), true)
			return status, err
		}
		return a.setUpdateStatus(DesktopAppUpdateStatus{
			Platform:          updatePlatformWindows,
			Phase:             "restarting",
			CurrentVersion:    u.currentVersion,
			AvailableVersion:  version,
			Message:           "Restarting into the staged Windows installer.",
			AllowDownloadLink: false,
			CanRestartNow:     false,
		}), nil
	case updatePlatformAppImage:
		current := ""
		if u.appImagePath != nil {
			current = u.appImagePath()
		}
		if u.replaceAppImage == nil {
			return a.failUpdate("AppImage replace is unavailable.", true), errUpdateNotStaged
		}
		if err := u.replaceAppImage(current, path); err != nil {
			return a.failUpdate("Could not replace the AppImage: "+err.Error(), true), err
		}
		if u.relaunchAppImage != nil {
			_ = u.relaunchAppImage(current)
		}
		return a.setUpdateStatus(DesktopAppUpdateStatus{
			Platform:          updatePlatformAppImage,
			Phase:             "restarting",
			CurrentVersion:    u.currentVersion,
			AvailableVersion:  version,
			Message:           "Replaced the AppImage and relaunching.",
			AllowDownloadLink: false,
			CanRestartNow:     false,
		}), nil
	default:
		return a.GetDesktopAppUpdateStatus(), errUpdateNotStaged
	}
}

func (a *App) PostponeDesktopAppUpdate() DesktopAppUpdateStatus {
	if a.updater().goos == "darwin" {
		postponeSparkleUpdate()
		return a.GetDesktopAppUpdateStatus()
	}
	u := a.updater()
	u.mu.Lock()
	defer u.mu.Unlock()
	state := a.loadDesktopAppUpdateState()
	state.PostponedVersion = u.status.AvailableVersion
	_ = a.saveDesktopAppUpdateState(state)
	u.status.CanRestartNow = false
	u.status.Phase = "idle"
	u.status.Message = "Update postponed. The staged payload remains available until the next restart."
	return u.status
}

func (a *App) checkDesktopAppUpdate(ctx context.Context, ignoreGap bool) (DesktopAppUpdateStatus, error) {
	u := a.updater()
	platform := u.goos
	switch platform {
	case "windows":
		return a.checkAndStageGitHubUpdate(ctx, ignoreGap, updatePlatformWindows)
	case "linux":
		path := ""
		if u.appImagePath != nil {
			path = u.appImagePath()
		}
		if path == "" {
			return a.setUpdateStatus(DesktopAppUpdateStatus{
				Platform:          updatePlatformUnsupported,
				Phase:             "idle",
				CurrentVersion:    u.currentVersion,
				Message:           "In-app Linux auto-update applies to writable AppImage builds. Installed .deb/.rpm packages are unchanged.",
				AllowDownloadLink: false,
			}), nil
		}
		return a.checkAndStageGitHubUpdate(ctx, ignoreGap, updatePlatformAppImage)
	default:
		return a.setUpdateStatus(DesktopAppUpdateStatus{
			Platform:          updatePlatformUnsupported,
			Phase:             "idle",
			CurrentVersion:    u.currentVersion,
			Message:           "In-app auto-update is not available on this platform.",
			AllowDownloadLink: false,
		}), nil
	}
}

func (a *App) checkAndStageGitHubUpdate(ctx context.Context, ignoreGap bool, platform string) (DesktopAppUpdateStatus, error) {
	u := a.updater()
	state := a.loadDesktopAppUpdateState()
	if !ignoreGap {
		last := parseUpdateTime(state.LastCheckAt)
		if !last.IsZero() && u.now().Sub(last) < u.minGap {
			u.mu.Lock()
			defer u.mu.Unlock()
			if u.status.Phase == "" {
				u.status.Phase = "idle"
				u.status.Message = "Using the last GitHub Releases check; not hammering the API."
			}
			return enforceWindowsNoLink(platform, u.status), nil
		}
	}

	a.setUpdateStatus(DesktopAppUpdateStatus{
		Platform:          platform,
		Phase:             "checking",
		CurrentVersion:    u.currentVersion,
		Message:           "Checking GitHub Releases for a newer desktop build.",
		AllowDownloadLink: false,
	})

	release, err := u.client.LatestRelease(ctx)
	if err != nil {
		return a.failUpdate("Could not read the latest GitHub Release: "+err.Error(), true), err
	}
	state.LastCheckAt = u.now().UTC().Format(time.RFC3339)
	_ = a.saveDesktopAppUpdateState(state)

	remoteVersion := release.Version()
	if compareDesktopVersions(u.currentVersion, remoteVersion) >= 0 {
		return a.setUpdateStatus(DesktopAppUpdateStatus{
			Platform:          platform,
			Phase:             "idle",
			CurrentVersion:    u.currentVersion,
			AvailableVersion:  remoteVersion,
			Message:           "This desktop build is up to date.",
			AllowDownloadLink: false,
		}), nil
	}

	sums := a.loadReleaseChecksums(ctx, release)
	asset, err := a.selectPlatformAsset(release, platform, remoteVersion)
	if err != nil {
		return a.failUpdate(err.Error(), true), err
	}
	applyChecksumsToAsset(&asset, sums)

	if platform == updatePlatformAppImage {
		path := u.appImagePath()
		writable := u.appImageWritable != nil && u.appImageWritable(path)
		if !writable {
			return a.notifyAppImageDownloadOnce(state, remoteVersion, asset.URL), nil
		}
	}

	staged, hash, err := a.downloadAndVerifyAsset(ctx, asset)
	if err != nil {
		msg := "Could not download or verify the update: " + err.Error()
		if platform == updatePlatformWindows {
			msg += " Retry from the app; Windows does not fall back to a download link."
		}
		return a.failUpdate(msg, true), err
	}
	u.mu.Lock()
	u.stagedPath = staged
	u.stagedVersion = remoteVersion
	u.mu.Unlock()
	state.StagedPath = staged
	state.StagedVersion = remoteVersion
	state.StagedAssetName = asset.Name
	state.StagedSHA256 = hash
	_ = a.saveDesktopAppUpdateState(state)

	return a.setUpdateStatus(DesktopAppUpdateStatus{
		Platform:          platform,
		Phase:             "staged",
		CurrentVersion:    u.currentVersion,
		AvailableVersion:  remoteVersion,
		Message:           "Update " + remoteVersion + " is staged. Restart now or later.",
		AllowDownloadLink: false,
		CanRestartNow:     true,
		CanRetry:          false,
	}), nil
}

func (a *App) selectPlatformAsset(release githubRelease, platform, version string) (selectedUpdateAsset, error) {
	u := a.updater()
	switch platform {
	case updatePlatformWindows:
		return selectWindowsInstallerAsset(release.Assets, u.goarch, version)
	case updatePlatformAppImage:
		lane := defaultLinuxWebKitLane
		if u.webkitLane != nil {
			lane = u.webkitLane()
		}
		return selectLinuxAppImageAsset(release.Assets, u.goarch, lane, version)
	default:
		return selectedUpdateAsset{}, fmt.Errorf("unsupported auto-update platform %s", platform)
	}
}

func (a *App) loadReleaseChecksums(ctx context.Context, release githubRelease) map[string]string {
	u := a.updater()
	sums := map[string]string{}
	for _, asset := range release.Assets {
		if hash := asset.SHA256(); hash != "" {
			sums[asset.Name] = hash
			sums[strings.ToLower(asset.Name)] = hash
		}
	}
	for _, name := range []string{"RELEASE_ASSET_SHA256SUMS", "SHA256SUMS"} {
		asset, err := firstNamedAsset(release.Assets, []string{name})
		if err != nil {
			continue
		}
		body, err := u.client.Download(ctx, asset.BrowserDownloadURL)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(body, 1<<20))
		_ = body.Close()
		if readErr != nil {
			continue
		}
		for file, hash := range checksumMapFromSHA256SUMS(string(data)) {
			sums[file] = hash
		}
	}
	return sums
}

func (a *App) downloadAndVerifyAsset(ctx context.Context, asset selectedUpdateAsset) (string, string, error) {
	u := a.updater()
	if asset.SHA256 == "" {
		return "", "", fmt.Errorf("release asset %s has no SHA-256 digest or SHA256SUMS entry", asset.Name)
	}
	root, err := a.desktopRuntime().ensureStateRoot()
	if err != nil {
		return "", "", err
	}
	dest := filepath.Join(root, "updates", asset.Name)
	body, err := u.client.Download(ctx, asset.URL)
	if err != nil {
		return "", "", err
	}
	defer body.Close()
	got, err := hashReaderToFile(body, dest)
	if err != nil {
		return "", "", err
	}
	if !strings.EqualFold(got, asset.SHA256) {
		_ = os.Remove(dest)
		return "", "", fmt.Errorf("SHA-256 mismatch for %s", asset.Name)
	}
	if err := verifySHA256(dest, asset.SHA256); err != nil {
		_ = os.Remove(dest)
		return "", "", err
	}
	if strings.HasSuffix(strings.ToLower(dest), ".appimage") {
		_ = os.Chmod(dest, 0o755)
	}
	return dest, got, nil
}

func (a *App) notifyAppImageDownloadOnce(state desktopAppUpdateState, version, url string) DesktopAppUpdateStatus {
	u := a.updater()
	already := state.NotifiedAppImageVersion != "" && compareDesktopVersions(state.NotifiedAppImageVersion, version) >= 0
	if !already {
		state.NotifiedAppImageVersion = version
		_ = a.saveDesktopAppUpdateState(state)
	}
	message := "This AppImage is not writable, so it cannot replace itself. Download " + version + " once and replace the file manually."
	if already {
		message = "Already notified for AppImage " + version + "; not nagging again."
	}
	return a.setUpdateStatus(DesktopAppUpdateStatus{
		Platform:          updatePlatformAppImage,
		Phase:             "notify",
		CurrentVersion:    u.currentVersion,
		AvailableVersion:  version,
		Message:           message,
		DownloadURL:       url,
		AllowDownloadLink: true,
		CanRestartNow:     false,
		CanRetry:          false,
	})
}

func (a *App) failUpdate(message string, retry bool) DesktopAppUpdateStatus {
	u := a.updater()
	platform := u.status.Platform
	if platform == "" {
		if u.goos == "windows" {
			platform = updatePlatformWindows
		} else if u.goos == "linux" {
			platform = updatePlatformAppImage
		}
	}
	return a.setUpdateStatus(DesktopAppUpdateStatus{
		Platform:          platform,
		Phase:             "error",
		CurrentVersion:    u.currentVersion,
		Message:           message,
		AllowDownloadLink: false,
		CanRetry:          retry,
		CanRestartNow:     false,
	})
}

func (a *App) setUpdateStatus(status DesktopAppUpdateStatus) DesktopAppUpdateStatus {
	status = enforceWindowsNoLink(status.Platform, status)
	u := a.updater()
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status = status
	return status
}

func enforceWindowsNoLink(platform string, status DesktopAppUpdateStatus) DesktopAppUpdateStatus {
	if platform == updatePlatformWindows || status.Platform == updatePlatformWindows {
		status.AllowDownloadLink = false
		status.DownloadURL = ""
		status.Platform = updatePlatformWindows
	}
	if status.Platform == updatePlatformSparkle {
		status.AllowDownloadLink = false
		status.DownloadURL = ""
	}
	return status
}
