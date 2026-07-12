package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
