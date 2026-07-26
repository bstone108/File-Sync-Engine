package main

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWindowsBundledDaemonLifecycleSmoke(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native Windows smoke only")
	}
	engineSource := os.Getenv("FSE_WINDOWS_SMOKE_ENGINE")
	if engineSource == "" {
		t.Fatal("FSE_WINDOWS_SMOKE_ENGINE is required")
	}
	data, err := os.ReadFile(engineSource)
	if err != nil {
		t.Fatalf("read built Windows daemon: %v", err)
	}
	root := t.TempDir()
	resourceRoot := filepath.Join(root, "engine")
	enginePath := filepath.Join(resourceRoot, "windows", "amd64", "fse.exe")
	if err := os.MkdirAll(filepath.Dir(enginePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(enginePath, data, 0o700); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.desktop = &desktopNativeRuntime{resourceRoot: resourceRoot, stateRoot: filepath.Join(root, "state"), serviceCandidates: []localDaemonCandidate{}}
	session, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{SessionMode: "temporary-session-only", PreferExistingReachableDaemon: true})
	if err != nil {
		t.Fatalf("launch bundled daemon: %v", err)
	}
	defer func() { _, _ = app.StopGUIOwnedNonServiceDaemonThroughAPI(session.SessionID) }()

	response, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: session.EncryptedAPIBaseURL, CredentialRef: session.CredentialRef, Method: http.MethodGet, Path: "/v1/status"})
	if err != nil {
		t.Fatalf("native authenticated status proxy: %v", err)
	}
	if response.Status != http.StatusOK || len(response.Body) == 0 {
		t.Fatalf("status response = %#v", response)
	}
	stopped, err := app.StopGUIOwnedNonServiceDaemonThroughAPI(session.SessionID)
	if err != nil {
		t.Fatalf("native authenticated stop: %v", err)
	}
	if stopped.PID != 0 {
		t.Fatalf("stopped PID = %d, want 0", stopped.PID)
	}
}
