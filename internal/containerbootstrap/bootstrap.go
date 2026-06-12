package containerbootstrap

import (
	"encoding/json"
	"os"
	"strings"

	"filesyncengine/internal/config"
)

type RuntimeDefaults struct {
	APIListen         string
	SyncListen        string
	LogLevel          string
	LogOutput         string
	DiscoveryLocal    bool
	DiscoveryDHT      bool
	WebGUIEnabled     bool
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
		DiscoveryDHT:      envBoolOrDefault("FSE_DISCOVERY_DHT", false),
		WebGUIEnabled:     envBoolOrDefault("FSE_WEB_GUI_ENABLED", true),
		WebGUIPackage:     envOrDefault("FSE_WEB_GUI_PACKAGE", "/opt/fse/web/fse-web-container-default.zip"),
		WebGUIInstallDir:  envOrDefault("FSE_WEB_GUI_INSTALL_DIR", "/config/web/current"),
		WebGUIListen:      envOrDefault("FSE_WEB_GUI_LISTEN", "0.0.0.0:8385"),
		WebGUITLSEnabled:  envBoolOrDefault("FSE_WEB_GUI_TLS_ENABLED", true),
		WebGUIHTTPSListen: envOrDefault("FSE_WEB_GUI_HTTPS_LISTEN", "0.0.0.0:8943"),
		WebGUIChecksum:    envOrDefault("FSE_WEB_GUI_CHECKSUM", "9f65e8d0ad7bff683a81a9ca081fd8aae53ed43df896b65f1b9c6fd56e0610ab"),
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
	_, err = ExportIdentityPackage(cfg, os.Getenv("FSE_IDENTITY_EXPORT_PATH"), envBoolOrDefault("FSE_IDENTITY_EXPORT_FORCE", false))
	return err
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
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
	cfg.Metadata = config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: "/config/metadata", PerFolder: true}
	cfg.Discovery.Local = defaults.DiscoveryLocal
	cfg.Discovery.DHT = defaults.DiscoveryDHT
	if defaults.WebGUIEnabled {
		cfg.WebGUI.Enabled = true
		cfg.WebGUI.Version = "container-default"
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
