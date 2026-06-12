package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type App struct {
	ctx     context.Context
	desktop *desktopNativeRuntime
}

type DesktopRuntimeInfo struct {
	AppName               string `json:"appName"`
	SeparateProcessDaemon bool   `json:"separateProcessDaemon"`
	ControlPlane          string `json:"controlPlane"`
}

type GUIManagedNonServiceDaemonSession struct {
	SessionID             string `json:"sessionID"`
	PID                   int    `json:"pid"`
	EncryptedAPIBaseURL   string `json:"encryptedApiBaseURL"`
	CredentialRef         string `json:"credentialRef"`
	ConfigPath            string `json:"configPath"`
	StatePath             string `json:"statePath"`
	SessionMode           string `json:"sessionMode"`
	LaunchedAt            string `json:"launchedAt"`
	ReconnectOnNextLaunch bool   `json:"reconnectOnNextLaunch"`
	Message               string `json:"message"`
}

type GUIOwnedNonServiceDaemonLaunchRequest struct {
	SessionMode                   string `json:"sessionMode"`
	PreferExistingReachableDaemon bool   `json:"preferExistingReachableDaemon"`
}

type desktopNativeRuntime struct {
	resourceRoot       string
	stateRoot          string
	launcher           func(command string, args []string, env []string) (int, error)
	stopClient         *http.Client
	credentialResolver func(ref string) (string, error)
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) RuntimeInfo() DesktopRuntimeInfo {
	return DesktopRuntimeInfo{
		AppName:               "File Synchronization Engine Desktop",
		SeparateProcessDaemon: true,
		ControlPlane:          "authenticated-encrypted-api",
	}
}

func (a *App) RequestGUIOwnedNonServiceDaemonLaunch(request GUIOwnedNonServiceDaemonLaunchRequest) (GUIManagedNonServiceDaemonSession, error) {
	rt := a.desktopRuntime()
	mode := request.SessionMode
	if mode == "" {
		mode = "persistent-user-daemon"
	}
	if mode != "persistent-user-daemon" && mode != "temporary-session-only" {
		return GUIManagedNonServiceDaemonSession{}, fmt.Errorf("unsupported GUI-owned daemon session mode: %s", mode)
	}
	if request.PreferExistingReachableDaemon {
		if existing, err := rt.loadSession(); err == nil && existing.SessionID != "" && existing.PID > 0 {
			existing.Message = "reconnected to existing GUI-owned non-service daemon session"
			return existing, nil
		}
	}
	enginePath, err := rt.engineExecutablePath()
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	if err := requireExecutableFile(enginePath); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	stateRoot, err := rt.ensureStateRoot()
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	sessionID := "gui-" + randomHex(16)
	sessionDir := filepath.Join(stateRoot, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	apiKey := randomHex(32)
	listen := "127.0.0.1:" + freeTCPPort()
	configPath := filepath.Join(sessionDir, "config.jsonc")
	if err := writeGUIOwnedDaemonConfig(configPath, listen, apiKey, sessionDir); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	args := []string{"start", configPath}
	pid, err := rt.launch(enginePath, args, []string{"FSE_DESKTOP_GUI_OWNED_DAEMON=1", "FSE_DESKTOP_SESSION_ID=" + sessionID})
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	session := GUIManagedNonServiceDaemonSession{
		SessionID:             sessionID,
		PID:                   pid,
		EncryptedAPIBaseURL:   "https://" + listen,
		CredentialRef:         "native://fse-desktop/gui-owned/" + sessionID + "/api-key",
		ConfigPath:            configPath,
		StatePath:             sessionDir,
		SessionMode:           mode,
		LaunchedAt:            time.Now().UTC().Format(time.RFC3339),
		ReconnectOnNextLaunch: mode == "persistent-user-daemon",
		Message:               "GUI-owned non-service daemon launched as a separate process and will be controlled through the encrypted daemon API.",
	}
	if err := rt.saveSession(session); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "api-key"), []byte(apiKey), 0o600); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	return session, nil
}

func (a *App) AdoptGUIOwnedNonServiceDaemon(sessionID string) (GUIManagedNonServiceDaemonSession, error) {
	if strings.TrimSpace(sessionID) == "" {
		return GUIManagedNonServiceDaemonSession{}, errors.New("session id is required")
	}
	session, err := a.desktopRuntime().loadSession()
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	if session.SessionID != sessionID {
		return GUIManagedNonServiceDaemonSession{}, fmt.Errorf("GUI-owned daemon session %s is not the active persisted session", sessionID)
	}
	session.Message = "GUI-owned non-service daemon session adopted for encrypted API control."
	return session, nil
}

func (a *App) GetGUIOwnedNonServiceDaemonSession() (*GUIManagedNonServiceDaemonSession, error) {
	session, err := a.desktopRuntime().loadSession()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (a *App) StopGUIOwnedNonServiceDaemonThroughAPI(sessionID string) (GUIManagedNonServiceDaemonSession, error) {
	rt := a.desktopRuntime()
	session, err := rt.loadSession()
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	if session.SessionID != sessionID {
		return GUIManagedNonServiceDaemonSession{}, fmt.Errorf("GUI-owned daemon session %s is not the active persisted session", sessionID)
	}
	apiKey, err := rt.resolveAPIKey(session)
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(session.EncryptedAPIBaseURL, "/")+"/v1/stop", bytes.NewReader([]byte("{}")))
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := rt.stopClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GUIManagedNonServiceDaemonSession{}, fmt.Errorf("daemon stop request failed: HTTP %d", resp.StatusCode)
	}
	session.PID = 0
	session.Message = "GUI-owned non-service daemon stop requested through the encrypted daemon API."
	if session.SessionMode == "temporary-session-only" || !session.ReconnectOnNextLaunch {
		if err := rt.clearSession(); err != nil {
			return GUIManagedNonServiceDaemonSession{}, err
		}
		return session, nil
	}
	if err := rt.saveSession(session); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	return session, nil
}

func (a *App) desktopRuntime() *desktopNativeRuntime {
	if a.desktop == nil {
		a.desktop = &desktopNativeRuntime{}
	}
	return a.desktop
}

func (rt *desktopNativeRuntime) launch(command string, args []string, env []string) (int, error) {
	if rt.launcher != nil {
		return rt.launcher(command, args, env)
	}
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

func (rt *desktopNativeRuntime) engineExecutablePath() (string, error) {
	root := rt.resourceRoot
	if root == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		root = filepath.Join(filepath.Dir(exe), "resources", "engine")
	}
	return filepath.Join(root, runtimeTargetOS(), runtimeTargetArch(), runtimeExecutableName()), nil
}

func (rt *desktopNativeRuntime) ensureStateRoot() (string, error) {
	root := rt.stateRoot
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(base, "fse-desktop")
		rt.stateRoot = root
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func (rt *desktopNativeRuntime) sessionPath() (string, error) {
	root, err := rt.ensureStateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "gui-owned-daemon-session.json"), nil
}

func (rt *desktopNativeRuntime) saveSession(session GUIManagedNonServiceDaemonSession) error {
	path, err := rt.sessionPath()
	if err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, bytes, 0o600)
}

func (rt *desktopNativeRuntime) loadSession() (GUIManagedNonServiceDaemonSession, error) {
	path, err := rt.sessionPath()
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	var session GUIManagedNonServiceDaemonSession
	if err := json.Unmarshal(bytes, &session); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
	return session, nil
}

func (rt *desktopNativeRuntime) clearSession() error {
	path, err := rt.sessionPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (rt *desktopNativeRuntime) resolveAPIKey(session GUIManagedNonServiceDaemonSession) (string, error) {
	if rt.credentialResolver != nil {
		return rt.credentialResolver(session.CredentialRef)
	}
	bytes, err := os.ReadFile(filepath.Join(session.StatePath, "api-key"))
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(bytes))
	if key == "" {
		return "", errors.New("empty GUI-owned daemon API key")
	}
	return key, nil
}

func runtimeTargetOS() string {
	if runtime.GOOS == "darwin" {
		return "darwin"
	}
	return runtime.GOOS
}

func runtimeTargetArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

func runtimeExecutableName() string {
	if runtime.GOOS == "windows" {
		return "fse.exe"
	}
	return "fse"
}

func requireExecutableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("bundled daemon executable unavailable: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("bundled daemon executable path is a directory: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("bundled daemon executable is not executable: %s", path)
	}
	return nil
}

func writeGUIOwnedDaemonConfig(path, listen, apiKey, stateDir string) error {
	metadataPath := filepath.ToSlash(filepath.Join(stateDir, "metadata"))
	logPath := filepath.ToSlash(filepath.Join(stateDir, "logs", "daemon.jsonl"))
	certPath := filepath.ToSlash(filepath.Join(stateDir, "api.crt"))
	keyPath := filepath.ToSlash(filepath.Join(stateDir, "api.key"))
	config := fmt.Sprintf(`{
  // Generated by the desktop GUI for a separate GUI-owned non-service daemon session.
  "nodeName": "fse-desktop-gui-owned-daemon",
  "api": {
    "listen": "%s",
    "apiKey": "%s",
    "encryption": {
      "mode": "manual-tls",
      "certificatePath": "%s",
      "privateKeyPath": "%s"
    }
  },
  "metadata": {
    "backend": "badger",
    "path": "%s",
    "perFolder": true
  },
  "logging": {
    "level": "info",
    "output": "%s"
  },
  "folders": [],
  "peers": []
}
`, listen, apiKey, certPath, keyPath, metadataPath, logPath)
	return atomicWriteFile(path, []byte(config), 0o600)
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func freeTCPPort() string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "17444"
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return "17444"
	}
	return port
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(sum[:bytesLen])
}
