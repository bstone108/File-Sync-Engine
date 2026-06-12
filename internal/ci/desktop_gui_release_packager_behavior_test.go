package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopGUIReleasePackagerPreflightsAllTargetsBeforeReplacingOutput(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	wailsRoot := filepath.Join(tmp, "wails-output")
	engineRoot := filepath.Join(tmp, "engine")
	outDir := filepath.Join(tmp, "out")
	mustMkdir(t, outDir)
	mustWriteFile(t, filepath.Join(outDir, "existing-sentinel.txt"), "keep me")
	mustWriteFile(t, filepath.Join(engineRoot, "manifest.json"), "{}")

	mustWriteFile(t, filepath.Join(wailsRoot, "linux-amd64", "fse-desktop"), "linux-amd64")
	mustWriteFile(t, filepath.Join(wailsRoot, "linux-arm64", "fse-desktop"), "linux-arm64")
	mustWriteFile(t, filepath.Join(wailsRoot, "windows-amd64", "fse-desktop.exe"), "windows-amd64")
	mustWriteFile(t, filepath.Join(wailsRoot, "windows-arm64", "fse-desktop.exe"), "windows-arm64")
	for _, rel := range []string{
		"linux/amd64/fse",
		"linux/arm64/fse",
		"darwin/amd64/fse",
		"darwin/arm64/fse",
		"windows/amd64/fse.exe",
		"windows/arm64/fse.exe",
	} {
		mustWriteFile(t, filepath.Join(engineRoot, rel), rel)
	}

	cmd := exec.Command("bash", "scripts/package-desktop-gui-release.sh", "0.1.99-test", wailsRoot, engineRoot, outDir)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("packager unexpectedly succeeded without Darwin Wails outputs:\n%s", output)
	}
	got := string(output)
	for _, want := range []string{
		"missing Wails output for darwin-amd64",
		"missing Wails output for darwin-arm64",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("packager did not report %q before aborting; output:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "existing-sentinel.txt")); err != nil {
		t.Fatalf("packager removed existing output before preflight completed: %v\noutput:\n%s", err, got)
	}
}

func TestDesktopGUIReleasePackagerRejectsEmptyTargetExecutable(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	wailsRoot := filepath.Join(tmp, "wails-output")
	engineRoot := filepath.Join(tmp, "engine")
	outDir := filepath.Join(tmp, "out")
	mustMkdir(t, outDir)
	mustWriteFile(t, filepath.Join(outDir, "existing-sentinel.txt"), "keep me")
	mustWriteFile(t, filepath.Join(engineRoot, "manifest.json"), "{}")

	mustWriteFile(t, filepath.Join(wailsRoot, "linux-amd64", "fse-desktop"), "")
	mustWriteFile(t, filepath.Join(wailsRoot, "linux-arm64", "fse-desktop"), "linux-arm64")
	mustWriteFile(t, filepath.Join(wailsRoot, "windows-amd64", "fse-desktop.exe"), "windows-amd64")
	mustWriteFile(t, filepath.Join(wailsRoot, "windows-arm64", "fse-desktop.exe"), "windows-arm64")
	mustWriteFile(t, filepath.Join(wailsRoot, "darwin-amd64", "fse-desktop.app", "Contents", "MacOS", "fse-desktop"), "darwin-amd64")
	mustWriteFile(t, filepath.Join(wailsRoot, "darwin-arm64", "fse-desktop.app", "Contents", "MacOS", "fse-desktop"), "darwin-arm64")
	for _, rel := range []string{
		"linux/amd64/fse",
		"linux/arm64/fse",
		"darwin/amd64/fse",
		"darwin/arm64/fse",
		"windows/amd64/fse.exe",
		"windows/arm64/fse.exe",
	} {
		mustWriteFile(t, filepath.Join(engineRoot, rel), rel)
	}

	cmd := exec.Command("bash", "scripts/package-desktop-gui-release.sh", "0.1.99-test", wailsRoot, engineRoot, outDir)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("packager unexpectedly succeeded with an empty Linux Wails executable:\n%s", output)
	}
	got := string(output)
	if !strings.Contains(got, "empty Wails executable for linux-amd64") {
		t.Fatalf("packager did not report empty linux-amd64 executable; output:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(outDir, "existing-sentinel.txt")); err != nil {
		t.Fatalf("packager removed existing output before preflight completed: %v\noutput:\n%s", err, got)
	}
}

func TestDesktopGUIReleasePackagerCanPackageExplicitVerifiedTargetSubset(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	wailsRoot := filepath.Join(tmp, "wails-output")
	engineRoot := filepath.Join(tmp, "engine")
	outDir := "desktop-gui-subset-test-output"
	outDirAbs := filepath.Join(root, outDir)
	t.Cleanup(func() { _ = os.RemoveAll(outDirAbs) })
	mustWriteFile(t, filepath.Join(engineRoot, "manifest.json"), "{}")

	mustWriteFile(t, filepath.Join(wailsRoot, "linux-amd64", "fse-desktop"), "linux-amd64")
	mustWriteFile(t, filepath.Join(wailsRoot, "windows-arm64", "fse-desktop.exe"), "windows-arm64")
	mustWriteFile(t, filepath.Join(engineRoot, "linux", "amd64", "fse"), "linux engine")
	mustWriteFile(t, filepath.Join(engineRoot, "windows", "arm64", "fse.exe"), "windows engine")

	cmd := exec.Command("bash", "scripts/package-desktop-gui-release.sh", "0.1.99-test", wailsRoot, engineRoot, outDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "FSE_DESKTOP_GUI_RELEASE_TARGETS=linux-amd64,windows-arm64")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("packager failed for explicit verified target subset:\n%s", output)
	}
	for _, target := range []string{"linux-amd64", "windows-arm64"} {
		zipPath := filepath.Join(outDirAbs, "fse-desktop-0.1.99-test-"+target+".zip")
		if info, err := os.Stat(zipPath); err != nil || info.Size() == 0 {
			t.Fatalf("missing non-empty archive for %s at %s (info=%v err=%v)\noutput:\n%s", target, zipPath, info, err, output)
		}
	}
	if _, err := os.Stat(filepath.Join(outDirAbs, "fse-desktop-0.1.99-test-darwin-amd64.zip")); !os.IsNotExist(err) {
		t.Fatalf("subset packaging unexpectedly wrote a darwin archive: %v\noutput:\n%s", err, output)
	}
	sha, err := os.ReadFile(filepath.Join(outDirAbs, "SHA256SUMS"))
	if err != nil {
		t.Fatalf("missing SHA256SUMS for subset packaging: %v\noutput:\n%s", err, output)
	}
	if got := string(sha); !strings.Contains(got, "linux-amd64") || !strings.Contains(got, "windows-arm64") || strings.Contains(got, "darwin") {
		t.Fatalf("unexpected subset SHA256SUMS contents:\n%s\noutput:\n%s", got, output)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
