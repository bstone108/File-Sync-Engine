package webgui

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallLocalPackageVerifiesChecksumAndExtractsAtomically(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "fse-web.zip")
	checksum := writeZipPackage(t, pkgPath, map[string]string{
		"index.html":    "<h1>FSE Web</h1>",
		"assets/app.js": "console.log('fse')",
		"VERSION":       "1.2.3",
	})

	installDir := filepath.Join(dir, "web", "current")
	result, err := InstallLocalPackage(InstallOptions{PackagePath: pkgPath, InstallDir: installDir, Version: "1.2.3", ChecksumSHA256: checksum})
	if err != nil {
		t.Fatalf("InstallLocalPackage: %v", err)
	}
	if result.Status != "installed" || result.Version != "1.2.3" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "index.html")); err != nil || string(got) != "<h1>FSE Web</h1>" {
		t.Fatalf("installed index mismatch: %q err=%v", string(got), err)
	}
	if got, err := InstalledVersion(installDir); err != nil || got != "1.2.3" {
		t.Fatalf("InstalledVersion=%q err=%v", got, err)
	}
}

func TestInstallLocalPackageRejectsChecksumMismatchAndZipSlip(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "bad-web.zip")
	checksum := writeZipPackage(t, pkgPath, map[string]string{
		"../escape.txt": "owned",
		"index.html":    "bad",
	})
	installDir := filepath.Join(dir, "web", "current")

	if _, err := InstallLocalPackage(InstallOptions{PackagePath: pkgPath, InstallDir: installDir, Version: "1.2.3", ChecksumSHA256: "0000000000000000000000000000000000000000000000000000000000000000"}); err == nil {
		t.Fatalf("expected checksum mismatch to be rejected")
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Fatalf("install dir should not be created on checksum mismatch: %v", err)
	}
	if _, err := InstallLocalPackage(InstallOptions{PackagePath: pkgPath, InstallDir: installDir, Version: "1.2.3", ChecksumSHA256: checksum}); err == nil {
		t.Fatalf("expected zip-slip package path to be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("zip-slip path escaped install root: %v", err)
	}
}

func TestInstallLocalPackageRejectsCrossPlatformUnsafeZipNames(t *testing.T) {
	dir := t.TempDir()
	installDir := filepath.Join(dir, "web", "current")
	cases := []string{
		`..\escape.txt`,
		`assets\..\escape.txt`,
		`C:\Users\Public\escape.txt`,
		`\absolute\escape.txt`,
	}
	for _, entryName := range cases {
		t.Run(entryName, func(t *testing.T) {
			pkgPath := filepath.Join(dir, "bad-web.zip")
			checksum := writeZipPackage(t, pkgPath, map[string]string{
				entryName:    "owned",
				"index.html": "bad",
			})
			if _, err := InstallLocalPackage(InstallOptions{PackagePath: pkgPath, InstallDir: installDir, Version: "1.2.3", ChecksumSHA256: checksum}); err == nil {
				t.Fatalf("expected unsafe package path %q to be rejected", entryName)
			}
			if _, err := os.Stat(filepath.Join(installDir, entryName)); !os.IsNotExist(err) {
				t.Fatalf("unsafe zip entry %q was created under install root: %v", entryName, err)
			}
		})
	}
}

func TestInstallRemotePackageFetchesHTTPSAndVerifiesChecksum(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "fse-web.zip")
	checksum := writeZipPackage(t, pkgPath, map[string]string{"index.html": "remote-web", "VERSION": "2.0.0"})
	pkgBytes, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fse-web.zip" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(pkgBytes)
	}))
	defer server.Close()

	installDir := filepath.Join(dir, "web", "current")
	result, err := InstallRemotePackage(InstallRemoteOptions{UpdateURL: server.URL + "/fse-web.zip", InstallDir: installDir, Version: "2.0.0", ChecksumSHA256: checksum, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("InstallRemotePackage: %v", err)
	}
	if result.Status != "installed" || result.Version != "2.0.0" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "index.html")); err != nil || string(got) != "remote-web" {
		t.Fatalf("installed remote package mismatch: %q err=%v", string(got), err)
	}
}

func TestServerStartStopAndHealth(t *testing.T) {
	dir := t.TempDir()
	installDir := filepath.Join(dir, "web", "current")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, versionMarkerName), []byte("3.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "index.html"), []byte("web app"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewServer()
	status, err := server.Start(StartOptions{InstallDir: installDir, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !status.Running || status.Version != "3.0.0" || status.URL == "" {
		t.Fatalf("unexpected running status: %+v", status)
	}
	resp, err := http.Get(status.URL + "/health")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp, err = http.Get(status.URL + "/")
	if err != nil {
		t.Fatalf("index request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "web app" {
		t.Fatalf("index body=%q err=%v", string(body), err)
	}
	stopped, err := server.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Running || stopped.Status != "stopped" {
		t.Fatalf("unexpected stopped status: %+v", stopped)
	}
}

func TestServerStartHostsHTTPAndHTTPSWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	installDir := filepath.Join(dir, "web", "current")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, versionMarkerName), []byte("4.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "index.html"), []byte("web app tls"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewServer()
	status, err := server.Start(StartOptions{InstallDir: installDir, Listen: "127.0.0.1:0", HTTPSListen: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("Start HTTP+HTTPS: %v", err)
	}
	if status.URL == "" || status.HTTPSURL == "" || status.HTTPSListen == "" {
		t.Fatalf("status missing dual listener URLs: %+v", status)
	}
	resp, err := http.Get(status.URL + "/health")
	if err != nil {
		t.Fatalf("HTTP health request: %v", err)
	}
	_ = resp.Body.Close()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err = client.Get(status.HTTPSURL + "/health")
	if err != nil {
		t.Fatalf("HTTPS health request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTPS health status=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if _, err := server.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func writeZipPackage(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(bytes))
}
