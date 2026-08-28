package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func runningAppImagePath() string {
	if path := strings.TrimSpace(os.Getenv("APPIMAGE")); path != "" {
		return path
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	// Typical AppImage layout: the mounted squashfs lives under /tmp/.mount_*
	// while APPIMAGE points at the original file. Without APPIMAGE, treat a
	// writable *.AppImage executable as the running payload.
	if strings.HasSuffix(strings.ToLower(exe), ".appimage") {
		return exe
	}
	if strings.Contains(exe, "/.mount_") || strings.Contains(exe, "squashfs-root") {
		if parent := strings.TrimSpace(os.Getenv("OWD")); parent != "" {
			matches, _ := filepath.Glob(filepath.Join(parent, "*.AppImage"))
			if len(matches) == 1 {
				return matches[0]
			}
		}
	}
	return ""
}

func appImageFileWritable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	dir := filepath.Dir(path)
	dirInfo, err := os.Stat(dir)
	if err != nil || !dirInfo.IsDir() {
		return false
	}
	tmp, err := os.CreateTemp(dir, ".fse-appimage-write-test-*")
	if err != nil {
		return false
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpName)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

func replaceRunningAppImage(currentPath, stagedPath string) error {
	if currentPath == "" || stagedPath == "" {
		return fmt.Errorf("AppImage replace requires the running path and a staged file")
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return err
	}
	backup := currentPath + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(currentPath, backup); err != nil {
		return fmt.Errorf("stage current AppImage aside: %w", err)
	}
	if err := os.Rename(stagedPath, currentPath); err != nil {
		_ = os.Rename(backup, currentPath)
		return fmt.Errorf("replace AppImage: %w", err)
	}
	_ = os.Remove(backup)
	if err := os.Chmod(currentPath, 0o755); err != nil {
		return err
	}
	return nil
}

func relaunchAppImage(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch AppImage: %w", err)
	}
	go func() {
		os.Exit(0)
	}()
	return nil
}

func detectLinuxWebKitLane() string {
	if lane := strings.TrimSpace(os.Getenv("FSE_DESKTOP_LINUX_WEBKIT_LANE")); lane == "webkit40" || lane == "webkit41" {
		return lane
	}
	if runtime.GOOS != "linux" {
		return defaultLinuxWebKitLane
	}
	exe, err := os.Executable()
	if err == nil {
		if data, readErr := os.ReadFile(exe); readErr == nil {
			text := string(data)
			if strings.Contains(text, "libwebkit2gtk-4.0") && !strings.Contains(text, "libwebkit2gtk-4.1") {
				return "webkit40"
			}
			if strings.Contains(text, "libwebkit2gtk-4.1") {
				return "webkit41"
			}
		}
	}
	if data, err := os.ReadFile("/proc/self/maps"); err == nil {
		text := string(data)
		if strings.Contains(text, "libwebkit2gtk-4.0") && !strings.Contains(text, "libwebkit2gtk-4.1") {
			return "webkit40"
		}
		if strings.Contains(text, "libwebkit2gtk-4.1") {
			return "webkit41"
		}
	}
	return defaultLinuxWebKitLane
}
