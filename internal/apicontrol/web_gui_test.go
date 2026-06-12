package apicontrol

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/webgui"
)

func TestHandleWebGUICommandReportsDisabledHeadlessStatus(t *testing.T) {
	cfg := config.Config{WebGUI: config.WebGUIConfig{Enabled: false}}
	manager := webgui.NewServer()

	response, err := HandleWebGUICommandWithManager(cfg, api.WebGUICommandRequest{Action: "status"}, manager, nil)
	if err != nil {
		t.Fatalf("disabled status: %v", err)
	}
	if response.Status != "disabled" || response.Running || response.Message != "web GUI is disabled; core daemon is running headless" {
		t.Fatalf("disabled headless status not explicit: %+v", response)
	}
	if response.InstallDir != "" || response.Version != "" || response.URL != "" {
		t.Fatalf("disabled headless status should not report package/runtime details: %+v", response)
	}
}

func TestHandleWebGUICommandInstallsTrustedLocalPackage(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "fse-web.zip")
	checksum := writeWebGUITestZipPackage(t, pkgPath, map[string]string{"index.html": "web", "VERSION": "1.2.3"})
	installDir := filepath.Join(dir, "web", "current")
	cfg := config.Config{WebGUI: config.WebGUIConfig{Enabled: true, Version: "1.2.3", PackagePath: pkgPath, InstallDir: installDir, ChecksumSHA256: checksum}}

	response, err := HandleWebGUICommand(cfg, api.WebGUICommandRequest{Action: "install"})
	if err != nil {
		t.Fatalf("install web GUI package: %v", err)
	}
	if response.Status != "installed" || response.Version != "1.2.3" || response.InstallDir != installDir {
		t.Fatalf("unexpected install response: %+v", response)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "index.html")); err != nil || string(got) != "web" {
		t.Fatalf("installed package mismatch: %q err=%v", string(got), err)
	}
}

func writeWebGUITestZipPackage(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(out)
	for name, content := range files {
		entry, err := zipWriter.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var _ *http.Client
