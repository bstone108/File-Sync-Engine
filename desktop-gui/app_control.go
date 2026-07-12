package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxNativeProxyBodyBytes = 1 << 20

type localDaemonCandidate struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Manager       string `json:"manager,omitempty"`
	ServiceName   string `json:"serviceName,omitempty"`
	APIBaseURL    string `json:"apiBaseURL"`
	CredentialRef string `json:"credentialRef"`
	StatePath     string `json:"statePath,omitempty"`
	ConfigPath    string `json:"configPath,omitempty"`
}

type LocalDaemonControlRequest struct {
	Action string `json:"action"`
	Source string `json:"source,omitempty"`
}

type NativeDaemonAPIRequest struct {
	APIBaseURL    string          `json:"apiBaseURL"`
	CredentialRef string          `json:"credentialRef"`
	Method        string          `json:"method"`
	Path          string          `json:"path"`
	Body          json.RawMessage `json:"body,omitempty"`
}

type NativeDaemonAPIResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

var nativeProxyPaths = map[string]map[string]bool{
	"/v1/status": {http.MethodGet: true}, "/v1/folders": {http.MethodGet: true}, "/v1/peers": {http.MethodGet: true},
	"/v1/logs": {http.MethodGet: true}, "/v1/events": {http.MethodGet: true}, "/v1/config": {http.MethodGet: true, http.MethodPatch: true},
	"/v1/folder-command": {http.MethodPost: true}, "/v1/peer-command": {http.MethodPost: true},
	"/v1/discovery-command": {http.MethodPost: true}, "/v1/transfer-command": {http.MethodPost: true},
	"/v1/maintenance/scrub": {http.MethodPost: true}, "/v1/web-gui-command": {http.MethodPost: true},
	"/v1/snapshots": {http.MethodPost: true}, "/v1/restore-plans": {http.MethodPost: true}, "/v1/restores": {http.MethodPost: true},
	"/v1/snapshot-retention": {http.MethodPost: true}, "/v1/backup/scrub": {http.MethodPost: true}, "/v1/backup/jobs": {http.MethodPost: true},
	"/v1/identity-package": {http.MethodPost: true}, "/v1/identity-import": {http.MethodPost: true},
	"/v1/api/trust": {http.MethodGet: true}, "/v1/api/trust-command": {http.MethodPost: true},
	"/v1/mesh/settings": {http.MethodGet: true}, "/v1/mesh/settings-command": {http.MethodPost: true},
	"/v1/filesystem/browse": {http.MethodGet: true}, "/v1/stop": {http.MethodPost: true},
}

func (a *App) DiscoverLocalDaemon() (DaemonRuntimeState, error) {
	rt := a.desktopRuntime()
	candidates := rt.serviceCandidates
	if candidates == nil {
		candidates = rt.defaultServiceCandidates()
	}
	var failures []string
	for _, candidate := range candidates {
		state, err := rt.probeLocalCandidate(candidate)
		if err == nil {
			state.Source, state.Kind, state.Manager, state.ServiceName = candidate.ID, candidate.Kind, candidate.Manager, candidate.ServiceName
			state.APIBaseURL, state.CredentialRef = candidate.APIBaseURL, candidate.CredentialRef
			state.Message = fmt.Sprintf("Reachable %s daemon discovered via %s.", candidate.Kind, candidate.ID)
			return state, nil
		}
		failures = append(failures, candidate.ID+": "+err.Error())
	}
	if session, err := rt.loadSession(); err == nil && session.SessionID != "" {
		state, probeErr := rt.probe(session)
		if probeErr == nil {
			state.Source, state.Kind = "portable:"+session.SessionID, "portable"
			state.APIBaseURL, state.CredentialRef = session.EncryptedAPIBaseURL, session.CredentialRef
			state.Message = "Reachable GUI-owned portable daemon discovered."
			return state, nil
		}
		failures = append(failures, "portable:"+session.SessionID+": "+probeErr.Error())
	}
	message := "No reachable installed service or GUI-owned portable daemon was found."
	if len(failures) > 0 {
		message += " Probes: " + strings.Join(failures, "; ")
	}
	return DaemonRuntimeState{ConnectionState: "stopped", Message: message}, nil
}

func (a *App) ControlLocalDaemon(request LocalDaemonControlRequest) (DaemonRuntimeState, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action != "status" && action != "start" && action != "stop" && action != "restart" {
		return DaemonRuntimeState{}, fmt.Errorf("unsupported local daemon action %q", request.Action)
	}
	rt := a.desktopRuntime()
	candidates := rt.serviceCandidates
	if candidates == nil {
		candidates = rt.defaultServiceCandidates()
	}
	var selected *localDaemonCandidate
	for i := range candidates {
		if candidates[i].ID == request.Source || (request.Source == "" && selected == nil) {
			selected = &candidates[i]
			if request.Source != "" {
				break
			}
		}
	}
	if selected == nil {
		return DaemonRuntimeState{}, fmt.Errorf("local daemon service %q was not discovered; start the portable daemon instead", request.Source)
	}
	if action != "status" {
		name, args, err := managerCommand(*selected, action)
		if err != nil {
			return DaemonRuntimeState{}, err
		}
		output, runErr := rt.runCommand(name, args...)
		if runErr != nil {
			return DaemonRuntimeState{}, fmt.Errorf("%s failed: %w: %s", strings.Join(append([]string{name}, args...), " "), runErr, strings.TrimSpace(string(output)))
		}
	}
	if action == "stop" {
		return DaemonRuntimeState{ConnectionState: "stopped", Source: selected.ID, Kind: selected.Kind, Manager: selected.Manager, ServiceName: selected.ServiceName, APIBaseURL: selected.APIBaseURL, CredentialRef: selected.CredentialRef, Message: "Installed service stop completed."}, nil
	}
	state, err := rt.probeLocalCandidate(*selected)
	if err != nil {
		return DaemonRuntimeState{}, fmt.Errorf("%s completed but daemon API is not reachable: %w", action, err)
	}
	state.Source, state.Kind, state.Manager, state.ServiceName = selected.ID, selected.Kind, selected.Manager, selected.ServiceName
	state.APIBaseURL, state.CredentialRef = selected.APIBaseURL, selected.CredentialRef
	return state, nil
}

func (a *App) DaemonAPIRequest(request NativeDaemonAPIRequest) (NativeDaemonAPIResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	parsed, err := url.Parse(request.Path)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path == "" || !nativeProxyPaths[parsed.Path][method] {
		return NativeDaemonAPIResponse{}, errors.New("native daemon API proxy rejected method or path")
	}
	if len(request.Body) > maxNativeProxyBodyBytes {
		return NativeDaemonAPIResponse{}, errors.New("native daemon API proxy body exceeds 1 MiB")
	}
	base, err := url.Parse(strings.TrimRight(request.APIBaseURL, "/"))
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return NativeDaemonAPIResponse{}, errors.New("native daemon API proxy requires an HTTPS endpoint")
	}
	key, err := a.desktopRuntime().resolveCredentialRef(request.CredentialRef)
	if err != nil {
		return NativeDaemonAPIResponse{}, fmt.Errorf("resolve daemon credential: %w", err)
	}
	req, err := http.NewRequest(method, base.String()+request.Path, bytes.NewReader(request.Body))
	if err != nil {
		return NativeDaemonAPIResponse{}, err
	}
	req.Header.Set("X-FSE-API-Key", key)
	req.Header.Set("Accept", "application/json")
	if len(request.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	client, err := a.desktopRuntime().proxyClient(request.CredentialRef)
	if err != nil {
		return NativeDaemonAPIResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return NativeDaemonAPIResponse{}, fmt.Errorf("daemon API %s %s failed: %w", method, parsed.Path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxNativeProxyBodyBytes+1))
	if err != nil {
		return NativeDaemonAPIResponse{}, err
	}
	if len(body) > maxNativeProxyBodyBytes {
		return NativeDaemonAPIResponse{}, errors.New("daemon API response exceeds 1 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NativeDaemonAPIResponse{}, fmt.Errorf("daemon API %s %s failed: HTTP %d: %s", method, parsed.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if len(body) == 0 {
		body = []byte(`{}`)
	}
	if !json.Valid(body) {
		return NativeDaemonAPIResponse{}, errors.New("daemon API returned invalid JSON")
	}
	return NativeDaemonAPIResponse{Status: resp.StatusCode, Body: body}, nil
}

func (rt *desktopNativeRuntime) defaultServiceCandidates() []localDaemonCandidate {
	platform := rt.platform
	if platform == "" {
		platform = runtime.GOOS
	}
	type spec struct{ id, manager, name, path string }
	var specs []spec
	if configured := os.Getenv("FSE_DESKTOP_SERVICE_CONFIG"); configured != "" {
		specs = append(specs, spec{platform + ":configured", defaultManager(platform, true), "fse", configured})
	}
	home, _ := os.UserHomeDir()
	switch platform {
	case "linux":
		specs = append(specs, spec{"systemd-user:fse", "systemd-user", "fse", filepath.Join(home, ".config", "fse", "config.jsonc")}, spec{"systemd:fse", "systemd", "fse", "/etc/fse/config.jsonc"}, spec{"systemd:file-sync-engine", "systemd", "file-sync-engine", "/etc/file-sync-engine/config.jsonc"})
	case "darwin":
		specs = append(specs, spec{"launchd-user:com.filesyncengine.daemon", "launchd-user", "com.filesyncengine.daemon", filepath.Join(home, "Library", "Application Support", "FileSyncEngine", "config.jsonc")}, spec{"launchd:com.filesyncengine.daemon", "launchd", "com.filesyncengine.daemon", "/Library/Application Support/FileSyncEngine/config.jsonc"})
	case "windows":
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		specs = append(specs, spec{"scm:FileSyncEngine", "scm", "FileSyncEngine", filepath.Join(programData, "FileSyncEngine", "config.jsonc")})
	}
	var candidates []localDaemonCandidate
	for _, item := range specs {
		if candidate, err := loadConfiguredServiceCandidate(item.id, item.manager, item.name, item.path); err == nil {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func loadConfiguredServiceCandidate(id, manager, serviceName, configPath string) (localDaemonCandidate, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return localDaemonCandidate{}, err
	}
	clean, err := stripJSONComments(data)
	if err != nil {
		return localDaemonCandidate{}, err
	}
	var cfg struct {
		API struct {
			Listen, APIKey string
			Encryption     struct{ Mode, CertFile string }
		}
	}
	if err := json.Unmarshal(clean, &cfg); err != nil {
		return localDaemonCandidate{}, fmt.Errorf("parse service config: %w", err)
	}
	if strings.TrimSpace(cfg.API.Listen) == "" || strings.TrimSpace(cfg.API.APIKey) == "" {
		return localDaemonCandidate{}, errors.New("service config has no API listener or key")
	}
	scheme := "https"
	if cfg.API.Encryption.Mode == "disabled" {
		scheme = "http"
	}
	if scheme != "https" {
		return localDaemonCandidate{}, errors.New("desktop GUI requires encrypted daemon API")
	}
	absolute, _ := filepath.Abs(configPath)
	return localDaemonCandidate{ID: id, Kind: "service", Manager: manager, ServiceName: serviceName, APIBaseURL: scheme + "://" + cfg.API.Listen, CredentialRef: "config://" + base64.RawURLEncoding.EncodeToString([]byte(absolute)), StatePath: filepath.Dir(absolute), ConfigPath: absolute}, nil
}

func (rt *desktopNativeRuntime) probeLocalCandidate(candidate localDaemonCandidate) (DaemonRuntimeState, error) {
	if rt.probeCandidate != nil {
		return rt.probeCandidate(candidate)
	}
	response, err := (&App{desktop: rt}).DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: candidate.APIBaseURL, CredentialRef: candidate.CredentialRef, Method: http.MethodGet, Path: "/v1/status"})
	if err != nil {
		return DaemonRuntimeState{}, err
	}
	var body struct {
		NodeName string `json:"nodeName"`
	}
	if err := json.Unmarshal(response.Body, &body); err != nil {
		return DaemonRuntimeState{}, err
	}
	return DaemonRuntimeState{ConnectionState: "running", NodeName: body.NodeName, Message: "Daemon API is reachable."}, nil
}

func (rt *desktopNativeRuntime) resolveCredentialRef(ref string) (string, error) {
	if rt.credentialResolver != nil {
		return rt.credentialResolver(ref)
	}
	if strings.HasPrefix(ref, "config://") {
		encoded := strings.TrimPrefix(ref, "config://")
		pathBytes, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return "", errors.New("invalid config credential reference")
		}
		data, err := os.ReadFile(string(pathBytes))
		if err != nil {
			return "", err
		}
		clean, err := stripJSONComments(data)
		if err != nil {
			return "", err
		}
		var cfg struct{ API struct{ APIKey string } }
		if err := json.Unmarshal(clean, &cfg); err != nil {
			return "", err
		}
		if cfg.API.APIKey == "" {
			return "", errors.New("service config API key is empty")
		}
		return cfg.API.APIKey, nil
	}
	if strings.HasPrefix(ref, "native://fse-desktop/gui-owned/") {
		session, err := rt.loadSession()
		if err != nil {
			return "", err
		}
		return rt.resolveAPIKey(session)
	}
	return "", errors.New("unsupported native credential reference")
}

func (rt *desktopNativeRuntime) proxyClient(ref string) (*http.Client, error) {
	if rt.apiClient != nil {
		return rt.apiClient, nil
	}
	var certPath string
	if strings.HasPrefix(ref, "config://") {
		pathBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ref, "config://"))
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(string(pathBytes))
		if err != nil {
			return nil, err
		}
		clean, err := stripJSONComments(data)
		if err != nil {
			return nil, err
		}
		var cfg struct {
			API struct{ Encryption struct{ CertFile string } }
		}
		if err := json.Unmarshal(clean, &cfg); err != nil {
			return nil, err
		}
		certPath = cfg.API.Encryption.CertFile
	} else if strings.HasPrefix(ref, "native://fse-desktop/gui-owned/") {
		session, err := rt.loadSession()
		if err != nil {
			return nil, err
		}
		certPath = filepath.Join(session.StatePath, "api.crt")
	}
	if certPath == "" {
		return nil, errors.New("daemon API certificate path is unavailable")
	}
	pem, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read daemon API certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("daemon API certificate is invalid")
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}, nil
}

func managerCommand(candidate localDaemonCandidate, action string) (string, []string, error) {
	switch candidate.Manager {
	case "systemd-user":
		return "systemctl", []string{"--user", action, candidate.ServiceName}, nil
	case "systemd":
		return "systemctl", []string{action, candidate.ServiceName}, nil
	case "launchd-user":
		if action == "start" {
			return "launchctl", []string{"kickstart", "gui/" + fmt.Sprint(os.Getuid()) + "/" + candidate.ServiceName}, nil
		}
		if action == "stop" {
			return "launchctl", []string{"kill", "SIGTERM", "gui/" + fmt.Sprint(os.Getuid()) + "/" + candidate.ServiceName}, nil
		}
		return "launchctl", []string{"kickstart", "-k", "gui/" + fmt.Sprint(os.Getuid()) + "/" + candidate.ServiceName}, nil
	case "launchd":
		if action == "stop" {
			return "launchctl", []string{"kill", "SIGTERM", "system/" + candidate.ServiceName}, nil
		}
		args := []string{"kickstart"}
		if action == "restart" {
			args = append(args, "-k")
		}
		return "launchctl", append(args, "system/"+candidate.ServiceName), nil
	case "scm":
		return "sc.exe", []string{action, candidate.ServiceName}, nil
	default:
		return "", nil, fmt.Errorf("unsupported service manager %q", candidate.Manager)
	}
}

func (rt *desktopNativeRuntime) runCommand(name string, args ...string) ([]byte, error) {
	if rt.commandRunner != nil {
		return rt.commandRunner(name, args...)
	}
	return exec.Command(name, args...).CombinedOutput()
}
func defaultManager(platform string, user bool) string {
	if platform == "linux" && user {
		return "systemd-user"
	}
	if platform == "darwin" && user {
		return "launchd-user"
	}
	if platform == "windows" {
		return "scm"
	}
	return platform
}

func stripJSONComments(input []byte) ([]byte, error) {
	var out bytes.Buffer
	inString, escaped := false, false
	for i := 0; i < len(input); i++ {
		c := input[i]
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(input) && input[i+1] == '/' {
			i += 2
			for i < len(input) && input[i] != '\n' {
				i++
			}
			if i < len(input) {
				out.WriteByte('\n')
			}
			continue
		}
		if c == '/' && i+1 < len(input) && input[i+1] == '*' {
			i += 2
			for i+1 < len(input) && !(input[i] == '*' && input[i+1] == '/') {
				i++
			}
			if i+1 >= len(input) {
				return nil, errors.New("unterminated JSON block comment")
			}
			i++
			continue
		}
		out.WriteByte(c)
	}
	if inString {
		return nil, errors.New("unterminated JSON string")
	}
	return out.Bytes(), nil
}
