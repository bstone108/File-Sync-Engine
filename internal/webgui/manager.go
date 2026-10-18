package webgui

import (
	"archive/zip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/advertise"
)

const versionMarkerName = ".fse-web-version"

// checksumVerifiedLog receives the success line after a package checksum matches.
var checksumVerifiedLog io.Writer = os.Stderr

type InstallOptions struct {
	PackagePath    string
	InstallDir     string
	Version        string
	ChecksumSHA256 string
}

type InstallRemoteOptions struct {
	UpdateURL      string
	InstallDir     string
	Version        string
	ChecksumSHA256 string
	HTTPClient     *http.Client
}

type InstallResult struct {
	Status      string    `json:"status"`
	Version     string    `json:"version"`
	InstallDir  string    `json:"installDir"`
	InstalledAt time.Time `json:"installedAt"`
}

type StartOptions struct {
	InstallDir       string
	Listen           string
	HTTPSListen      string
	TLSCertFile      string
	TLSKeyFile       string
	NativeAPIHandler http.Handler
	NativeAPIKey     string
}

type Status struct {
	Status      string `json:"status"`
	Running     bool   `json:"running"`
	Version     string `json:"version,omitempty"`
	InstallDir  string `json:"installDir,omitempty"`
	Listen      string `json:"listen,omitempty"`
	URL         string `json:"url,omitempty"`
	HTTPSListen string `json:"httpsListen,omitempty"`
	HTTPSURL    string `json:"httpsUrl,omitempty"`
}

type Server struct {
	mu           sync.Mutex
	server       *http.Server
	httpsServer  *http.Server
	ln           net.Listener
	httpsLn      net.Listener
	status       Status
	nativeAPI    http.Handler
	nativeAPIKey string
}

func NewServer() *Server { return &Server{} }

// SetNativeAPI configures the daemon-owned status bridge. Its credential remains
// in the daemon process and is never read from, or forwarded by, browser requests.
func (s *Server) SetNativeAPI(handler http.Handler, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nativeAPI = handler
	s.nativeAPIKey = key
}

func (s *Server) Start(opts StartOptions) (Status, error) {
	if opts.InstallDir == "" {
		return Status{}, errors.New("web GUI install dir is required")
	}
	listen := opts.Listen
	if listen == "" {
		listen = "127.0.0.1:0"
	}
	version, err := InstalledVersion(opts.InstallDir)
	if err != nil {
		return Status{}, err
	}
	nativeAPI := opts.NativeAPIHandler
	nativeAPIKey := opts.NativeAPIKey
	if nativeAPI == nil || nativeAPIKey == "" {
		s.mu.Lock()
		if nativeAPI == nil {
			nativeAPI = s.nativeAPI
		}
		if nativeAPIKey == "" {
			nativeAPIKey = s.nativeAPIKey
		}
		s.mu.Unlock()
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return Status{}, err
	}

	var httpsLn net.Listener
	certFile := opts.TLSCertFile
	keyFile := opts.TLSKeyFile
	if opts.HTTPSListen != "" {
		if certFile == "" && keyFile == "" {
			certFile = filepath.Join(opts.InstallDir, ".fse-web-auto.crt")
			keyFile = filepath.Join(opts.InstallDir, ".fse-web-auto.key")
		}
		if certFile == "" || keyFile == "" {
			_ = ln.Close()
			return Status{}, errors.New("web GUI HTTPS requires both cert and key files")
		}
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			if err := generateSelfSignedCertificate(certFile, keyFile, opts.HTTPSListen); err != nil {
				_ = ln.Close()
				return Status{}, err
			}
		} else if err != nil {
			_ = ln.Close()
			return Status{}, err
		}
		httpsLn, err = net.Listen("tcp", opts.HTTPSListen)
		if err != nil {
			_ = ln.Close()
			return Status{}, err
		}
	}

	s.mu.Lock()
	if s.server != nil || s.httpsServer != nil {
		s.mu.Unlock()
		_ = ln.Close()
		if httpsLn != nil {
			_ = httpsLn.Close()
		}
		return Status{}, errors.New("web GUI server is already running")
	}
	httpServer := &http.Server{Handler: webMux(opts.InstallDir, version, nativeAPI, nativeAPIKey)}
	var httpsServer *http.Server
	addr := ln.Addr().String()
	httpsAddr := ""
	httpsURL := ""
	if httpsLn != nil {
		httpsServer = &http.Server{Handler: webMux(opts.InstallDir, version, nativeAPI, nativeAPIKey)}
		httpsAddr = httpsLn.Addr().String()
		httpsURL = "https://" + httpsAddr
	}
	s.server = httpServer
	s.httpsServer = httpsServer
	s.ln = ln
	s.httpsLn = httpsLn
	s.status = Status{Status: "running", Running: true, Version: version, InstallDir: opts.InstallDir, Listen: addr, URL: "http://" + addr, HTTPSListen: httpsAddr, HTTPSURL: httpsURL}
	status := s.status
	s.mu.Unlock()

	go s.serveHTTP(httpServer, ln)
	if httpsServer != nil {
		go s.serveHTTPS(httpsServer, httpsLn, certFile, keyFile)
	}
	return status, nil
}

func webMux(installDir, version string, nativeAPI http.Handler, nativeAPIKey string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": version})
	})
	if nativeAPI != nil && nativeAPIKey != "" {
		for _, route := range dockerProxyRoutes() {
			mux.HandleFunc(route.BrowserPath, nativeMethodBridge(nativeAPI, nativeAPIKey, route.NativePath, route.Methods, route.QueryKeys))
		}
	}
	mux.Handle("/", http.FileServer(http.Dir(installDir)))
	return mux
}

type proxyRoute struct {
	BrowserPath string
	NativePath  string
	Methods     []string
	QueryKeys   []string
}

// dockerProxyRoutes is the browser allowlist. Daemon routes such as /v1/api/trust
// are registered under /api plus that path so the GUI prefix is not dropped.
func dockerProxyRoutes() []proxyRoute {
	get := []string{http.MethodGet}
	post := []string{http.MethodPost}
	return []proxyRoute{
		{BrowserPath: "/api/v1/status", NativePath: "/v1/status", Methods: get},
		{BrowserPath: "/api/v1/folders", NativePath: "/v1/folders", Methods: get},
		{BrowserPath: "/api/v1/peers", NativePath: "/v1/peers", Methods: get},
		{BrowserPath: "/api/v1/transfers", NativePath: "/v1/transfers", Methods: get},
		{BrowserPath: "/api/v1/actionable-errors", NativePath: "/v1/actionable-errors", Methods: get},
		{BrowserPath: "/api/v1/logs", NativePath: "/v1/logs", Methods: get},
		{BrowserPath: "/api/v1/schema", NativePath: "/v1/schema", Methods: get},
		{BrowserPath: "/api/v1/config", NativePath: "/v1/config", Methods: []string{http.MethodGet, http.MethodPatch}},
		{BrowserPath: "/api/v1/encryption-profiles", NativePath: "/v1/encryption-profiles", Methods: get},
		{BrowserPath: "/api/v1/filesystem/browse", NativePath: "/v1/filesystem/browse", Methods: get, QueryKeys: []string{"path"}},
		{BrowserPath: "/api/v1/api/trust", NativePath: "/v1/api/trust", Methods: get},
		{BrowserPath: "/api/v1/api/trust-command", NativePath: "/v1/api/trust-command", Methods: post},
		{BrowserPath: "/api/v1/peer-command", NativePath: "/v1/peer-command", Methods: post},
		{BrowserPath: "/api/v1/folder-command", NativePath: "/v1/folder-command", Methods: post},
		{BrowserPath: "/api/v1/discovery-command", NativePath: "/v1/discovery-command", Methods: post},
		{BrowserPath: "/api/v1/transfer-command", NativePath: "/v1/transfer-command", Methods: post},
		{BrowserPath: "/api/v1/identity-package", NativePath: "/v1/identity-package", Methods: post},
		{BrowserPath: "/api/v1/identity-import", NativePath: "/v1/identity-import", Methods: post},
		{BrowserPath: "/api/v1/pair-code", NativePath: "/v1/pair-code", Methods: post},
		{BrowserPath: "/api/v1/pair-code/redeem", NativePath: "/v1/pair-code/redeem", Methods: post},
		{BrowserPath: "/api/v1/maintenance/scrub", NativePath: "/v1/maintenance/scrub", Methods: post},
		{BrowserPath: "/api/v1/web-gui-command", NativePath: "/v1/web-gui-command", Methods: post},
		{BrowserPath: "/api/v1/mesh/settings", NativePath: "/v1/mesh/settings", Methods: get, QueryKeys: []string{"nodeId"}},
		{BrowserPath: "/api/v1/mesh/settings-command", NativePath: "/v1/mesh/settings-command", Methods: post},
		{BrowserPath: "/api/v1/config-sync", NativePath: "/v1/config-sync", Methods: get},
		{BrowserPath: "/api/v1/config-sync/edit", NativePath: "/v1/config-sync/edit", Methods: post},
		{BrowserPath: "/api/v1/config-sync/merge", NativePath: "/v1/config-sync/merge", Methods: post},
		{BrowserPath: "/api/v1/snapshots", NativePath: "/v1/snapshots", Methods: post},
		{BrowserPath: "/api/v1/restore-plans", NativePath: "/v1/restore-plans", Methods: post},
		{BrowserPath: "/api/v1/restores", NativePath: "/v1/restores", Methods: post},
		{BrowserPath: "/api/v1/snapshot-retention", NativePath: "/v1/snapshot-retention", Methods: post},
		{BrowserPath: "/api/v1/backup/scrub", NativePath: "/v1/backup/scrub", Methods: post},
		{BrowserPath: "/api/v1/backup/jobs", NativePath: "/v1/backup/jobs", Methods: get, QueryKeys: []string{"snapshotId"}},
	}
}

func nativeReadOnlyBridge(nativeAPI http.Handler, nativeAPIKey, nativePath string) http.HandlerFunc {
	return nativeMethodBridge(nativeAPI, nativeAPIKey, nativePath, []string{http.MethodGet}, nil)
}

func nativeMethodBridge(nativeAPI http.Handler, nativeAPIKey, nativePath string, methods []string, queryKeys []string) http.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, method := range methods {
		allowed[method] = struct{}{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.Method]; !ok {
			w.Header().Set("Allow", strings.Join(methods, ", "))
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		target := "http://fse-native" + nativePath
		if encoded := allowlistedQuery(r, queryKeys); encoded != "" {
			target += "?" + encoded
		}
		var body io.Reader
		if r.Method != http.MethodGet && r.Body != nil {
			limited := io.LimitReader(r.Body, 1<<20)
			payload, err := io.ReadAll(limited)
			if err != nil {
				http.Error(w, "web control bridge request failed", http.StatusBadRequest)
				return
			}
			body = strings.NewReader(string(payload))
		}
		nativeRequest, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
		if err != nil {
			http.Error(w, "web control bridge request failed", http.StatusInternalServerError)
			return
		}
		if body != nil {
			nativeRequest.Header.Set("Content-Type", "application/json")
		}
		nativeRequest.Header.Set("X-FSE-API-Key", nativeAPIKey)
		if host := guiHostHeader(r); host != "" {
			nativeRequest.Header.Set(advertise.GUIHostHeader, host)
		}
		nativeRequest.Host = "fse-native"
		recorder := httptest.NewRecorder()
		nativeAPI.ServeHTTP(recorder, nativeRequest)
		if contentType := recorder.Header().Get("Content-Type"); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(scrubProxyBody(recorder.Body.Bytes()))
	}
}

func guiHostHeader(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return strings.TrimSpace(r.Host)
}

func scrubProxyBody(body []byte) []byte {
	if !bytesContainSecretKey(body) {
		return body
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return body
	}
	scrubJSONSecrets(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return body
	}
	return encoded
}

func bytesContainSecretKey(body []byte) bool {
	text := string(body)
	return strings.Contains(text, `"apiKey"`) || strings.Contains(text, `"privateKey"`) || strings.Contains(text, `"identityPrivateKey"`)
}

func scrubJSONSecrets(value any) {
	switch typed := value.(type) {
	case map[string]any:
		delete(typed, "apiKey")
		delete(typed, "privateKey")
		delete(typed, "identityPrivateKey")
		if _, hasListen := typed["listen"]; hasListen {
			delete(typed, "key")
		}
		for _, child := range typed {
			scrubJSONSecrets(child)
		}
	case []any:
		for _, child := range typed {
			scrubJSONSecrets(child)
		}
	}
}

func (s *Server) serveHTTP(server *http.Server, ln net.Listener) {
	if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.markError(server, nil)
	}
}

func (s *Server) serveHTTPS(server *http.Server, ln net.Listener, certFile, keyFile string) {
	if err := server.ServeTLS(ln, certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.markError(nil, server)
	}
}

func (s *Server) markError(httpServer *http.Server, httpsServer *http.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (httpServer != nil && s.server == httpServer) || (httpsServer != nil && s.httpsServer == httpsServer) {
		s.status.Status = "error"
		s.status.Running = false
	}
}

func (s *Server) Stop() (Status, error) {
	s.mu.Lock()
	server := s.server
	httpsServer := s.httpsServer
	if server == nil && httpsServer == nil {
		status := s.status
		if status.Status == "" {
			status.Status = "stopped"
		}
		status.Running = false
		s.mu.Unlock()
		return status, nil
	}
	s.server = nil
	s.httpsServer = nil
	s.ln = nil
	s.httpsLn = nil
	s.status.Status = "stopped"
	s.status.Running = false
	status := s.status
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			return Status{}, err
		}
	}
	if httpsServer != nil {
		if err := httpsServer.Shutdown(ctx); err != nil {
			return Status{}, err
		}
	}
	return status, nil
}

func (s *Server) Status(installDir string) Status {
	s.mu.Lock()
	status := s.status
	s.mu.Unlock()
	if status.Running {
		return status
	}
	version, err := InstalledVersion(installDir)
	if err != nil {
		return Status{Status: "not_installed", InstallDir: installDir}
	}
	return Status{Status: "installed", Version: version, InstallDir: installDir}
}

func InstallLocalPackage(opts InstallOptions) (InstallResult, error) {
	if opts.PackagePath == "" {
		return InstallResult{}, errors.New("web GUI package path is required")
	}
	if opts.InstallDir == "" {
		return InstallResult{}, errors.New("web GUI install dir is required")
	}
	if opts.Version == "" {
		return InstallResult{}, errors.New("web GUI version is required")
	}
	if opts.ChecksumSHA256 == "" {
		return InstallResult{}, errors.New("web GUI package checksum is required")
	}
	if err := verifyPackageChecksum(opts.PackagePath, opts.ChecksumSHA256); err != nil {
		return InstallResult{}, err
	}

	parent := filepath.Dir(opts.InstallDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return InstallResult{}, err
	}
	tmp, err := os.MkdirTemp(parent, ".fse-web-install-*")
	if err != nil {
		return InstallResult{}, err
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmp)
		}
	}()

	if err := extractZip(opts.PackagePath, tmp); err != nil {
		return InstallResult{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, versionMarkerName), []byte(opts.Version+"\n"), 0o644); err != nil {
		return InstallResult{}, err
	}

	backup := opts.InstallDir + ".previous"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(opts.InstallDir); err == nil {
		if err := os.Rename(opts.InstallDir, backup); err != nil {
			return InstallResult{}, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return InstallResult{}, err
	}
	if err := os.Rename(tmp, opts.InstallDir); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, opts.InstallDir)
		}
		return InstallResult{}, err
	}
	cleanupTmp = false
	_ = os.RemoveAll(backup)
	return InstallResult{Status: "installed", Version: opts.Version, InstallDir: opts.InstallDir, InstalledAt: time.Now().UTC()}, nil
}

func InstallRemotePackage(opts InstallRemoteOptions) (InstallResult, error) {
	if opts.UpdateURL == "" {
		return InstallResult{}, errors.New("web GUI update URL is required")
	}
	parsed, err := url.Parse(opts.UpdateURL)
	if err != nil {
		return InstallResult{}, err
	}
	if parsed.Scheme != "https" {
		return InstallResult{}, errors.New("web GUI update URL must use https")
	}
	parent := filepath.Dir(opts.InstallDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return InstallResult{}, err
	}
	tmp, err := os.CreateTemp(parent, ".fse-web-download-*.zip")
	if err != nil {
		return InstallResult{}, err
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Close(); err != nil {
		return InstallResult{}, err
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(opts.UpdateURL)
	if err != nil {
		return InstallResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return InstallResult{}, fmt.Errorf("web GUI update fetch failed with status %s", resp.Status)
	}
	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return InstallResult{}, err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		return InstallResult{}, err
	}
	if err := out.Close(); err != nil {
		return InstallResult{}, err
	}
	result, err := InstallLocalPackage(InstallOptions{PackagePath: tmpPath, InstallDir: opts.InstallDir, Version: opts.Version, ChecksumSHA256: opts.ChecksumSHA256})
	if err != nil {
		return InstallResult{}, err
	}
	cleanupTmp = true
	return result, nil
}

func InstalledVersion(installDir string) (string, error) {
	bytes, err := os.ReadFile(filepath.Join(installDir, versionMarkerName))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytes)), nil
}

func verifyPackageChecksum(path string, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := fmt.Sprintf("%x", h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("web GUI package checksum mismatch")
	}
	fmt.Fprintln(checksumVerifiedLog, "web GUI package checksum verified")
	return nil
}

func allowlistedQuery(r *http.Request, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	query := url.Values{}
	for _, key := range keys {
		if value := r.URL.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}
	return query.Encode()
}

func extractZip(packagePath string, dest string) error {
	r, err := zip.OpenReader(packagePath)
	if err != nil {
		return err
	}
	defer r.Close()
	cleanDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		entryName := f.Name
		if entryName == "" || strings.Contains(entryName, "..") || strings.Contains(entryName, "\\") || strings.Contains(entryName, ":") {
			return fmt.Errorf("unsafe web GUI package path %q", entryName)
		}
		cleanName := filepath.Clean(entryName)
		if cleanName == "." || cleanName == ".." || filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe web GUI package path %q", entryName)
		}
		absTarget, err := filepath.Abs(filepath.Join(cleanDest, cleanName))
		if err != nil {
			return err
		}
		if absTarget != cleanDest && !strings.HasPrefix(absTarget, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe web GUI package path %q", entryName)
		}
		mode := f.FileInfo().Mode()
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(absTarget, mode.Perm()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(absTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
		if err != nil {
			_ = rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = rc.Close()
			return err
		}
		if err := rc.Close(); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

func generateSelfSignedCertificate(certFile, keyFile, listen string) error {
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return err
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return err
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "filesyncengine web GUI auto-generated certificate"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if host := listenHost(listen); host != "" {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = []net.IP{ip}
		} else {
			template.DNSNames = []string{host}
		}
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(keyFile, keyPEM, 0o600)
}

func listenHost(listen string) string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = listen
	}
	return strings.Trim(host, "[]")
}
