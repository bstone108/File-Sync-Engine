package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteCredentialVaultStoresMetadataWithoutReturningSecret(t *testing.T) {
	var storedService, storedAccount, storedSecret string
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform: "linux",
		credentialVaultSet: func(service, account, secret string) error {
			storedService, storedAccount, storedSecret = service, account, secret
			return nil
		},
	}
	record := RemoteInstanceCredentialRecord{
		CredentialRef: "desktop-vault:remote:home-nas",
		InstanceID:    "remote-home-nas",
		Label:         "Home NAS",
		CreatedAt:     time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		UpdatedAt:     time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	got, err := app.StoreRemoteInstanceCredential(record, RemoteInstanceCredentialSecret{CredentialRef: record.CredentialRef, SecretValue: "remote-api-secret"})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if got.Platform != "linux" || got.CredentialRef != record.CredentialRef || storedService != remoteCredentialVaultService || storedAccount != record.CredentialRef || storedSecret != "remote-api-secret" {
		t.Fatalf("record=%#v service=%q account=%q secret=%q", got, storedService, storedAccount, storedSecret)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "remote-api-secret") {
		t.Fatalf("metadata leaked secret: %s", encoded)
	}
}

func TestRemoteCredentialVaultRejectsMalformedReferenceBeforeBackendCall(t *testing.T) {
	calls := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{credentialVaultSet: func(string, string, string) error { calls++; return nil }}
	_, err := app.StoreRemoteInstanceCredential(
		RemoteInstanceCredentialRecord{CredentialRef: "desktop-vault:local-api-key", InstanceID: "remote-a", Label: "Remote A"},
		RemoteInstanceCredentialSecret{CredentialRef: "desktop-vault:local-api-key", SecretValue: "secret"},
	)
	if err == nil || calls != 0 {
		t.Fatalf("expected fail-closed validation, err=%v calls=%d", err, calls)
	}
}

func TestDaemonAPIProxyResolvesRemoteCredentialInsideNativeBoundary(t *testing.T) {
	var auth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("X-FSE-API-Key")
		_ = json.NewEncoder(w).Encode(map[string]any{"nodeName": "remote-node"})
	}))
	defer server.Close()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		apiClient: server.Client(),
		credentialVaultGet: func(service, account string) (string, error) {
			if service != remoteCredentialVaultService || account != "desktop-vault:remote:home-nas" {
				t.Fatalf("service=%q account=%q", service, account)
			}
			return "vault-api-secret", nil
		},
	}
	response, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: server.URL, CredentialRef: "desktop-vault:remote:home-nas", Method: "GET", Path: "/v1/status"})
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if auth != "vault-api-secret" || strings.Contains(string(response.Body), "vault-api-secret") {
		t.Fatalf("auth=%q body=%s", auth, response.Body)
	}
}

func TestDeleteRemoteCredentialUsesVaultAndTreatsMissingAsSuccess(t *testing.T) {
	deleted := ""
	app := NewApp()
	app.desktop = &desktopNativeRuntime{credentialVaultDelete: func(service, account string) error {
		deleted = service + ":" + account
		return errCredentialNotFound
	}}
	if err := app.DeleteRemoteInstanceCredential("desktop-vault:remote:home-nas"); err != nil {
		t.Fatalf("delete missing credential: %v", err)
	}
	if deleted != remoteCredentialVaultService+":desktop-vault:remote:home-nas" {
		t.Fatalf("deleted=%q", deleted)
	}
}

func TestBundledManifestInspectionReadsAndHashesPackagedResources(t *testing.T) {
	tmp := t.TempDir()
	enginePath := filepath.Join(tmp, "linux", "amd64", "fse")
	if err := os.MkdirAll(filepath.Dir(enginePath), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("engine-binary")
	if err := os.WriteFile(enginePath, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	manifest := map[string]any{"version": "1.2.3", "entries": []map[string]any{
		{"target": "linux-amd64", "relativePath": "linux/amd64/fse", "expectedExecutable": "fse", "expectedVersion": "1.2.3", "expectedSHA256": fmt.Sprintf("%x", sum)},
		{"target": "windows-amd64", "relativePath": "windows/amd64/fse.exe", "expectedExecutable": "fse.exe", "expectedVersion": "1.2.3", "expectedSHA256": strings.Repeat("0", 64)},
	}}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.desktop = &desktopNativeRuntime{resourceRoot: tmp}
	got, err := app.InspectBundledEngineResources()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !got.Verified || got.Version != "1.2.3" || len(got.Entries) != 2 || !got.Entries[0].Exists || !got.Entries[0].Verified || got.Entries[1].Exists {
		t.Fatalf("inspection = %#v", got)
	}
}

func TestBundledManifestInspectionRejectsEscapingResourcePath(t *testing.T) {
	tmp := t.TempDir()
	manifest := `{"version":"1","entries":[{"target":"linux-amd64","relativePath":"../outside","expectedExecutable":"fse","expectedVersion":"1","expectedSHA256":"00"}]}`
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.desktop = &desktopNativeRuntime{resourceRoot: tmp}
	if _, err := app.InspectBundledEngineResources(); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected unsafe path rejection, got %v", err)
	}
}

func TestDesktopPreferencesPersistThroughNativeBoundary(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	want := DesktopPreferences{Theme: "dark", Density: "compact", MinimizeToTray: true, NotificationsEnabled: false}
	if got, err := app.SaveDesktopPreferences(want); err != nil || got != want {
		t.Fatalf("save = %#v, %v", got, err)
	}
	reloaded := NewApp()
	reloaded.desktop = &desktopNativeRuntime{stateRoot: tmp}
	if got, err := reloaded.GetDesktopPreferences(); err != nil || got != want {
		t.Fatalf("reload = %#v, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(tmp, "desktop-preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestDesktopPreferencesRejectUnsupportedValuesWithoutReplacingSavedState(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	want := DesktopPreferences{Theme: "system", Density: "comfortable", NotificationsEnabled: true}
	if _, err := app.SaveDesktopPreferences(want); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveDesktopPreferences(DesktopPreferences{Theme: "script:bad", Density: "compact"}); err == nil {
		t.Fatal("expected validation error")
	}
	if got, err := app.GetDesktopPreferences(); err != nil || got != want {
		t.Fatalf("saved state changed: %#v, %v", got, err)
	}
}

func TestRequestGUIOwnedNonServiceDaemonLaunchPrefersReachableInstalledService(t *testing.T) {
	launches := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform:          "linux",
		serviceCandidates: []localDaemonCandidate{{ID: "systemd-user:fse", Kind: "service", Manager: "systemd-user", ServiceName: "fse", APIBaseURL: "https://127.0.0.1:22420", CredentialRef: "config://key"}},
		probeCandidate: func(candidate localDaemonCandidate) (DaemonRuntimeState, error) {
			return DaemonRuntimeState{ConnectionState: "running", NodeName: "service-node"}, nil
		},
		launcher: func(string, []string, []string) (int, error) { launches++; return 42, nil },
	}
	got, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{PreferExistingReachableDaemon: true})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if launches != 0 || got.Kind != "service" || got.Manager != "systemd-user" || got.SessionID != "systemd-user:fse" {
		t.Fatalf("got %#v after %d portable launches", got, launches)
	}
}

func TestDiscoverLocalDaemonPrefersReachableServiceOverPortableSession(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		stateRoot: tmp,
		platform:  "linux",
		serviceCandidates: []localDaemonCandidate{
			{ID: "systemd-user:fse", Kind: "service", Manager: "systemd-user", ServiceName: "fse", APIBaseURL: "https://127.0.0.1:22420", CredentialRef: "file://service-key", StatePath: tmp},
		},
		probeCandidate: func(candidate localDaemonCandidate) (DaemonRuntimeState, error) {
			if candidate.Kind == "service" {
				return DaemonRuntimeState{ConnectionState: "running", NodeName: "installed", Source: candidate.ID, Manager: candidate.Manager, ServiceName: candidate.ServiceName}, nil
			}
			return DaemonRuntimeState{}, errors.New("unexpected portable probe")
		},
	}
	if err := app.desktop.saveSession(GUIManagedNonServiceDaemonSession{SessionID: "portable", PID: 99}); err != nil {
		t.Fatal(err)
	}

	got, err := app.DiscoverLocalDaemon()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got.Source != "systemd-user:fse" || got.NodeName != "installed" || got.Manager != "systemd-user" {
		t.Fatalf("discovered %#v, want reachable service", got)
	}
}

func TestControlLocalDaemonRunsPlatformManagerAndReturnsRefreshedStatus(t *testing.T) {
	var commands [][]string
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform:          "linux",
		serviceCandidates: []localDaemonCandidate{{ID: "systemd-user:fse", Kind: "service", Manager: "systemd-user", ServiceName: "fse"}},
		commandRunner: func(name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return []byte("active\n"), nil
		},
		probeCandidate: func(candidate localDaemonCandidate) (DaemonRuntimeState, error) {
			return DaemonRuntimeState{ConnectionState: "running", Source: candidate.ID, Manager: candidate.Manager, ServiceName: candidate.ServiceName}, nil
		},
	}

	got, err := app.ControlLocalDaemon(LocalDaemonControlRequest{Action: "restart", Source: "systemd-user:fse"})
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if len(commands) != 1 || strings.Join(commands[0], " ") != "systemctl --user restart fse" {
		t.Fatalf("commands = %#v", commands)
	}
	if got.ConnectionState != "running" {
		t.Fatalf("status = %#v", got)
	}
}

func TestControlLocalDaemonReportsActionableManagerFailure(t *testing.T) {
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform:          "linux",
		serviceCandidates: []localDaemonCandidate{{ID: "systemd:fse", Kind: "service", Manager: "systemd", ServiceName: "fse"}},
		commandRunner: func(name string, args ...string) ([]byte, error) {
			return []byte("Access denied"), errors.New("exit status 1")
		},
	}
	_, err := app.ControlLocalDaemon(LocalDaemonControlRequest{Action: "start", Source: "systemd:fse"})
	if err == nil || !strings.Contains(err.Error(), "systemctl start fse") || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("expected actionable manager failure, got %v", err)
	}
}

func TestDaemonAPIProxyUsesNativeCredentialAndCorrectHeaderWithoutReturningSecret(t *testing.T) {
	var header string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-FSE-API-Key")
		if r.URL.Path != "/v1/folders" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"folders": []any{map[string]any{"id": "docs"}}})
	}))
	defer server.Close()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		apiClient: server.Client(),
		credentialResolver: func(ref string) (string, error) {
			if ref != "native://local" {
				t.Fatalf("ref = %q", ref)
			}
			return "native-secret", nil
		},
	}
	response, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: server.URL, CredentialRef: "native://local", Method: "GET", Path: "/v1/folders"})
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if header != "native-secret" {
		t.Fatalf("auth header = %q", header)
	}
	if strings.Contains(string(response.Body), "native-secret") {
		t.Fatalf("proxy leaked credential: %s", response.Body)
	}
}

func TestDaemonAPIProxyRestrictsMethodPathAndBody(t *testing.T) {
	app := NewApp()
	for _, request := range []NativeDaemonAPIRequest{
		{APIBaseURL: "https://127.0.0.1", CredentialRef: "x", Method: "DELETE", Path: "/v1/config"},
		{APIBaseURL: "https://127.0.0.1", CredentialRef: "x", Method: "GET", Path: "/v1/folder-file"},
		{APIBaseURL: "https://127.0.0.1", CredentialRef: "x", Method: "POST", Path: "/v1/peer-command", Body: make([]byte, maxNativeProxyBodyBytes+1)},
	} {
		if _, err := app.DaemonAPIRequest(request); err == nil {
			t.Fatalf("request should be rejected: %#v", request)
		}
	}
}

func TestLoadConfiguredServiceCandidateReadsJSONCWithoutExposingAPIKey(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.jsonc")
	if err := os.WriteFile(configPath, []byte(`{
		// service config
		"nodeName":"service-node",
		"api":{"listen":"127.0.0.1:22420","apiKey":"super-secret","encryption":{"mode":"manual-tls","certFile":"/tmp/api.crt"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := loadConfiguredServiceCandidate("systemd-user:fse", "systemd-user", "fse", configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	encoded, _ := json.Marshal(candidate)
	if candidate.APIBaseURL != "https://127.0.0.1:22420" || !strings.HasPrefix(candidate.CredentialRef, "config://") {
		t.Fatalf("candidate = %#v", candidate)
	}
	if strings.Contains(string(encoded), "super-secret") {
		t.Fatalf("candidate leaked secret: %s", encoded)
	}
}

func TestDaemonAPIProxyPreservesDaemonErrorMessage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"folder id is required"}`)
	}))
	defer server.Close()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{apiClient: server.Client(), credentialResolver: func(string) (string, error) { return "secret", nil }}
	_, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: server.URL, CredentialRef: "native://local", Method: "POST", Path: "/v1/folder-command", Body: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "folder id is required") {
		t.Fatalf("error = %v", err)
	}
}
