package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	root, err := os.MkdirTemp("", "fse-windows-lifecycle-")
	if err != nil {
		t.Fatalf("create Windows lifecycle smoke workspace: %v", err)
	}
	defer func() {
		if t.Failed() {
			logPath := filepath.Join(root, "state", "sessions")
			matches, _ := filepath.Glob(filepath.Join(logPath, "*", "logs", "daemon.jsonl"))
			for _, match := range matches {
				if bytes, readErr := os.ReadFile(match); readErr == nil {
					t.Logf("daemon startup log (%s):\n%s", match, string(bytes))
				}
			}
		}
		_ = os.RemoveAll(root)
	}()
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

	status := windowsSmokeProxy(t, app, session, http.MethodGet, "/v1/status", nil)
	if status.Status != http.StatusOK || len(status.Body) == 0 {
		t.Fatalf("status response = %#v", status)
	}

	syncView := windowsSmokeProxy(t, app, session, http.MethodGet, "/v1/config-sync", nil)
	var view struct {
		LocalInstanceID string `json:"localInstanceId"`
	}
	if err := json.Unmarshal(syncView.Body, &view); err != nil || view.LocalInstanceID == "" {
		t.Fatalf("config sync view = %s %v", syncView.Body, err)
	}
	edit, err := json.Marshal(map[string]any{"targetInstanceId": view.LocalInstanceID, "path": "logging.level", "value": "debug"})
	if err != nil {
		t.Fatal(err)
	}
	windowsSmokeProxy(t, app, session, http.MethodPost, "/v1/config-sync/edit", edit)
	configView := windowsSmokeProxy(t, app, session, http.MethodGet, "/v1/config", nil)
	var redacted struct {
		Logging struct {
			Level string `json:"level"`
		} `json:"logging"`
	}
	if err := json.Unmarshal(configView.Body, &redacted); err != nil || redacted.Logging.Level != "debug" {
		t.Fatalf("managed logging level = %s %v", configView.Body, err)
	}

	folderPath := filepath.Join(root, "shared")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	folderBody, err := json.Marshal(map[string]any{"action": "add", "id": "shared", "path": folderPath, "mode": "sendrecv"})
	if err != nil {
		t.Fatal(err)
	}
	windowsSmokeProxy(t, app, session, http.MethodPost, "/v1/folder-command", folderBody)
	folders := windowsSmokeProxy(t, app, session, http.MethodGet, "/v1/folders", nil)
	if !strings.Contains(string(folders.Body), "shared") {
		t.Fatalf("folders = %s", folders.Body)
	}
	journal := windowsSmokeEngine(t, app, session, http.MethodGet, "/v1/sync-journal?folder=shared", "")
	if journal.status != http.StatusOK || !strings.Contains(string(journal.body), `"folder"`) {
		t.Fatalf("sync journal = %d %s", journal.status, journal.body)
	}
	rejectedConfig := windowsSmokeEngine(t, app, session, http.MethodGet, "/v1/config", "not-the-daemon-key")
	if rejectedConfig.status != http.StatusUnauthorized {
		t.Fatalf("wrong key config = %d %s", rejectedConfig.status, rejectedConfig.body)
	}
	rejectedJournal := windowsSmokeEngine(t, app, session, http.MethodGet, "/v1/sync-journal?folder=shared", "not-the-daemon-key")
	if rejectedJournal.status != http.StatusUnauthorized {
		t.Fatalf("wrong key sync journal = %d %s", rejectedJournal.status, rejectedJournal.body)
	}

	stopped, err := app.StopGUIOwnedNonServiceDaemonThroughAPI(session.SessionID)
	if err != nil {
		t.Fatalf("native authenticated stop: %v", err)
	}
	if stopped.PID != 0 {
		t.Fatalf("stopped PID = %d, want 0", stopped.PID)
	}
}

func windowsSmokeProxy(t *testing.T, app *App, session GUIManagedNonServiceDaemonSession, method, path string, body []byte) NativeDaemonAPIResponse {
	t.Helper()
	response, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{
		APIBaseURL: session.EncryptedAPIBaseURL, CredentialRef: session.CredentialRef, Method: method, Path: path, Body: body,
	})
	if err != nil {
		t.Fatalf("desktop bridge %s %s: %v", method, path, err)
	}
	return response
}

type windowsSmokeReply struct {
	status int
	body   []byte
}

func windowsSmokeEngine(t *testing.T, app *App, session GUIManagedNonServiceDaemonSession, method, path, key string) windowsSmokeReply {
	t.Helper()
	rt := app.desktopRuntime()
	if key == "" {
		resolved, err := rt.resolveCredentialRef(session.CredentialRef)
		if err != nil {
			t.Fatal(err)
		}
		key = resolved
	}
	client, err := rt.proxyClient(session.CredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, strings.TrimRight(session.EncryptedAPIBaseURL, "/")+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-FSE-API-Key", key)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return windowsSmokeReply{status: resp.StatusCode, body: body}
}
