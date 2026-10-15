package webgui

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInstallLogsChecksumVerified(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "fse-web.zip")
	checksum := writeZipPackage(t, pkgPath, map[string]string{"index.html": "ok"})
	var log bytes.Buffer
	previous := checksumVerifiedLog
	checksumVerifiedLog = &log
	t.Cleanup(func() { checksumVerifiedLog = previous })
	if _, err := InstallLocalPackage(InstallOptions{PackagePath: pkgPath, InstallDir: filepath.Join(dir, "web"), Version: "1.2.3", ChecksumSHA256: checksum}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "web GUI package checksum verified") {
		t.Fatalf("checksum log = %q", log.String())
	}
}

func TestAPITrustProxyUsesDaemonRoute(t *testing.T) {
	dir := t.TempDir()
	installDir := filepath.Join(dir, "web")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, versionMarkerName), []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	var nativePath, guiHost string
	native := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nativePath = r.URL.Path
		guiHost = r.Header.Get("X-FSE-GUI-Host")
		if r.Host != "fse-native" {
			t.Fatalf("native host = %q", r.Host)
		}
		if r.Header.Get("X-FSE-API-Key") != "native-secret" {
			t.Fatalf("api key = %q", r.Header.Get("X-FSE-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"mode":"auto","tlsEnabled":true,"tlsRequired":true,"trustedCertificateConfigured":false,"trustedCertificateMatches":false}`)
	})
	server := NewServer()
	status, err := server.Start(StartOptions{InstallDir: installDir, Listen: "127.0.0.1:0", NativeAPIHandler: native, NativeAPIKey: "native-secret"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = server.Stop() })
	resp, err := http.Get(status.URL + "/api/v1/api/trust")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || nativePath != "/v1/api/trust" || !strings.Contains(string(body), `"mode":"auto"`) {
		t.Fatalf("trust proxy status=%d native=%s body=%s", resp.StatusCode, nativePath, body)
	}
	if guiHost == "" || guiHost == "fse-native" {
		t.Fatalf("gui host = %q", guiHost)
	}
}

func TestNativeBridgeForwardsConsoleHost(t *testing.T) {
	var gotHost, gotHeader string
	native := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotHeader = r.Header.Get("X-FSE-GUI-Host")
		w.WriteHeader(http.StatusNoContent)
	})
	handler := nativeMethodBridge(native, "native-secret", "/v1/status", []string{http.MethodGet}, nil)
	req := httptest.NewRequest(http.MethodGet, "http://ignored/v1/status", nil)
	req.Host = "192.0.2.20:8385"
	rec := httptest.NewRecorder()
	handler(rec, req)
	if gotHost != "fse-native" || gotHeader != "192.0.2.20:8385" {
		t.Fatalf("host=%q header=%q", gotHost, gotHeader)
	}
	req = httptest.NewRequest(http.MethodGet, "http://ignored/v1/status", nil)
	req.Host = "172.17.0.2:8385"
	req.Header.Set("X-Forwarded-Host", "files.example:8385, proxy.internal")
	rec = httptest.NewRecorder()
	handler(rec, req)
	if gotHeader != "files.example:8385" {
		t.Fatalf("forwarded host = %q", gotHeader)
	}
}

func TestDockerProxyRoutesCoverDaemonAPIClientPaths(t *testing.T) {
	client, err := os.ReadFile(filepath.Join("..", "..", "desktop-gui", "src", "lib", "daemonApi.ts"))
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]struct{}{}
	for _, route := range dockerProxyRoutes() {
		routes[route.NativePath] = struct{}{}
		if !strings.HasPrefix(route.BrowserPath, "/api"+route.NativePath) {
			t.Fatalf("browser path %s must be /api plus %s", route.BrowserPath, route.NativePath)
		}
	}
	for _, forbidden := range []string{"/v1/pair-exchange", "/v1/stop", "/v1/folder-file", "/v1/sync-journal"} {
		if _, ok := routes[forbidden]; ok {
			t.Fatalf("forbidden route %s is proxied", forbidden)
		}
	}
	seen := map[string]struct{}{}
	for _, match := range regexp.MustCompile(`['"](/v1/[^'"?]+)`).FindAllStringSubmatch(string(client), -1) {
		path := match[1]
		seen[path] = struct{}{}
		if _, ok := routes[path]; !ok {
			t.Fatalf("GUI client path %s is not proxied", path)
		}
	}
	if _, ok := seen["/v1/api/trust"]; !ok {
		t.Fatal("daemon API client no longer calls /v1/api/trust")
	}
	req := httptest.NewRequest(http.MethodGet, "http://gui/api/v1/transfers?limit=10&token=nope", nil)
	if got := allowlistedQuery(req, []string{"limit"}); got != "limit=10" {
		t.Fatalf("query allowlist = %q", got)
	}
}
