package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type App struct {
	ctx                    context.Context
	desktop                *desktopNativeRuntime
	remoteInstanceRegistry sync.Mutex
}

type DesktopRuntimeInfo struct {
	AppName               string `json:"appName"`
	SeparateProcessDaemon bool   `json:"separateProcessDaemon"`
	ControlPlane          string `json:"controlPlane"`
}

type GUIManagedNonServiceDaemonSession struct {
	SessionID             string `json:"sessionID"`
	PID                   int    `json:"pid"`
	Kind                  string `json:"kind,omitempty"`
	Manager               string `json:"manager,omitempty"`
	ServiceName           string `json:"serviceName,omitempty"`
	EncryptedAPIBaseURL   string `json:"encryptedApiBaseURL"`
	CredentialRef         string `json:"credentialRef"`
	ConfigPath            string `json:"configPath"`
	StatePath             string `json:"statePath"`
	SessionMode           string `json:"sessionMode"`
	LaunchedAt            string `json:"launchedAt"`
	ReconnectOnNextLaunch bool   `json:"reconnectOnNextLaunch"`
	Message               string `json:"message"`
	ConnectionState       string `json:"connectionState"`
	NodeName              string `json:"nodeName,omitempty"`
}

type DaemonRuntimeState struct {
	ConnectionState string `json:"connectionState"`
	NodeName        string `json:"nodeName,omitempty"`
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionID,omitempty"`
	Source          string `json:"source,omitempty"`
	Kind            string `json:"kind,omitempty"`
	Manager         string `json:"manager,omitempty"`
	ServiceName     string `json:"serviceName,omitempty"`
	APIBaseURL      string `json:"apiBaseURL,omitempty"`
	CredentialRef   string `json:"credentialRef,omitempty"`
	Message         string `json:"message"`
}

type GUIOwnedNonServiceDaemonLaunchRequest struct {
	SessionMode                   string `json:"sessionMode"`
	PreferExistingReachableDaemon bool   `json:"preferExistingReachableDaemon"`
}

type desktopNativeRuntime struct {
	resourceRoot            string
	stateRoot               string
	platform                string
	launcher                func(command string, args []string, env []string) (int, error)
	terminateProcess        func(pid int) error
	commandRunner           func(name string, args ...string) ([]byte, error)
	stopClient              *http.Client
	apiClient               *http.Client
	credentialResolver      func(ref string) (string, error)
	credentialVaultSet      func(service, account, secret string) error
	credentialVaultGet      func(service, account string) (string, error)
	credentialVaultDelete   func(service, account string) error
	remoteRegistryWrite     func(path string, data []byte) error
	statusClient            *http.Client
	probeSession            func(GUIManagedNonServiceDaemonSession) (DaemonRuntimeState, error)
	probeCandidate          func(localDaemonCandidate) (DaemonRuntimeState, error)
	serviceCandidates       []localDaemonCandidate
	readinessAttempts       int
	serviceStopPollAttempts int
	serviceStopPollInterval time.Duration
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Best-effort startup reconciliation. Failures remain durable and can be
	// retried through ReconcileRemoteInstanceCredentialCleanup.
	_, _ = a.ReconcileRemoteInstanceCredentialCleanup()
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
		if discovered, err := a.DiscoverLocalDaemon(); err == nil && discovered.ConnectionState == "running" && discovered.Kind == "service" {
			return GUIManagedNonServiceDaemonSession{
				SessionID: discovered.Source, Kind: "service", Manager: discovered.Manager, ServiceName: discovered.ServiceName,
				EncryptedAPIBaseURL: discovered.APIBaseURL, CredentialRef: discovered.CredentialRef,
				SessionMode: "installed-service", ConnectionState: "running", NodeName: discovered.NodeName,
				Message: "Connected to reachable installed service daemon; no second portable daemon was started.",
			}, nil
		}
		candidates := rt.serviceCandidates
		if candidates == nil {
			candidates = rt.defaultServiceCandidates()
		}
		if len(candidates) > 0 {
			candidate := candidates[0]
			started, err := a.ControlLocalDaemon(LocalDaemonControlRequest{Action: "start", Source: candidate.ID})
			if err != nil {
				return GUIManagedNonServiceDaemonSession{}, fmt.Errorf("configured local daemon service %s could not be started; portable fallback was not launched to avoid a second engine: %w", candidate.ID, err)
			}
			return GUIManagedNonServiceDaemonSession{
				SessionID: started.Source, Kind: "service", Manager: started.Manager, ServiceName: started.ServiceName,
				EncryptedAPIBaseURL: started.APIBaseURL, CredentialRef: started.CredentialRef,
				SessionMode: "installed-service", ConnectionState: started.ConnectionState, NodeName: started.NodeName,
				Message: "Configured installed service daemon was started and its encrypted API is reachable; no portable daemon was launched.",
			}, nil
		}
		if existing, err := rt.loadSession(); err == nil && existing.SessionID != "" && existing.PID > 0 {
			if state, probeErr := rt.probe(existing); probeErr == nil {
				existing.ConnectionState = state.ConnectionState
				existing.NodeName = state.NodeName
				existing.Message = "reconnected to reachable GUI-owned non-service daemon session"
				return existing, nil
			}
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
	if err := os.WriteFile(filepath.Join(sessionDir, "api-key"), []byte(apiKey), 0o600); err != nil {
		return GUIManagedNonServiceDaemonSession{}, err
	}
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
	state, err := rt.waitUntilReachable(session)
	if err != nil {
		if cleanupErr := rt.cleanupFailedGUIOwnedDaemonStart(session); cleanupErr != nil {
			return GUIManagedNonServiceDaemonSession{}, fmt.Errorf("bundled daemon process started (PID %d) but its API did not become reachable: %w; automatic cleanup failed: %v; see %s", pid, err, cleanupErr, filepath.Join(sessionDir, "logs", "daemon.jsonl"))
		}
		return GUIManagedNonServiceDaemonSession{}, fmt.Errorf("bundled daemon process started (PID %d) but its API did not become reachable: %w; the failed process was stopped and its session was cleared, so retrying Start local engine is safe; see %s", pid, err, filepath.Join(sessionDir, "logs", "daemon.jsonl"))
	}
	session.ConnectionState = state.ConnectionState
	session.NodeName = state.NodeName
	session.Message = "Bundled daemon is running as a separate process and its encrypted API is reachable."
	if err := rt.saveSession(session); err != nil {
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

func (a *App) GetGUIOwnedNonServiceDaemonState() (DaemonRuntimeState, error) {
	session, err := a.desktopRuntime().loadSession()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DaemonRuntimeState{ConnectionState: "stopped", Message: "No local daemon session is recorded."}, nil
		}
		return DaemonRuntimeState{}, err
	}
	return a.desktopRuntime().probe(session)
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
	req.Header.Set("X-FSE-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := rt.stopClient
	if client == nil {
		client, err = rt.clientForSession(session, 10*time.Second)
		if err != nil {
			return GUIManagedNonServiceDaemonSession{}, err
		}
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
	root, err := rt.engineResourceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, runtimeTargetOS(), runtimeTargetArch(), runtimeExecutableName()), nil
}

func (rt *desktopNativeRuntime) engineResourceRoot() (string, error) {
	if rt.resourceRoot != "" {
		return rt.resourceRoot, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return desktopEngineResourceRootForExecutable(exe), nil
}

func desktopEngineResourceRootForExecutable(executable string) string {
	appDirectory := filepath.Dir(executable)
	packagedRoot := filepath.Clean(filepath.Join(appDirectory, "..", "engine"))
	if info, err := os.Stat(packagedRoot); err == nil && info.IsDir() {
		return packagedRoot
	}
	return filepath.Join(appDirectory, "resources", "engine")
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

func (rt *desktopNativeRuntime) cleanupFailedGUIOwnedDaemonStart(session GUIManagedNonServiceDaemonSession) error {
	if session.PID <= 0 {
		return errors.New("failed daemon start has no process ID")
	}
	if rt.terminateProcess != nil {
		if err := rt.terminateProcess(session.PID); err != nil {
			return fmt.Errorf("stop failed daemon process: %w", err)
		}
	} else {
		process, err := os.FindProcess(session.PID)
		if err != nil {
			return fmt.Errorf("find failed daemon process: %w", err)
		}
		if err := process.Kill(); err != nil {
			return fmt.Errorf("stop failed daemon process: %w", err)
		}
	}
	if err := rt.clearSession(); err != nil {
		return fmt.Errorf("clear failed daemon session: %w", err)
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

func (rt *desktopNativeRuntime) clientForSession(session GUIManagedNonServiceDaemonSession, timeout time.Duration) (*http.Client, error) {
	pool := x509.NewCertPool()
	cert, err := os.ReadFile(filepath.Join(session.StatePath, "api.crt"))
	if err != nil {
		return nil, fmt.Errorf("read daemon API certificate: %w", err)
	}
	if !pool.AppendCertsFromPEM(cert) {
		return nil, errors.New("daemon API certificate is invalid")
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}, nil
}

func (rt *desktopNativeRuntime) waitUntilReachable(session GUIManagedNonServiceDaemonSession) (DaemonRuntimeState, error) {
	attempts := rt.readinessAttempts
	if attempts == 0 {
		attempts = 40
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		state, err := rt.probe(session)
		if err == nil {
			return state, nil
		}
		lastErr = err
		if rt.readinessAttempts == 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return DaemonRuntimeState{}, lastErr
}

func (rt *desktopNativeRuntime) probe(session GUIManagedNonServiceDaemonSession) (DaemonRuntimeState, error) {
	if rt.probeSession != nil {
		return rt.probeSession(session)
	}
	apiKey, err := rt.resolveAPIKey(session)
	if err != nil {
		return DaemonRuntimeState{}, err
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(session.EncryptedAPIBaseURL, "/")+"/v1/status", nil)
	if err != nil {
		return DaemonRuntimeState{}, err
	}
	req.Header.Set("X-FSE-API-Key", apiKey)
	client := rt.statusClient
	if client == nil {
		client, err = rt.clientForSession(session, 2*time.Second)
		if err != nil {
			return DaemonRuntimeState{}, err
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return DaemonRuntimeState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return DaemonRuntimeState{}, fmt.Errorf("daemon status request failed: HTTP %d", resp.StatusCode)
	}
	var body struct {
		NodeName string `json:"nodeName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return DaemonRuntimeState{}, fmt.Errorf("decode daemon status: %w", err)
	}
	return DaemonRuntimeState{ConnectionState: "running", NodeName: body.NodeName, PID: session.PID, SessionID: session.SessionID, Message: "Daemon API is reachable."}, nil
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
      "certFile": "%s",
      "keyFile": "%s"
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
