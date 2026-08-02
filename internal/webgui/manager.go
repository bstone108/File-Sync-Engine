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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const versionMarkerName = ".fse-web-version"

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
		mux.HandleFunc("/api/v1/status", nativeReadOnlyBridge(nativeAPI, nativeAPIKey, "/v1/status"))
		mux.HandleFunc("/api/v1/folders", nativeReadOnlyBridge(nativeAPI, nativeAPIKey, "/v1/folders"))
	}
	mux.Handle("/", http.FileServer(http.Dir(installDir)))
	return mux
}

func nativeReadOnlyBridge(nativeAPI http.Handler, nativeAPIKey, nativePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		nativeRequest, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://fse-native"+nativePath, nil)
		if err != nil {
			http.Error(w, "web read-only bridge request failed", http.StatusInternalServerError)
			return
		}
		nativeRequest.Header.Set("X-FSE-API-Key", nativeAPIKey)
		nativeAPI.ServeHTTP(w, nativeRequest)
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
	return nil
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
