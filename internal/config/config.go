package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/peeridentity"
)

const DefaultBlockSize = 128 * 1024

type FolderMode string

type PermissionMode string

type APIEncryptionMode string

const (
	ModeSendReceive FolderMode = "sendrecv"
	ModeSendOnly    FolderMode = "sendonly"
	ModeReceiveOnly FolderMode = "recvonly"

	PermissionIgnore  PermissionMode = "ignore"
	PermissionSync    PermissionMode = "sync"
	PermissionDefault PermissionMode = "default"
	PermissionFixed   PermissionMode = "fixed"

	APIEncryptionAuto      APIEncryptionMode = "auto"
	APIEncryptionOff       APIEncryptionMode = "off"
	APIEncryptionManualTLS APIEncryptionMode = "manual-tls"
)

type Config struct {
	NodeName    string            `json:"nodeName"`
	Listen      []string          `json:"listen"`
	API         APIConfig         `json:"api"`
	Logging     LoggingConfig     `json:"logging,omitempty"`
	Transfer    TransferConfig    `json:"transfer,omitempty"`
	Backup      BackupConfig      `json:"backup,omitempty"`
	WebGUI      WebGUIConfig      `json:"webGUI,omitempty"`
	Identity    IdentityConfig    `json:"identity"`
	Discovery   DiscoveryConfig   `json:"discovery"`
	Metadata    MetadataConfig    `json:"metadata,omitempty"`
	Maintenance MaintenanceConfig `json:"maintenance,omitempty"`
	Peers       []PeerConfig      `json:"peers"`
	Folders     []FolderConfig    `json:"folders"`
}

type MetadataBackend string
type LogLevel string
type BackupMode string

type MaintenanceScrubMode string

const (
	MetadataBackendJSON   MetadataBackend = "json"
	MetadataBackendBadger MetadataBackend = "badger"

	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelOff   LogLevel = "off"

	BackupModeBlockArchiveOnly      BackupMode = "block-archive-only"
	BackupModeMirrorPlusArchive     BackupMode = "mirror-plus-archive"
	BackupModeMirrorPlusFullArchive BackupMode = "mirror-plus-full-archive"

	MaintenanceScrubLightMetadata MaintenanceScrubMode = "light-metadata"
	MaintenanceScrubSampledBlocks MaintenanceScrubMode = "sampled-blocks"
	MaintenanceScrubFullBlocks    MaintenanceScrubMode = "full-blocks"
)

type MetadataConfig struct {
	Backend MetadataBackend `json:"backend,omitempty"`
	Path    string          `json:"path,omitempty"`
	// PerFolder stores each shared folder in its own physical metadata DB under Path.
	// It is only supported by the Badger backend; aggregate cross-folder lookup is layered above it.
	PerFolder bool `json:"perFolder,omitempty"`
}

type LoggingConfig struct {
	Level  LogLevel `json:"level,omitempty"`
	Output string   `json:"output,omitempty"`
}

type TransferConfig struct {
	SendBytesPerSecond    int64 `json:"sendBytesPerSecond,omitempty"`
	ReceiveBytesPerSecond int64 `json:"receiveBytesPerSecond,omitempty"`
}

type BackupConfig struct {
	Enabled        bool       `json:"enabled,omitempty"`
	Mode           BackupMode `json:"mode,omitempty"`
	MirrorPath     string     `json:"mirrorPath,omitempty"`
	ArchivePath    string     `json:"archivePath,omitempty"`
	CheckpointPath string     `json:"checkpointPath,omitempty"`
}

type WebGUIConfig struct {
	Enabled        bool   `json:"enabled,omitempty"`
	Version        string `json:"version,omitempty"`
	PackagePath    string `json:"packagePath,omitempty"`
	InstallDir     string `json:"installDir,omitempty"`
	Listen         string `json:"listen,omitempty"`
	HTTPSListen    string `json:"httpsListen,omitempty"`
	TLSCertFile    string `json:"tlsCertFile,omitempty"`
	TLSKeyFile     string `json:"tlsKeyFile,omitempty"`
	UpdateURL      string `json:"updateURL,omitempty"`
	ChecksumSHA256 string `json:"checksumSHA256,omitempty"`
}

type EffectiveTransferConfig struct {
	SendBytesPerSecond    int64 `json:"sendBytesPerSecond"`
	ReceiveBytesPerSecond int64 `json:"receiveBytesPerSecond"`
}

type TransferLimitDetails struct {
	Effective    EffectiveTransferConfig
	SendCause    string
	ReceiveCause string
}

func EffectiveTransferLimits(localGlobal TransferConfig, localPeer PeerConfig, remoteGlobal TransferConfig, remotePeer PeerConfig) EffectiveTransferConfig {
	return EffectiveTransferLimitDetails(localGlobal, localPeer, remoteGlobal, remotePeer).Effective
}

func EffectiveTransferLimitDetails(localGlobal TransferConfig, localPeer PeerConfig, remoteGlobal TransferConfig, remotePeer PeerConfig) TransferLimitDetails {
	localSend, localSendCause := peerCapOrGlobalWithCause(localPeer.SendBytesPerSecond, localGlobal.SendBytesPerSecond, "local_peer", "local_global")
	localReceive, localReceiveCause := peerCapOrGlobalWithCause(localPeer.ReceiveBytesPerSecond, localGlobal.ReceiveBytesPerSecond, "local_peer", "local_global")
	remoteSend, remoteSendCause := peerCapOrGlobalWithCause(remotePeer.SendBytesPerSecond, remoteGlobal.SendBytesPerSecond, "remote_send", "remote_send")
	remoteReceive, remoteReceiveCause := peerCapOrGlobalWithCause(remotePeer.ReceiveBytesPerSecond, remoteGlobal.ReceiveBytesPerSecond, "remote_receive", "remote_receive")
	send, sendCause := lowestNonZeroWithCause(localSend, localSendCause, remoteReceive, remoteReceiveCause)
	receive, receiveCause := lowestNonZeroWithCause(localReceive, localReceiveCause, remoteSend, remoteSendCause)
	return TransferLimitDetails{
		Effective: EffectiveTransferConfig{
			SendBytesPerSecond:    send,
			ReceiveBytesPerSecond: receive,
		},
		SendCause:    sendCause,
		ReceiveCause: receiveCause,
	}
}

func peerCapOrGlobal(peerCap int64, globalCap int64) int64 {
	cap, _ := peerCapOrGlobalWithCause(peerCap, globalCap, "peer", "global")
	return cap
}

func peerCapOrGlobalWithCause(peerCap int64, globalCap int64, peerCause string, globalCause string) (int64, string) {
	if peerCap > 0 {
		return peerCap, peerCause
	}
	if globalCap > 0 {
		return globalCap, globalCause
	}
	return 0, "unlimited"
}

func lowestNonZeroWithCause(a int64, aCause string, b int64, bCause string) (int64, string) {
	if a == 0 && b == 0 {
		return 0, "unlimited"
	}
	if a == 0 {
		return b, bCause
	}
	if b == 0 || a <= b {
		return a, aCause
	}
	return b, bCause
}

func lowestNonZero(a int64, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

type MaintenanceConfig struct {
	Enabled            bool                 `json:"enabled,omitempty"`
	Frequency          string               `json:"frequency,omitempty"`
	IdleOnly           bool                 `json:"idleOnly,omitempty"`
	MaxFilesPerRun     int                  `json:"maxFilesPerRun,omitempty"`
	MaxBytesPerRun     int64                `json:"maxBytesPerRun,omitempty"`
	MaxFilesPerDay     int                  `json:"maxFilesPerDay,omitempty"`
	MaxBytesPerDay     int64                `json:"maxBytesPerDay,omitempty"`
	ScrubMode          MaintenanceScrubMode `json:"scrubMode,omitempty"`
	SampleEveryNBlocks int                  `json:"sampleEveryNBlocks,omitempty"`
	AutoRepair         bool                 `json:"autoRepair,omitempty"`
}

type IdentityConfig struct {
	PrivateKey         string                  `json:"privateKey,omitempty"`
	PublicKey          string                  `json:"publicKey,omitempty"`
	EncryptionLevel    int                     `json:"encryptionLevel"`
	Groups             []IdentityGroupConfig   `json:"groups,omitempty"`
	Revoked            []RevokedIdentityConfig `json:"revoked,omitempty"`
	encryptionLevelSet bool
}

type IdentityGroupConfig struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
}

type RevokedIdentityConfig struct {
	GroupID               string    `json:"groupId"`
	DiscoveryID           string    `json:"discoveryId,omitempty"`
	BootstrapProofKeyHash string    `json:"bootstrapProofKeyHash"`
	RevokedAt             time.Time `json:"revokedAt,omitempty"`
}

func (i *IdentityConfig) UnmarshalJSON(data []byte) error {
	type identityAlias IdentityConfig
	var raw struct {
		*identityAlias
		EncryptionLevel *int `json:"encryptionLevel"`
	}
	raw.identityAlias = (*identityAlias)(i)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.EncryptionLevel == nil {
		i.EncryptionLevel = peeridentity.DefaultEncryptionLevel
		i.encryptionLevelSet = false
		return nil
	}
	i.EncryptionLevel = *raw.EncryptionLevel
	i.encryptionLevelSet = true
	return nil
}

type APIConfig struct {
	Listen     string              `json:"listen"`
	Key        string              `json:"key"`
	Encryption APIEncryptionConfig `json:"encryption,omitempty"`
}

type APIEncryptionConfig struct {
	Mode                     APIEncryptionMode `json:"mode,omitempty"`
	CertFile                 string            `json:"certFile,omitempty"`
	KeyFile                  string            `json:"keyFile,omitempty"`
	TrustedCertificateSHA256 string            `json:"trustedCertificateSha256,omitempty"`
}

func (a APIConfig) RequiresTLS() bool {
	mode := a.Encryption.Mode
	if mode == "" {
		mode = APIEncryptionAuto
	}
	switch mode {
	case APIEncryptionManualTLS:
		return true
	case APIEncryptionAuto:
		return a.Listen != "" && !apiListenIsLoopback(a.Listen)
	default:
		return false
	}
}

type DiscoveryConfig struct {
	Disabled          bool               `json:"disabled"`
	DHT               bool               `json:"dht"`
	Local             bool               `json:"local"`
	DHTNamespace      string             `json:"dhtNamespace,omitempty"`
	DHTBootstrapPeers []string           `json:"dhtBootstrapPeers,omitempty"`
	NetworkHints      NetworkHintsConfig `json:"networkHints,omitempty"`
}

type NetworkHintsConfig struct {
	LocalContainerGatewayIPs []string                     `json:"localContainerGatewayIPs,omitempty"`
	LocalCIDRs               []string                     `json:"localCIDRs,omitempty"`
	PublishedPortMappings    []PublishedPortMappingConfig `json:"publishedPortMappings,omitempty"`
}

type PublishedPortMappingConfig struct {
	HostIP        string `json:"hostIP"`
	HostPort      int    `json:"hostPort"`
	ContainerIP   string `json:"containerIP,omitempty"`
	ContainerPort int    `json:"containerPort,omitempty"`
}

func (d DiscoveryConfig) AllDisabled() bool {
	return d.Disabled || (!d.DHT && !d.Local)
}

type PeerConfig struct {
	ID                    string           `json:"id"`
	APIKey                string           `json:"apiKey,omitempty"`
	IdentityPublicKey     string           `json:"identityPublicKey,omitempty"`
	EncryptionLevel       int              `json:"encryptionLevel"`
	SendBytesPerSecond    int64            `json:"sendBytesPerSecond,omitempty"`
	ReceiveBytesPerSecond int64            `json:"receiveBytesPerSecond,omitempty"`
	Addresses             []string         `json:"addresses"`
	Endpoints             []EndpointConfig `json:"endpoints"`
	encryptionLevelSet    bool
}

func (p *PeerConfig) UnmarshalJSON(data []byte) error {
	type peerAlias PeerConfig
	var raw struct {
		*peerAlias
		EncryptionLevel *int `json:"encryptionLevel"`
	}
	raw.peerAlias = (*peerAlias)(p)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.EncryptionLevel == nil {
		p.EncryptionLevel = peeridentity.DefaultEncryptionLevel
		p.encryptionLevelSet = false
		return nil
	}
	p.EncryptionLevel = *raw.EncryptionLevel
	p.encryptionLevelSet = true
	return nil
}

type EndpointConfig struct {
	Kind        string `json:"kind"`
	Address     string `json:"address"`
	NetworkHint string `json:"networkHint,omitempty"`
}

type FolderConfig struct {
	ID            string            `json:"id"`
	Path          string            `json:"path"`
	Enabled       bool              `json:"enabled"`
	AdvertisedBy  string            `json:"advertisedBy,omitempty"`
	IdentityGroup string            `json:"identityGroup,omitempty"`
	SyncGroup     string            `json:"syncGroup,omitempty"`
	Mode          FolderMode        `json:"mode"`
	BlockSize     int               `json:"blockSize"`
	Ignore        []string          `json:"ignore"`
	Permissions   PermissionPolicy  `json:"permissions,omitempty"`
	Maintenance   MaintenanceConfig `json:"maintenance,omitempty"`
	enabledSet    bool
}

func (f *FolderConfig) UnmarshalJSON(data []byte) error {
	type folderAlias FolderConfig
	var raw struct {
		*folderAlias
		Enabled *bool `json:"enabled"`
	}
	raw.folderAlias = (*folderAlias)(f)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Enabled == nil {
		f.Enabled = true
		f.enabledSet = false
		return nil
	}
	f.Enabled = *raw.Enabled
	f.enabledSet = true
	return nil
}

type PermissionPolicy struct {
	Mode          PermissionMode `json:"mode"`
	FileMode      string         `json:"fileMode,omitempty"`
	DirMode       string         `json:"dirMode,omitempty"`
	PreserveOwner bool           `json:"preserveOwner,omitempty"`
	PreserveGroup bool           `json:"preserveGroup,omitempty"`
	PreserveACL   bool           `json:"preserveACL,omitempty"`
}

func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(stripJSONComments(data), &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.NodeName == "" {
		return errors.New("nodeName is required")
	}
	if c.Discovery.Disabled && (c.Discovery.DHT || c.Discovery.Local) {
		return errors.New("discovery.disabled cannot be true when discovery.dht or discovery.local is enabled")
	}
	if c.Discovery.DHT && c.Discovery.DHTNamespace == "" {
		c.Discovery.DHTNamespace = "filesyncengine/v1"
	}
	if err := validateLoggingConfig(c.Logging); err != nil {
		return err
	}
	if err := validateAPIConfig(&c.API); err != nil {
		return err
	}
	if err := validateTransferConfig("transfer", c.Transfer); err != nil {
		return err
	}
	if err := validateBackupConfig(c.Backup); err != nil {
		return err
	}
	if err := validateWebGUIConfig(c.WebGUI); err != nil {
		return err
	}
	switch c.Metadata.Backend {
	case "":
		c.Metadata.Backend = MetadataBackendJSON
	case MetadataBackendJSON, MetadataBackendBadger:
	default:
		return fmt.Errorf("metadata.backend has invalid value %q", c.Metadata.Backend)
	}
	if c.Metadata.PerFolder && c.Metadata.Backend != MetadataBackendBadger {
		return errors.New("metadata.perFolder requires metadata.backend to be badger")
	}
	for i, peer := range c.Discovery.DHTBootstrapPeers {
		if peer == "" {
			return fmt.Errorf("discovery.dhtBootstrapPeers[%d] is required", i)
		}
	}
	for i, ip := range c.Discovery.NetworkHints.LocalContainerGatewayIPs {
		trimmed := strings.TrimSpace(ip)
		if net.ParseIP(trimmed) == nil {
			return fmt.Errorf("discovery.networkHints.localContainerGatewayIPs[%d] must be an IP address", i)
		}
		c.Discovery.NetworkHints.LocalContainerGatewayIPs[i] = trimmed
	}
	for i, cidr := range c.Discovery.NetworkHints.LocalCIDRs {
		trimmed := strings.TrimSpace(cidr)
		if _, _, err := net.ParseCIDR(trimmed); err != nil {
			return fmt.Errorf("discovery.networkHints.localCIDRs[%d] must be a CIDR", i)
		}
		c.Discovery.NetworkHints.LocalCIDRs[i] = trimmed
	}
	for i := range c.Discovery.NetworkHints.PublishedPortMappings {
		mapping := &c.Discovery.NetworkHints.PublishedPortMappings[i]
		mapping.HostIP = strings.TrimSpace(mapping.HostIP)
		mapping.ContainerIP = strings.TrimSpace(mapping.ContainerIP)
		if net.ParseIP(mapping.HostIP) == nil {
			return fmt.Errorf("discovery.networkHints.publishedPortMappings[%d].hostIP must be an IP address", i)
		}
		if !validTCPPort(mapping.HostPort) {
			return fmt.Errorf("discovery.networkHints.publishedPortMappings[%d].hostPort must be between 1 and 65535", i)
		}
		if mapping.ContainerIP != "" && net.ParseIP(mapping.ContainerIP) == nil {
			return fmt.Errorf("discovery.networkHints.publishedPortMappings[%d].containerIP must be an IP address", i)
		}
		if mapping.ContainerPort != 0 && !validTCPPort(mapping.ContainerPort) {
			return fmt.Errorf("discovery.networkHints.publishedPortMappings[%d].containerPort must be between 1 and 65535", i)
		}
	}
	if c.Identity.EncryptionLevel == 0 && !c.Identity.encryptionLevelSet {
		c.Identity.EncryptionLevel = peeridentity.DefaultEncryptionLevel
	}
	if err := peeridentity.ValidateEncryptionLevel(c.Identity.EncryptionLevel); err != nil {
		return fmt.Errorf("identity.%w", err)
	}
	if err := validateMaintenanceConfig("maintenance", c.Maintenance); err != nil {
		return err
	}
	seenGroups := map[string]struct{}{}
	for i, group := range c.Identity.Groups {
		if group.ID == "" {
			return fmt.Errorf("identity.groups[%d].id is required", i)
		}
		if _, ok := seenGroups[group.ID]; ok {
			return fmt.Errorf("duplicate identity group id %q", group.ID)
		}
		seenGroups[group.ID] = struct{}{}
		if group.Enabled && len(group.Token) < 64 {
			return fmt.Errorf("identity group %q token must be at least 64 characters", group.ID)
		}
	}
	for i, record := range c.Identity.Revoked {
		if record.GroupID == "" {
			return fmt.Errorf("identity.revoked[%d].groupId is required", i)
		}
		if record.BootstrapProofKeyHash == "" {
			return fmt.Errorf("identity.revoked[%d].bootstrapProofKeyHash is required", i)
		}
	}
	seenFolders := map[string]struct{}{}
	for i := range c.Folders {
		f := &c.Folders[i]
		if f.ID == "" {
			return fmt.Errorf("folders[%d].id is required", i)
		}
		if _, ok := seenFolders[f.ID]; ok {
			return fmt.Errorf("duplicate folder id %q", f.ID)
		}
		seenFolders[f.ID] = struct{}{}
		if !f.enabledSet && !f.Enabled {
			f.Enabled = true
		}
		if f.Path == "" && f.Enabled {
			return fmt.Errorf("folder %q path is required", f.ID)
		}
		if !f.Enabled && f.Path == "" {
			if f.AdvertisedBy == "" {
				return fmt.Errorf("disabled folder %q advertisedBy is required when path is empty", f.ID)
			}
			if f.IdentityGroup == "" {
				return fmt.Errorf("disabled folder %q identityGroup is required when path is empty", f.ID)
			}
		}
		if f.Mode == "" {
			f.Mode = ModeSendReceive
		}
		switch f.Mode {
		case ModeSendReceive, ModeSendOnly, ModeReceiveOnly:
		default:
			return fmt.Errorf("folder %q has invalid mode %q", f.ID, f.Mode)
		}
		if f.BlockSize == 0 {
			f.BlockSize = DefaultBlockSize
		}
		if f.BlockSize < 4096 {
			return fmt.Errorf("folder %q blockSize must be at least 4096", f.ID)
		}
		if err := validatePermissionPolicy(f.ID, &f.Permissions); err != nil {
			return err
		}
		if err := validateMaintenanceConfig(fmt.Sprintf("folder %q maintenance", f.ID), f.Maintenance); err != nil {
			return err
		}
	}
	seenPeers := map[string]struct{}{}
	for i, p := range c.Peers {
		if p.ID == "" {
			return fmt.Errorf("peers[%d].id is required", i)
		}
		if _, ok := seenPeers[p.ID]; ok {
			return fmt.Errorf("duplicate peer id %q", p.ID)
		}
		if p.EncryptionLevel == 0 && !p.encryptionLevelSet {
			p.EncryptionLevel = peeridentity.DefaultEncryptionLevel
		}
		if err := peeridentity.ValidateEncryptionLevel(p.EncryptionLevel); err != nil {
			return fmt.Errorf("peer %q %w", p.ID, err)
		}
		if err := validateTransferConfig(fmt.Sprintf("peer %q transfer", p.ID), TransferConfig{SendBytesPerSecond: p.SendBytesPerSecond, ReceiveBytesPerSecond: p.ReceiveBytesPerSecond}); err != nil {
			return err
		}
		seenPeers[p.ID] = struct{}{}
		for j, endpoint := range p.Endpoints {
			if !validEndpointKind(endpoint.Kind) {
				return fmt.Errorf("peer %q endpoint[%d] has invalid kind %q", p.ID, j, endpoint.Kind)
			}
			if endpoint.Address == "" {
				return fmt.Errorf("peer %q endpoint[%d] address is required", p.ID, j)
			}
			if !validEndpointNetworkHint(endpoint.NetworkHint) {
				return fmt.Errorf("peer %q endpoint[%d] has invalid networkHint %q", p.ID, j, endpoint.NetworkHint)
			}
		}
	}
	return nil
}

func validEndpointKind(kind string) bool {
	switch kind {
	case "manual", "relay", "proxy", "vpn", "pipe", "sidecar":
		return true
	default:
		return false
	}
}

func validEndpointNetworkHint(hint string) bool {
	switch hint {
	case "", "local", "wan", "vpn_overlay", "container_bridge":
		return true
	default:
		return false
	}
}

func validTCPPort(port int) bool {
	return port >= 1 && port <= 65535
}

func validateBackupConfig(cfg BackupConfig) error {
	switch cfg.Mode {
	case "", BackupModeBlockArchiveOnly, BackupModeMirrorPlusArchive, BackupModeMirrorPlusFullArchive:
		return nil
	default:
		return fmt.Errorf("backup.mode has invalid value %q", cfg.Mode)
	}
}

func validateWebGUIConfig(cfg WebGUIConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Version == "" {
		return errors.New("webGUI.version is required when webGUI.enabled is true")
	}
	if cfg.InstallDir == "" {
		return errors.New("webGUI.installDir is required when webGUI.enabled is true")
	}
	if cfg.PackagePath == "" && cfg.UpdateURL == "" {
		return errors.New("webGUI requires packagePath or updateURL when enabled")
	}
	if cfg.ChecksumSHA256 == "" {
		return errors.New("webGUI.checksumSHA256 is required when webGUI.enabled is true")
	}
	if len(cfg.ChecksumSHA256) != sha256.Size*2 {
		return errors.New("webGUI.checksumSHA256 must be a 64-character SHA-256 hex digest")
	}
	for _, ch := range cfg.ChecksumSHA256 {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return errors.New("webGUI.checksumSHA256 must be a 64-character SHA-256 hex digest")
		}
	}
	if cfg.UpdateURL != "" && !strings.HasPrefix(cfg.UpdateURL, "https://") {
		return errors.New("webGUI.updateURL must use https")
	}
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return errors.New("webGUI TLS requires both tlsCertFile and tlsKeyFile when either is configured")
	}
	return nil
}

func validateLoggingConfig(cfg LoggingConfig) error {
	switch cfg.Level {
	case "", LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError, LogLevelOff:
		return nil
	default:
		return fmt.Errorf("logging.level has invalid value %q", cfg.Level)
	}
}

func validateAPIConfig(cfg *APIConfig) error {
	if cfg.Encryption.Mode == "" {
		cfg.Encryption.Mode = APIEncryptionAuto
	}
	if fp := cfg.Encryption.TrustedCertificateSHA256; fp != "" {
		decoded, err := hex.DecodeString(fp)
		if err != nil || len(decoded) != 32 || strings.ToLower(fp) != fp {
			return errors.New("api.encryption.trustedCertificateSha256 must be a lowercase SHA-256 hex fingerprint")
		}
	}
	switch cfg.Encryption.Mode {
	case APIEncryptionAuto:
		if cfg.RequiresTLS() && ((cfg.Encryption.CertFile == "") != (cfg.Encryption.KeyFile == "")) {
			return errors.New("api.encryption auto requires both certFile and keyFile when either is configured")
		}
	case APIEncryptionOff:
		if cfg.Listen != "" && !apiListenIsLoopback(cfg.Listen) {
			return errors.New("api.encryption off is allowed only for loopback listeners")
		}
	case APIEncryptionManualTLS:
		if cfg.Encryption.CertFile == "" || cfg.Encryption.KeyFile == "" {
			return errors.New("api.encryption manual-tls requires certFile and keyFile")
		}
	default:
		return fmt.Errorf("api.encryption.mode has invalid value %q", cfg.Encryption.Mode)
	}
	return nil
}

func apiListenIsLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = listen
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateTransferConfig(label string, cfg TransferConfig) error {
	if cfg.SendBytesPerSecond < 0 {
		return fmt.Errorf("%s.sendBytesPerSecond cannot be negative", label)
	}
	if cfg.ReceiveBytesPerSecond < 0 {
		return fmt.Errorf("%s.receiveBytesPerSecond cannot be negative", label)
	}
	return nil
}

func validateMaintenanceConfig(label string, cfg MaintenanceConfig) error {
	if cfg.Frequency != "" {
		if _, err := time.ParseDuration(cfg.Frequency); err != nil {
			return fmt.Errorf("%s.frequency must be a Go duration like 1h or 30m", label)
		}
	}
	switch cfg.ScrubMode {
	case "", MaintenanceScrubLightMetadata, MaintenanceScrubSampledBlocks, MaintenanceScrubFullBlocks:
	default:
		return fmt.Errorf("%s.scrubMode has invalid value %q", label, cfg.ScrubMode)
	}
	if cfg.SampleEveryNBlocks < 0 {
		return fmt.Errorf("%s.sampleEveryNBlocks cannot be negative", label)
	}
	if cfg.ScrubMode == MaintenanceScrubSampledBlocks && cfg.SampleEveryNBlocks == 0 {
		return fmt.Errorf("%s.sampleEveryNBlocks must be positive when scrubMode is sampled-blocks", label)
	}
	if cfg.MaxFilesPerRun < 0 {
		return fmt.Errorf("%s.maxFilesPerRun cannot be negative", label)
	}
	if cfg.MaxBytesPerRun < 0 {
		return fmt.Errorf("%s.maxBytesPerRun cannot be negative", label)
	}
	if cfg.MaxFilesPerDay < 0 {
		return fmt.Errorf("%s.maxFilesPerDay cannot be negative", label)
	}
	if cfg.MaxBytesPerDay < 0 {
		return fmt.Errorf("%s.maxBytesPerDay cannot be negative", label)
	}
	return nil
}

func validatePermissionPolicy(folderID string, policy *PermissionPolicy) error {
	if policy.Mode == "" {
		policy.Mode = PermissionIgnore
	}
	switch policy.Mode {
	case PermissionIgnore, PermissionSync, PermissionDefault, PermissionFixed:
	default:
		return fmt.Errorf("folder %q has invalid permission mode %q", folderID, policy.Mode)
	}
	if policy.FileMode != "" && !validOctalMode(policy.FileMode) {
		return fmt.Errorf("folder %q permissions.fileMode must be a four-digit octal mode", folderID)
	}
	if policy.DirMode != "" && !validOctalMode(policy.DirMode) {
		return fmt.Errorf("folder %q permissions.dirMode must be a four-digit octal mode", folderID)
	}
	return nil
}

func validOctalMode(mode string) bool {
	if len(mode) != 4 {
		return false
	}
	for _, ch := range mode {
		if ch < '0' || ch > '7' {
			return false
		}
	}
	return true
}

type Manager struct {
	path    string
	mu      sync.RWMutex
	current Config
	modTime time.Time
	digest  [32]byte
}

func NewManager(path string) (*Manager, error) {
	cfg, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &Manager{path: path, current: cfg, modTime: st.ModTime(), digest: sha256.Sum256(data)}, nil
}

func (m *Manager) Current() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *Manager) ReloadIfChanged() (bool, error) {
	st, err := os.Stat(m.path)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(data)
	m.mu.RLock()
	knownDigest := m.digest
	m.mu.RUnlock()
	if digest == knownDigest {
		return false, nil
	}
	var cfg Config
	if err := json.Unmarshal(stripJSONComments(data), &cfg); err != nil {
		return false, err
	}
	if err := cfg.Validate(); err != nil {
		return false, err
	}
	m.mu.Lock()
	m.current = cfg
	m.modTime = st.ModTime()
	m.digest = digest
	m.mu.Unlock()
	return true, nil
}
