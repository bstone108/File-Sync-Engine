package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"filesyncengine/internal/peeridentity"
)

func EnsureFile(path string) (Config, bool, error) {
	if _, err := os.Stat(path); err == nil {
		cfg, changed, err := EnsureAPIKey(path)
		return cfg, changed, err
	} else if !os.IsNotExist(err) {
		return Config{}, false, err
	}
	key, err := GenerateAPIKey()
	if err != nil {
		return Config{}, false, err
	}
	identity, err := peeridentity.GenerateIdentity()
	if err != nil {
		return Config{}, false, err
	}
	cfg := Config{
		NodeName:    "node-CHANGE-ME",
		Listen:      []string{"tcp://0.0.0.0:22000"},
		API:         APIConfig{Listen: "127.0.0.1:22420", Key: key, Encryption: APIEncryptionConfig{Mode: APIEncryptionAuto}},
		Logging:     LoggingConfig{Level: LogLevelInfo, Output: "stderr"},
		Transfer:    TransferConfig{SendBytesPerSecond: 0, ReceiveBytesPerSecond: 0},
		Backup:      BackupConfig{Enabled: false, Mode: BackupModeBlockArchiveOnly},
		WebGUI:      WebGUIConfig{Enabled: false, InstallDir: "./web/current", Listen: "127.0.0.1:8385"},
		Identity:    IdentityConfig{PrivateKey: identity.PrivateKey, PublicKey: identity.PublicKey, EncryptionLevel: peeridentity.DefaultEncryptionLevel},
		Discovery:   DiscoveryConfig{DHT: false, Local: true, DHTNamespace: "filesyncengine/v1", DHTBootstrapPeers: []string{"/dnsaddr/bootstrap.libp2p.io"}},
		Metadata:    MetadataConfig{Backend: MetadataBackendJSON},
		Maintenance: MaintenanceConfig{Enabled: false, Frequency: "6h", IdleOnly: true, MaxFilesPerRun: 100, MaxBytesPerRun: 104857600, AutoRepair: false},
		Peers: []PeerConfig{{
			ID:        "peer-example",
			Endpoints: []EndpointConfig{{Kind: "manual", Address: "/ip4/192.0.2.10/tcp/22000/p2p/example"}, {Kind: "pipe", Address: "stdio"}},
		}},
		Folders: []FolderConfig{{
			ID:        "docs",
			Path:      "./docs",
			Mode:      ModeSendReceive,
			BlockSize: DefaultBlockSize,
			Ignore:    []string{"*.tmp"},
			Permissions: PermissionPolicy{
				Mode: PermissionIgnore,
			},
			Maintenance: MaintenanceConfig{Enabled: false},
		}},
	}
	if err := WriteFileAtomic(path, []byte(Skeleton(cfg)), 0o600); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func EnsureAPIKey(path string) (Config, bool, error) {
	cfg, err := LoadFile(path)
	if err != nil {
		return Config{}, false, err
	}
	if cfg.API.Key != "" {
		return cfg, false, nil
	}
	key, err := GenerateAPIKey()
	if err != nil {
		return Config{}, false, err
	}
	cfg.API.Key = key
	if cfg.API.Listen == "" {
		cfg.API.Listen = "127.0.0.1:22420"
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Config{}, false, err
	}
	if err := WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func GenerateAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func Skeleton(cfg Config) string {
	return fmt.Sprintf(`{
  // nodeName is this device/computer ID. Use a stable unique value.
  "nodeName": %q,
  // listen lists daemon sync listeners. Keep empty if another wrapper supplies pipe/proxy transport.
  "listen": ["tcp://0.0.0.0:22000"],
  // api exposes realtime status/events for embedding software. X-FSE-API-Key is required.
  // encryption.mode is auto, off, or manual-tls. auto permits plaintext only on loopback;
  // non-loopback API listeners auto-generate a local certificate/key when certFile/keyFile are empty.
  // trustedCertificateSha256 optionally pins the daemon API certificate fingerprint for TOFU/pairing trust.
  "api": {"listen": %q, "key": %q, "encryption": {"mode": "auto", "certFile": "", "keyFile": "", "trustedCertificateSha256": ""}},
  // logging controls structured daemon JSON logs. level is debug, info, warn, error, or off; output is stderr, stdout, or a file path relative to this config.
  "logging": {"level": "info", "output": "stderr"},
  // transfer sets global byte-per-second caps. 0 means disabled/no limit for each direction.
  "transfer": {"sendBytesPerSecond": 0, "receiveBytesPerSecond": 0},
  // backup marks this node as a backup destination when enabled. mode is block-archive-only, mirror-plus-archive, or mirror-plus-full-archive.
  // mirrorPath is optional; when set, snapshot creation mirrors each folder's current files under mirrorPath/<folder-id> for mirror modes.
  // archivePath is optional; when set, snapshot creation verifies and writes selected snapshot blocks into archivePath/blocks/<hash-prefix>/<hash>.
  // checkpointPath is optional; when set, snapshot creation writes offline metadata checkpoint JSON outside the live mutable DB.
  // Restore, retention, and long-running mirror/archive workers are still planned; snapshot-time mirror/archive/checkpoint updates are a prototype foundation.
  "backup": {"enabled": false, "mode": "block-archive-only", "mirrorPath": "", "archivePath": "", "checkpointPath": ""},
  // webGUI defines the optional web management UI package. It is disabled in the core daemon by default.
  // When enabled, version/installDir plus a trusted packagePath or HTTPS updateURL and SHA-256 checksum are required; start/stop/status commands can serve the installed package on listen.
  "webGUI": {"enabled": false, "version": "", "packagePath": "", "installDir": "./web/current", "listen": "127.0.0.1:8385", "updateURL": "", "checksumSHA256": ""},
  // identity signs peer hello messages. privateKey is local secret; share only publicKey with trusted peers.
  "identity": {"privateKey": %q, "publicKey": %q, "encryptionLevel": %d},
  // discovery is optional. Set disabled true to rely only on configured peers.
  // dhtNamespace/dhtBootstrapPeers are used when public DHT discovery is enabled.
  // networkHints.localContainerGatewayIPs can promote exact Docker host-gateway IPs to true-local paths after the deployment proves they reach LAN peers.
  // networkHints.localCIDRs can promote broader deployment-proven LAN/published-port CIDRs when container bridge/NAT inference is too conservative.
  // networkHints.publishedPortMappings promotes only exact hostIP:hostPort published ports to true-local paths, keeping other Docker bridge ports conservative.
  "discovery": {"disabled": false, "dht": false, "local": true, "dhtNamespace": %q, "dhtBootstrapPeers": ["/dnsaddr/bootstrap.libp2p.io"], "networkHints": {"localContainerGatewayIPs": [], "localCIDRs": [], "publishedPortMappings": []}},
  // metadata selects the prototype metadata store. backend is json or badger; path is optional.
  // Badger uses key-level prototype metadata records. Set perFolder true with badger to store each share in a separate DB under path.
  "metadata": {"backend": "json", "perFolder": false},
  // maintenance controls low-priority DB/filesystem scrub crawls. Per-folder settings can override these budgets.
  // Durations use Go syntax like "30m" or "6h". Negative budgets are rejected.
  // scrubMode can be "light-metadata", "sampled-blocks", or "full-blocks"; sampled mode uses sampleEveryNBlocks.
  // autoRepair defaults false: scrub detects and reports corruption, but engine-managed moves/replacements require explicit opt-in.
  "maintenance": {"enabled": false, "frequency": "6h", "idleOnly": true, "maxFilesPerRun": 100, "maxBytesPerRun": 104857600, "maxFilesPerDay": 1000, "maxBytesPerDay": 1073741824, "scrubMode": "light-metadata", "sampleEveryNBlocks": 16, "autoRepair": false},
  // peers are authorized computers/devices. endpoints may be manual, relay, proxy, vpn, or pipe.
  // Per-peer sendBytesPerSecond/receiveBytesPerSecond override the global cap when non-zero.
  "peers": [
    {"id": "peer-example", "sendBytesPerSecond": 0, "receiveBytesPerSecond": 0, "endpoints": [{"kind": "manual", "address": "/ip4/192.0.2.10/tcp/22000/p2p/example"}, {"kind": "pipe", "address": "stdio"}]}
  ],
  // folders define synchronized libraries. mode is sendrecv, sendonly, or recvonly.
  // permissions.mode is ignore, sync, default, or fixed. ignore lets OS defaults/umask decide.
  "folders": [
    {"id": "docs", "path": "./docs", "mode": "sendrecv", "blockSize": %d, "ignore": ["*.tmp"], "permissions": {"mode": "ignore"}, "maintenance": {"enabled": false, "frequency": "6h", "idleOnly": true, "maxFilesPerRun": 100, "maxBytesPerRun": 104857600, "scrubMode": "light-metadata", "sampleEveryNBlocks": 16}}
  ]
}
`, cfg.NodeName, cfg.API.Listen, cfg.API.Key, cfg.Identity.PrivateKey, cfg.Identity.PublicKey, cfg.Identity.EncryptionLevel, cfg.Discovery.DHTNamespace, DefaultBlockSize)
}

func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}
		if ch == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, data[i])
			}
			continue
		}
		out = append(out, ch)
	}
	return out
}
