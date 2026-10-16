package containerbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/guischema"
	"filesyncengine/internal/schemaversion"
)

type RuntimeDefaults struct {
	APIListen         string
	SyncListen        string
	LogLevel          string
	LogOutput         string
	DiscoveryLocal    bool
	DiscoveryDHT      bool
	WebGUIEnabled     bool
	WebGUIVersion     string
	WebGUIPackage     string
	WebGUIInstallDir  string
	WebGUIListen      string
	WebGUITLSEnabled  bool
	WebGUIHTTPSListen string
	WebGUIChecksum    string
}

func DefaultsFromEnvironment() RuntimeDefaults {
	return RuntimeDefaults{
		APIListen:         envOrDefault("FSE_API_LISTEN", "0.0.0.0:22420"),
		SyncListen:        envOrDefault("FSE_SYNC_LISTEN", "tcp://0.0.0.0:22000"),
		LogLevel:          envOrDefault("FSE_LOG_LEVEL", "info"),
		LogOutput:         envOrDefault("FSE_LOG_OUTPUT", "/config/logs/fse.jsonl"),
		DiscoveryLocal:    envBoolOrDefault("FSE_DISCOVERY_LOCAL", true),
		DiscoveryDHT:      envBoolOrDefault("FSE_DISCOVERY_DHT", true),
		WebGUIEnabled:     envBoolOrDefault("FSE_WEB_GUI_ENABLED", false),
		WebGUIVersion:     os.Getenv("FSE_WEB_GUI_VERSION"),
		WebGUIPackage:     os.Getenv("FSE_WEB_GUI_PACKAGE"),
		WebGUIInstallDir:  os.Getenv("FSE_WEB_GUI_INSTALL_DIR"),
		WebGUIListen:      os.Getenv("FSE_WEB_GUI_LISTEN"),
		WebGUITLSEnabled:  envBoolOrDefault("FSE_WEB_GUI_TLS_ENABLED", false),
		WebGUIHTTPSListen: os.Getenv("FSE_WEB_GUI_HTTPS_LISTEN"),
		WebGUIChecksum:    os.Getenv("FSE_WEB_GUI_CHECKSUM"),
	}
}

func RunFromEnvironment(configPath string) error {
	var (
		cfg config.Config
		err error
	)
	if envBoolOrDefault("FSE_CONTAINER_FIRST_RUN", false) {
		cfg, err = SaveFirstRunDefaults(configPath, DefaultsFromEnvironment())
	} else {
		cfg, err = config.LoadFile(configPath)
	}
	if err != nil {
		return err
	}
	if updated, dhtErr := config.PersistShippedDefaultDHT(configPath, cfg); dhtErr != nil {
		fmt.Fprintf(os.Stderr, "public DHT discovery was not enabled: %v\n", dhtErr)
	} else if updated.Discovery.DHT && !cfg.Discovery.DHT {
		fmt.Fprintln(os.Stderr, "public DHT discovery enabled so pairing codes can be looked up")
		cfg = updated
	} else {
		cfg = updated
	}
	if name := strings.TrimSpace(os.Getenv("FSE_NODE_NAME")); name != "" && envBoolOrDefault("FSE_CONTAINER_FIRST_RUN", false) {
		cfg.NodeName = name
		if err := writeConfig(configPath, cfg); err != nil {
			return err
		}
	}
	if repaired, logErr := config.PersistLoggingOutput(configPath, cfg); logErr != nil {
		fmt.Fprintf(os.Stderr, "log output was not restored: %v\n", logErr)
	} else {
		cfg = repaired
	}
	if err := advertise.Apply(&cfg, os.Getenv(advertise.EnvVar)); err != nil {
		if message := advertise.StartupMessage(cfg.API.AdvertiseURL, err); message != "" {
			fmt.Fprintln(os.Stderr, message)
		}
	} else if message := advertise.StartupMessage(cfg.API.AdvertiseURL, nil); message != "" {
		fmt.Fprintln(os.Stderr, message)
	}
	discovery.SetAnnounceAPIURL(cfg.API.AdvertiseURL)
	cfg, err = ReconcileWebGUI(configPath, cfg, DefaultsFromEnvironment())
	if err != nil {
		return err
	}
	_, err = ExportIdentityPackage(cfg, os.Getenv("FSE_IDENTITY_EXPORT_PATH"), envBoolOrDefault("FSE_IDENTITY_EXPORT_FORCE", false))
	return err
}

// ReconcileWebGUI applies the GUI environment on every container start.
// Unset or false disables the listener. A complete explicit package is installed
// as given. Otherwise the newest catalog package compatible with this engine
// schema is downloaded and checksum-verified. An incompatible or empty catalog
// is logged and the GUI stays off; the daemon still starts.
func ReconcileWebGUI(configPath string, cfg config.Config, defaults RuntimeDefaults) (config.Config, error) {
	if !defaults.WebGUIEnabled {
		if cfg.WebGUI.Enabled {
			cfg.WebGUI.Enabled = false
			if err := writeConfig(configPath, cfg); err != nil {
				return cfg, err
			}
			fmt.Fprintln(os.Stderr, "web GUI disabled because FSE_WEB_GUI_ENABLED is not true")
		}
		return cfg, nil
	}
	if defaults.webGUIOptInComplete() {
		cfg.WebGUI.Enabled = true
		cfg.WebGUI.Version = defaults.WebGUIVersion
		cfg.WebGUI.PackagePath = defaults.WebGUIPackage
		cfg.WebGUI.InstallDir = defaults.WebGUIInstallDir
		cfg.WebGUI.Listen = defaults.WebGUIListen
		cfg.WebGUI.ChecksumSHA256 = defaults.WebGUIChecksum
		cfg.WebGUI.UpdateURL = ""
		if defaults.WebGUITLSEnabled {
			cfg.WebGUI.HTTPSListen = defaults.WebGUIHTTPSListen
		} else {
			cfg.WebGUI.HTTPSListen = ""
		}
		return cfg, writeConfig(configPath, cfg)
	}
	index := os.Getenv("FSE_WEB_GUI_SCHEMA_INDEX")
	if index == "" {
		index = "/usr/local/share/fse/schema-versions.json"
	}
	installDir := defaults.WebGUIInstallDir
	if installDir == "" {
		installDir = "/config/web-gui"
	}
	listen := defaults.WebGUIListen
	if listen == "" {
		listen = "0.0.0.0:8385"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	catalog, err := guischema.LoadCatalog(ctx, index, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web GUI skipped: cannot read schema catalog %s: %v\n", index, err)
		return disableWebGUI(configPath, cfg)
	}
	selected, err := guischema.SelectNewestCompatible(catalog.Packages, schemaversion.Engine)
	if err != nil {
		if errors.Is(err, guischema.ErrNoneCompatible) {
			fmt.Fprintf(os.Stderr, "web GUI skipped: no GUI package is compatible with engine schema %d\n", schemaversion.Engine)
		} else {
			fmt.Fprintf(os.Stderr, "web GUI skipped: %v\n", err)
		}
		return disableWebGUI(configPath, cfg)
	}
	if cfg.WebGUI.Version == selected.GUIVersion && strings.EqualFold(cfg.WebGUI.ChecksumSHA256, selected.ChecksumSHA256) && cfg.WebGUI.PackagePath != "" {
		if _, statErr := os.Stat(cfg.WebGUI.PackagePath); statErr == nil {
			cfg.WebGUI.Enabled = true
			cfg.WebGUI.InstallDir = installDir
			cfg.WebGUI.Listen = listen
			return cfg, writeConfig(configPath, cfg)
		}
	}
	resolved, err := guischema.ResolveNewestCompatible(ctx, index, filepath.Join(filepath.Dir(configPath), "web-gui-packages"), schemaversion.Engine, nil)
	if err != nil {
		if errors.Is(err, guischema.ErrNoneCompatible) {
			fmt.Fprintf(os.Stderr, "web GUI skipped: no GUI package is compatible with engine schema %d\n", schemaversion.Engine)
		} else {
			fmt.Fprintf(os.Stderr, "web GUI skipped: %v\n", err)
		}
		return disableWebGUI(configPath, cfg)
	}
	cfg.WebGUI.Enabled = true
	cfg.WebGUI.Version = resolved.GUIVersion
	cfg.WebGUI.PackagePath = resolved.LocalPath
	cfg.WebGUI.InstallDir = installDir
	cfg.WebGUI.Listen = listen
	cfg.WebGUI.ChecksumSHA256 = resolved.ChecksumSHA256
	cfg.WebGUI.UpdateURL = ""
	fmt.Fprintf(os.Stderr, "web GUI selected package %s for engine schema %d\n", resolved.GUIVersion, schemaversion.Engine)
	return cfg, writeConfig(configPath, cfg)
}

func disableWebGUI(configPath string, cfg config.Config) (config.Config, error) {
	cfg.WebGUI.Enabled = false
	return cfg, writeConfig(configPath, cfg)
}

func writeConfig(path string, cfg config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func bootstrapPeersFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("FSE_DHT_BOOTSTRAP_PEERS"))
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n'
	})
	peers := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			peers = append(peers, field)
		}
	}
	return peers
}

func envBoolOrDefault(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return strings.EqualFold(value, "true")
}

func ApplyFirstRunDefaults(cfg config.Config, defaults RuntimeDefaults) config.Config {
	if defaults.SyncListen != "" {
		cfg.Listen = []string{defaults.SyncListen}
	}
	if defaults.APIListen != "" {
		cfg.API.Listen = defaults.APIListen
	}
	if defaults.LogLevel != "" {
		cfg.Logging.Level = config.LogLevel(defaults.LogLevel)
	}
	if defaults.LogOutput != "" {
		cfg.Logging.Output = defaults.LogOutput
	}
	if name := strings.TrimSpace(os.Getenv("FSE_NODE_NAME")); name != "" {
		cfg.NodeName = name
	}
	cfg.Metadata = config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: "/config/metadata", PerFolder: true}
	cfg.Discovery.Local = defaults.DiscoveryLocal
	cfg.Discovery.DHT = defaults.DiscoveryDHT
	if peers := bootstrapPeersFromEnv(); len(peers) > 0 {
		cfg.Discovery.DHTBootstrapPeers = peers
	}
	if defaults.webGUIOptInComplete() {
		cfg.WebGUI.Enabled = true
		cfg.WebGUI.Version = defaults.WebGUIVersion
		cfg.WebGUI.PackagePath = defaults.WebGUIPackage
		cfg.WebGUI.InstallDir = defaults.WebGUIInstallDir
		cfg.WebGUI.Listen = defaults.WebGUIListen
		cfg.WebGUI.UpdateURL = ""
		cfg.WebGUI.ChecksumSHA256 = defaults.WebGUIChecksum
		if defaults.WebGUITLSEnabled {
			cfg.WebGUI.HTTPSListen = defaults.WebGUIHTTPSListen
		} else {
			cfg.WebGUI.HTTPSListen = ""
		}
	}
	return cfg
}

func (d RuntimeDefaults) webGUIOptInComplete() bool {
	return d.WebGUIEnabled &&
		d.WebGUIVersion != "" &&
		d.WebGUIPackage != "" &&
		d.WebGUIInstallDir != "" &&
		d.WebGUIListen != "" &&
		d.WebGUIChecksum != ""
}

func SaveFirstRunDefaults(path string, defaults RuntimeDefaults) (config.Config, error) {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return config.Config{}, err
	}
	cfg = ApplyFirstRunDefaults(cfg, defaults)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return config.Config{}, err
	}
	if err := config.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

type identityExportPackage struct {
	Version            int                          `json:"version"`
	Kind               string                       `json:"kind"`
	NodeName           string                       `json:"nodeName"`
	IdentityPublicKey  string                       `json:"identityPublicKey"`
	IdentityPrivateKey string                       `json:"identityPrivateKey"`
	IdentityGroups     []config.IdentityGroupConfig `json:"identityGroups"`
}

func ExportIdentityPackage(cfg config.Config, path string, force bool) (bool, error) {
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err == nil && !force {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	pkg := identityExportPackage{
		Version:            1,
		Kind:               "filesyncengine-container-identity-package",
		NodeName:           cfg.NodeName,
		IdentityPublicKey:  cfg.Identity.PublicKey,
		IdentityPrivateKey: cfg.Identity.PrivateKey,
		IdentityGroups:     cfg.Identity.Groups,
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return false, err
	}
	if err := config.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return false, err
	}
	return true, nil
}
