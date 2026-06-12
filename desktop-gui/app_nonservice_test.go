package main

import (
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
	persisted, err := app.GetGUIOwnedNonServiceDaemonSession()
	if err != nil {
		t.Fatalf("persisted session returned error: %v", err)
	}
	if persisted == nil || persisted.SessionID != session.SessionID || !persisted.ReconnectOnNextLaunch {
		t.Fatalf("persisted session = %#v, want reconnectable session %q", persisted, session.SessionID)
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
		authHeader = r.Header.Get("X-API-Key")
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
		t.Fatalf("X-API-Key = %q", authHeader)
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
