//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -F${SRCDIR}/third_party/sparkle
#cgo LDFLAGS: -F${SRCDIR}/third_party/sparkle -framework Sparkle -framework Foundation -framework AppKit -Wl,-rpath,@executable_path/../Frameworks
#include "app_update_sparkle_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"sync"
	"unsafe"
)

var (
	sparkleMu      sync.Mutex
	sparkleStatus  DesktopAppUpdateStatus
	sparkleStarted bool
)

func sparklePlatformSupported() bool { return true }

func startSparkleUpdater() error {
	sparkleMu.Lock()
	defer sparkleMu.Unlock()
	if C.FSESparkleStart() == 0 {
		sparkleStatus = DesktopAppUpdateStatus{
			Platform:          "sparkle",
			Phase:             "error",
			CurrentVersion:    currentDesktopVersion(),
			Message:           "Sparkle failed to start in this macOS build.",
			AllowDownloadLink: false,
			CanRetry:          true,
		}
		return nil
	}
	sparkleStarted = true
	if sparkleStatus.Phase == "" {
		sparkleStatus = DesktopAppUpdateStatus{
			Platform:          "sparkle",
			Phase:             "idle",
			CurrentVersion:    currentDesktopVersion(),
			Message:           "Sparkle will check GitHub Releases about every two days.",
			AllowDownloadLink: false,
		}
	}
	return nil
}

func checkSparkleUpdates() {
	_ = startSparkleUpdater()
	C.FSESparkleCheck()
}

func restartSparkleUpdate() error {
	if C.FSESparkleRestartNow() == 0 {
		return errSparkleNotReady()
	}
	return nil
}

func postponeSparkleUpdate() {
	C.FSESparklePostpone()
	sparkleMu.Lock()
	defer sparkleMu.Unlock()
	sparkleStatus.Phase = "idle"
	sparkleStatus.CanRestartNow = false
	sparkleStatus.Message = "macOS update postponed. Sparkle kept the staged payload for later."
}

func snapshotSparkleStatus() DesktopAppUpdateStatus {
	sparkleMu.Lock()
	defer sparkleMu.Unlock()
	status := sparkleStatus
	status.Platform = "sparkle"
	status.AllowDownloadLink = false
	status.DownloadURL = ""
	if status.CurrentVersion == "" {
		status.CurrentVersion = currentDesktopVersion()
	}
	return status
}

func errSparkleNotReady() error {
	return errUpdateNotStaged
}

//export fseSparkleStatusChanged
func fseSparkleStatusChanged() {
	version := C.FSESparkleVersion()
	message := C.FSESparkleMessage()
	defer C.free(unsafe.Pointer(version))
	defer C.free(unsafe.Pointer(message))
	sparkleMu.Lock()
	defer sparkleMu.Unlock()
	sparkleStatus.Platform = "sparkle"
	sparkleStatus.CurrentVersion = currentDesktopVersion()
	sparkleStatus.AvailableVersion = C.GoString(version)
	sparkleStatus.Message = C.GoString(message)
	sparkleStatus.AllowDownloadLink = false
	sparkleStatus.DownloadURL = ""
	sparkleStatus.CanRetry = C.FSESparkleError() != 0
	if C.FSESparkleReady() != 0 {
		sparkleStatus.Phase = "staged"
		sparkleStatus.CanRestartNow = true
	} else if C.FSESparkleError() != 0 {
		sparkleStatus.Phase = "error"
		sparkleStatus.CanRestartNow = false
	} else if sparkleStatus.AvailableVersion != "" {
		sparkleStatus.Phase = "downloading"
		sparkleStatus.CanRestartNow = false
	}
}
