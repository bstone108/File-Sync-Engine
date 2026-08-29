//go:build !darwin || !cgo

package main

func sparklePlatformSupported() bool { return false }

func startSparkleUpdater() error { return nil }

func checkSparkleUpdates() {}

func restartSparkleUpdate() error { return errUpdateNotStaged }

func postponeSparkleUpdate() {}

func snapshotSparkleStatus() DesktopAppUpdateStatus {
	return DesktopAppUpdateStatus{
		Platform:          "sparkle",
		Phase:             "idle",
		CurrentVersion:    currentDesktopVersion(),
		Message:           "Sparkle is only linked in native macOS desktop builds.",
		AllowDownloadLink: false,
	}
}
