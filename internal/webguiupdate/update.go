// Package webguiupdate checks the published GUI catalog while the web GUI is
// enabled and swaps in a newer compatible package. It does not download when
// the GUI environment is unset or auto-update is turned off.
package webguiupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/guischema"
	"filesyncengine/internal/schemaversion"
	"filesyncengine/internal/webgui"
)

const (
	EnvEnabled  = "FSE_WEB_GUI_ENABLED"
	EnvAuto     = "FSE_WEB_GUI_AUTO_UPDATE"
	EnvInterval = "FSE_WEB_GUI_AUTO_UPDATE_INTERVAL"
	EnvCatalog  = "FSE_WEB_GUI_UPDATE_CATALOG"
	EnvIndex    = "FSE_WEB_GUI_SCHEMA_INDEX"
	EnvPublic   = "FSE_WEB_GUI_UPDATE_PUBLIC_KEY"

	DefaultInterval   = 6 * time.Hour
	minimumInterval   = 15 * time.Minute
	DefaultCatalogURL = "https://github.com/bstone108/File-Sync-Engine/releases/latest/download/schema-versions.json"
)

// Result is one auto-update attempt. Outcome is a stable token for the GUI.
type Result struct {
	CheckedAt time.Time
	Version   string
	Outcome   string
	Message   string
	Enabled   bool
	Auto      bool
}

// Options controls one check. Zero CatalogSources uses the release catalog and
// the local schema index. A nil Probe uses the installed index.html check.
type Options struct {
	GUIEnabledEnv  string
	AutoUpdateEnv  string
	CatalogSources []string
	PublicKey      string
	ConfigPath     string
	InstallDir     string
	Listen         string
	PackageDir     string
	CurrentVersion string
	CurrentSum     string
	EngineSchema   int
	HTTPClient     *http.Client
	Probe          func(installDir string) error
	Now            time.Time
}

// AutoUpdateEnabled reports whether a download is allowed. The GUI environment
// must be the string true. Auto-update defaults on and turns off only for an
// explicit false or any other non-empty opt-out value.
func AutoUpdateEnabled(guiEnv, autoEnv string) bool {
	if !strings.EqualFold(strings.TrimSpace(guiEnv), "true") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(autoEnv)) {
	case "", "true":
		return true
	default:
		return false
	}
}

// ParseInterval reads a Go duration. Empty, invalid, and sub-15-minute values
// use the six-hour default so a short typo cannot hammer the release host.
func ParseInterval(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultInterval
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < minimumInterval {
		return DefaultInterval
	}
	return parsed
}

// ExplicitPackagePinned reports a complete operator-supplied package. That
// path is installed as given and is not replaced from the catalog.
func ExplicitPackagePinned(version, pkg, installDir, listen, checksum string) bool {
	return version != "" && pkg != "" && installDir != "" && listen != "" && checksum != ""
}

// Check looks for a newer compatible package and installs it. It returns
// before any catalog or package request when downloads are not allowed.
func Check(ctx context.Context, opts Options) Result {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := Result{CheckedAt: now, Version: opts.CurrentVersion, Enabled: strings.EqualFold(strings.TrimSpace(opts.GUIEnabledEnv), "true")}
	if !result.Enabled {
		result.Outcome = "disabled"
		result.Message = "Web GUI auto-update did not download because FSE_WEB_GUI_ENABLED is not true."
		return result
	}
	result.Auto = AutoUpdateEnabled(opts.GUIEnabledEnv, opts.AutoUpdateEnv)
	if !result.Auto {
		result.Outcome = "opted-out"
		result.Message = "Web GUI auto-update is off. The installed package is unchanged."
		return result
	}
	if opts.EngineSchema == 0 {
		opts.EngineSchema = schemaversion.Engine
	}
	sources := opts.CatalogSources
	if len(sources) == 0 {
		sources = defaultSources()
	}
	selected, err := selectAcross(ctx, sources, opts.EngineSchema, opts.HTTPClient)
	if err != nil {
		if errors.Is(err, guischema.ErrNoneCompatible) {
			result.Outcome = "incompatible"
			result.Message = fmt.Sprintf("No published GUI package is compatible with engine schema %d. The installed package is unchanged.", opts.EngineSchema)
			return result
		}
		result.Outcome = "failed"
		result.Message = "Web GUI update check failed. The installed package is unchanged."
		return result
	}
	if !newer(selected, opts.CurrentVersion, opts.CurrentSum) {
		result.Outcome = "current"
		result.Version = opts.CurrentVersion
		if result.Version == "" {
			result.Version = selected.GUIVersion
		}
		result.Message = fmt.Sprintf("Web GUI %s is already the newest package compatible with this engine.", result.Version)
		return result
	}
	installDir := opts.InstallDir
	if installDir == "" {
		installDir = "/config/web-gui"
	}
	packageDir := opts.PackageDir
	if packageDir == "" && opts.ConfigPath != "" {
		packageDir = filepath.Join(filepath.Dir(opts.ConfigPath), "web-gui-packages")
	}
	if packageDir == "" {
		result.Outcome = "failed"
		result.Message = "Web GUI update has no directory for the downloaded package."
		return result
	}
	dest := filepath.Join(packageDir, sanitize(selected.GUIVersion)+".zip")
	if err := guischema.DownloadCompatible(ctx, selected, dest, opts.EngineSchema, opts.PublicKey, opts.HTTPClient); err != nil {
		result.Outcome = "failed"
		result.Message = "Web GUI package was not installed. The previous package is still in place."
		return result
	}
	if _, err := webgui.InstallLocalPackage(webgui.InstallOptions{
		PackagePath: dest, InstallDir: installDir, Version: selected.GUIVersion, ChecksumSHA256: selected.ChecksumSHA256, KeepPrevious: true,
	}); err != nil {
		result.Outcome = "failed"
		result.Message = "Web GUI package failed to install. The previous package is still in place."
		return result
	}
	probe := opts.Probe
	if probe == nil {
		probe = func(dir string) error {
			if err := webgui.ProbeInstall(dir); err != nil {
				return err
			}
			return probeListen(ctx, opts.Listen, opts.HTTPClient)
		}
	}
	if err := probe(installDir); err != nil {
		if rbErr := webgui.RollbackPrevious(installDir); rbErr != nil {
			_ = os.RemoveAll(installDir)
			result.Version = ""
			result.Outcome = "failed"
			result.Message = "The new web GUI did not serve and there was no previous package to restore."
			return result
		}
		result.Outcome = "rolled-back"
		result.Message = "The new web GUI did not serve. The previous package was restored."
		return result
	}
	if err := persist(opts.ConfigPath, selected, dest, installDir, opts.Listen); err != nil {
		result.Version = selected.GUIVersion
		result.Outcome = "updated"
		result.Message = fmt.Sprintf("Web GUI %s is installed, and the config file was not updated.", selected.GUIVersion)
		return result
	}
	result.Version = selected.GUIVersion
	result.Outcome = "updated"
	result.Message = fmt.Sprintf("Web GUI updated to %s.", selected.GUIVersion)
	return result
}

func defaultSources() []string {
	sources := []string{}
	if catalog := strings.TrimSpace(os.Getenv(EnvCatalog)); catalog != "" {
		sources = append(sources, catalog)
	} else {
		sources = append(sources, DefaultCatalogURL)
	}
	if index := strings.TrimSpace(os.Getenv(EnvIndex)); index != "" {
		sources = append(sources, index)
	}
	return sources
}

func selectAcross(ctx context.Context, sources []string, engineSchema int, client *http.Client) (guischema.Package, error) {
	var (
		packages []guischema.Package
		lastErr  error
	)
	for _, source := range sources {
		catalog, err := guischema.LoadCatalog(ctx, source, client)
		if err != nil {
			lastErr = err
			continue
		}
		packages = append(packages, catalog.Packages...)
	}
	if len(packages) == 0 {
		if lastErr != nil {
			return guischema.Package{}, lastErr
		}
		return guischema.Package{}, guischema.ErrNoneCompatible
	}
	return guischema.SelectNewestCompatible(packages, engineSchema)
}

func newer(pkg guischema.Package, currentVersion, currentSum string) bool {
	if currentVersion == "" {
		return true
	}
	switch guischema.CompareGUIVersion(pkg.GUIVersion, currentVersion) {
	case 1:
		return true
	case 0:
		return currentSum != "" && !strings.EqualFold(pkg.ChecksumSHA256, currentSum)
	default:
		return false
	}
}

func probeListen(ctx context.Context, listen string, client *http.Client) error {
	endpoint := probeURL(listen)
	if endpoint == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("web GUI index returned %s", resp.Status)
	}
	return nil
}

func probeURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/index.html"
}

func persist(configPath string, pkg guischema.Package, packagePath, installDir, listen string) error {
	if configPath == "" {
		return nil
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}
	cfg.WebGUI.Enabled = true
	cfg.WebGUI.Version = pkg.GUIVersion
	cfg.WebGUI.PackagePath = packagePath
	cfg.WebGUI.InstallDir = installDir
	if listen != "" {
		cfg.WebGUI.Listen = listen
	} else if cfg.WebGUI.Listen == "" {
		cfg.WebGUI.Listen = "0.0.0.0:8385"
	}
	cfg.WebGUI.ChecksumSHA256 = pkg.ChecksumSHA256
	cfg.WebGUI.UpdateURL = ""
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(configPath, append(data, '\n'), 0o600)
}

func sanitize(version string) string {
	var b strings.Builder
	for _, r := range version {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "gui"
	}
	return b.String()
}
