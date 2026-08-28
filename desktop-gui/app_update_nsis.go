package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func windowsInstallerLaunchCommand(installerPath string) (string, []string) {
	// Delay so the GUI can exit and unlock the installed files, then run the
	// staged NSIS installer silently and relaunch.
	script := fmt.Sprintf(
		`ping -n 3 127.0.0.1 >nul & "%s" /S /RELAUNCH`,
		installerPath,
	)
	return "cmd.exe", []string{"/C", "start", "", "cmd.exe", "/C", script}
}

func launchStagedWindowsInstaller(installerPath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windows installer launch is only available on Windows")
	}
	name, args := windowsInstallerLaunchCommand(installerPath)
	cmd := exec.Command(name, args...)
	cmd.Dir = filepath.Dir(installerPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start staged Windows installer: %w", err)
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func windowsUpdateAllowsDownloadLink() bool {
	return false
}
