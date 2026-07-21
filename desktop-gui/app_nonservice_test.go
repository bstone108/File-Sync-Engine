package main

import (
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestGUIOwnedNonServiceDaemonLaunchStartsSeparateVerifiedDaemonAndPersistsSession(t *testing.T) {
	tmp := t.TempDir()
	engine := filepath.Join(tmp, "resources", "engine", runtimeTargetOS(), runtimeTargetArch(), runtimeExecutableName())
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var started launchRecord
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		resourceRoot: filepath.Join(tmp, "resources", "engine"),
		stateRoot:    filepath.Join(tmp, "state"),
		launcher: func(command string, args []string, env []string) (int, error) {
			started = launchRecord{command: command, args: args, env: env}
			return 4242, nil
		},
		probeSession: func(GUIManagedNonServiceDaemonSession) (DaemonRuntimeState, error) {
			return DaemonRuntimeState{ConnectionState: "running", NodeName: "desktop"}, nil
		},
	}

	session, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{
		SessionMode:                   "persistent-user-daemon",
		PreferExistingReachableDaemon: true,
	})
	if err != nil {
		t.Fatalf("launch returned error: %v", err)
	}
	if session.PID != 4242 {
		t.Fatalf("PID = %d, want 4242", session.PID)
	}
	if session.SessionID == "" {
		t.Fatalf("expected session id")
	}
	if session.EncryptedAPIBaseURL == "" || !strings.HasPrefix(session.EncryptedAPIBaseURL, "https://127.0.0.1:") {
		t.Fatalf("encrypted API base URL = %q", session.EncryptedAPIBaseURL)
	}
	if !strings.HasPrefix(session.CredentialRef, "native://fse-desktop/gui-owned/") {
		t.Fatalf("credential ref = %q", session.CredentialRef)
	}
	if started.command != engine {
		t.Fatalf("launched command = %q, want %q", started.command, engine)
	}
	if !containsArg(started.args, "start") || !containsArg(started.args, session.ConfigPath) {
		t.Fatalf("launch args = %#v, want start and config path %q", started.args, session.ConfigPath)
	}
	configBytes, err := os.ReadFile(session.ConfigPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	configText := string(configBytes)
	if !strings.Contains(configText, "127.0.0.1") || !strings.Contains(configText, "manual-tls") {
		t.Fatalf("generated config does not describe local encrypted API: %s", configText)
	}
	if !strings.Contains(configText, `"certFile"`) || !strings.Contains(configText, `"keyFile"`) || strings.Contains(configText, `"certificatePath"`) || strings.Contains(configText, `"privateKeyPath"`) {
		t.Fatalf("generated config does not use the daemon's real TLS config keys: %s", configText)
	}
	persisted, err := app.GetGUIOwnedNonServiceDaemonSession()
	if err != nil {
		t.Fatalf("persisted session returned error: %v", err)
	}
	if persisted == nil || persisted.SessionID != session.SessionID || !persisted.ReconnectOnNextLaunch {
		t.Fatalf("persisted session = %#v, want reconnectable session %q", persisted, session.SessionID)
	}
}

func TestDesktopRuntimeUsesSiblingEngineDirectoryForPackagedGUI(t *testing.T) {
	tmp := t.TempDir()
	executable := filepath.Join(tmp, "app", "fse-desktop.exe")
	engineRoot := filepath.Join(tmp, "engine")
	if err := os.MkdirAll(engineRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("desktop"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := desktopEngineResourceRootForExecutable(executable); got != engineRoot {
		t.Fatalf("packaged engine root = %q, want sibling %q", got, engineRoot)
	}
}

func TestAdoptGUIOwnedNonServiceDaemonReturnsPersistedSessionByID(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	want := GUIManagedNonServiceDaemonSession{SessionID: "session-123", PID: 99, EncryptedAPIBaseURL: "https://127.0.0.1:18001", CredentialRef: "native://fse-desktop/gui-owned/session-123/api-key", ConfigPath: filepath.Join(tmp, "config.jsonc"), StatePath: tmp, SessionMode: "persistent-user-daemon", ReconnectOnNextLaunch: true}
	if err := app.desktop.saveSession(want); err != nil {
		t.Fatalf("save session: %v", err)
	}
	got, err := app.AdoptGUIOwnedNonServiceDaemon("session-123")
	if err != nil {
		t.Fatalf("adopt returned error: %v", err)
	}
	if got.SessionID != want.SessionID || got.PID != want.PID || got.EncryptedAPIBaseURL != want.EncryptedAPIBaseURL {
		t.Fatalf("adopted session = %#v, want %#v", got, want)
	}
}

func TestStopGUIOwnedNonServiceDaemonThroughAPIPostsStopAndMarksTemporarySessionStopped(t *testing.T) {
	tmp := t.TempDir()
	var authHeader string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stop" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s, want POST /v1/stop", r.Method, r.URL.Path)
		}
		authHeader = r.Header.Get("X-FSE-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, stopClient: server.Client(), credentialResolver: func(ref string) (string, error) {
		if ref != "native://fse-desktop/gui-owned/session-123/api-key" {
			t.Fatalf("credential ref = %q", ref)
		}
		return "test-api-key", nil
	}}
	stored := GUIManagedNonServiceDaemonSession{SessionID: "session-123", PID: 100, EncryptedAPIBaseURL: server.URL, CredentialRef: "native://fse-desktop/gui-owned/session-123/api-key", ConfigPath: filepath.Join(tmp, "config.jsonc"), StatePath: tmp, SessionMode: "temporary-session-only", ReconnectOnNextLaunch: false}
	if err := app.desktop.saveSession(stored); err != nil {
		t.Fatalf("save session: %v", err)
	}
	stopped, err := app.StopGUIOwnedNonServiceDaemonThroughAPI("session-123")
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if authHeader != "test-api-key" {
		t.Fatalf("X-FSE-API-Key = %q", authHeader)
	}
	if stopped.PID != 0 || !strings.Contains(stopped.Message, "stop requested") {
		t.Fatalf("stopped session = %#v", stopped)
	}
	persisted, err := app.GetGUIOwnedNonServiceDaemonSession()
	if err != nil {
		t.Fatalf("get persisted: %v", err)
	}
	if persisted != nil {
		t.Fatalf("temporary session should be cleared after stop, got %#v", persisted)
	}
}

func TestStopGUIOwnedNonServiceDaemonThroughAPITrustsPersistedSessionCertificate(t *testing.T) {
	tmp := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(tmp, "api.crt"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "api-key"), []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: filepath.Join(tmp, "state")}
	session := GUIManagedNonServiceDaemonSession{SessionID: "tls", PID: 55, EncryptedAPIBaseURL: server.URL, StatePath: tmp, SessionMode: "temporary-session-only"}
	if err := app.desktop.saveSession(session); err != nil {
		t.Fatal(err)
	}
	if _, err := app.StopGUIOwnedNonServiceDaemonThroughAPI(session.SessionID); err != nil {
		t.Fatalf("stop with persisted certificate: %v", err)
	}
}

func TestRequestGUIOwnedNonServiceDaemonLaunchDoesNotAdoptUnreachablePersistedSession(t *testing.T) {
	tmp := t.TempDir()
	engine := filepath.Join(tmp, "resources", "engine", runtimeTargetOS(), runtimeTargetArch(), runtimeExecutableName())
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	launches := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		resourceRoot: filepath.Join(tmp, "resources", "engine"), stateRoot: filepath.Join(tmp, "state"),
		launcher: func(string, []string, []string) (int, error) { launches++; return 5252, nil },
		probeSession: func(session GUIManagedNonServiceDaemonSession) (DaemonRuntimeState, error) {
			if session.PID == 99 {
				return DaemonRuntimeState{}, errors.New("connection refused")
			}
			return DaemonRuntimeState{ConnectionState: "running", NodeName: "replacement"}, nil
		},
	}
	stale := GUIManagedNonServiceDaemonSession{SessionID: "stale", PID: 99, EncryptedAPIBaseURL: "https://127.0.0.1:1", StatePath: tmp, SessionMode: "persistent-user-daemon", ReconnectOnNextLaunch: true}
	if err := app.desktop.saveSession(stale); err != nil {
		t.Fatal(err)
	}
	got, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{PreferExistingReachableDaemon: true})
	if err != nil {
		t.Fatalf("launch replacement: %v", err)
	}
	if launches != 1 || got.SessionID == stale.SessionID || got.PID != 5252 {
		t.Fatalf("got session %#v with %d launches; stale session was incorrectly adopted", got, launches)
	}
}

func TestRequestGUIOwnedNonServiceDaemonLaunchWaitsForReachableAPI(t *testing.T) {
	tmp := t.TempDir()
	engine := filepath.Join(tmp, "resources", "engine", runtimeTargetOS(), runtimeTargetArch(), runtimeExecutableName())
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	probes := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		resourceRoot: filepath.Join(tmp, "resources", "engine"), stateRoot: filepath.Join(tmp, "state"),
		launcher: func(string, []string, []string) (int, error) { return 6262, nil },
		probeSession: func(GUIManagedNonServiceDaemonSession) (DaemonRuntimeState, error) {
			probes++
			if probes < 3 {
				return DaemonRuntimeState{}, errors.New("starting")
			}
			return DaemonRuntimeState{ConnectionState: "running", NodeName: "desktop"}, nil
		}, readinessAttempts: 3,
	}
	got, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if probes != 3 || got.ConnectionState != "running" || got.NodeName != "desktop" {
		t.Fatalf("session returned before real API readiness: probes=%d session=%#v", probes, got)
	}
}

func TestRequestGUIOwnedNonServiceDaemonLaunchCleansUpFailedStartupBeforeReturning(t *testing.T) {
	tmp := t.TempDir()
	engine := filepath.Join(tmp, "resources", "engine", runtimeTargetOS(), runtimeTargetArch(), runtimeExecutableName())
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	terminatedPID := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		resourceRoot: filepath.Join(tmp, "resources", "engine"),
		stateRoot:    filepath.Join(tmp, "state"),
		launcher:     func(string, []string, []string) (int, error) { return 7373, nil },
		terminateProcess: func(pid int) error {
			terminatedPID = pid
			return nil
		},
		probeSession: func(GUIManagedNonServiceDaemonSession) (DaemonRuntimeState, error) {
			return DaemonRuntimeState{}, errors.New("connection refused")
		},
		readinessAttempts: 1,
	}

	if _, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{}); err == nil || !strings.Contains(err.Error(), "did not become reachable") {
		t.Fatalf("expected actionable readiness error, got %v", err)
	}
	if session, err := app.GetGUIOwnedNonServiceDaemonSession(); err != nil || session != nil {
		t.Fatalf("failed startup must not remain reconnectable: session=%#v err=%v", session, err)
	}
	if terminatedPID != 7373 {
		t.Fatalf("failed startup did not terminate the owned daemon process: pid=%d", terminatedPID)
	}
}

func TestGetGUIOwnedNonServiceDaemonStateReportsRealAPIStatus(t *testing.T) {
	tmp := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-FSE-API-Key") != "test-key" {
			t.Fatalf("missing native daemon API auth")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"nodeName": "live-node", "status": "running", "folders": []any{}})
	}))
	defer server.Close()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, statusClient: server.Client()}
	session := GUIManagedNonServiceDaemonSession{SessionID: "live", PID: 44, EncryptedAPIBaseURL: server.URL, CredentialRef: "native://live", StatePath: tmp, SessionMode: "persistent-user-daemon"}
	if err := os.WriteFile(filepath.Join(tmp, "api-key"), []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.desktop.saveSession(session); err != nil {
		t.Fatal(err)
	}
	state, err := app.GetGUIOwnedNonServiceDaemonState()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.ConnectionState != "running" || state.NodeName != "live-node" || state.PID != 44 {
		t.Fatalf("state = %#v", state)
	}
}

type launchRecord struct {
	command string
	args    []string
	env     []string
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
