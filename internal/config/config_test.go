package config

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigAcceptsManualPeersAndFolderModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"listen":["tcp://127.0.0.1:22000"],
		"discovery":{"dht":true,"local":false},
		"peers":[{"id":"node-b","addresses":["/ip4/127.0.0.1/tcp/22001/p2p/peer"]}],
		"folders":[
			{"id":"docs","path":"./docs","mode":"sendrecv"},
			{"id":"backup","path":"./backup","mode":"sendonly","blockSize":65536},
			{"id":"mirror","path":"./mirror","mode":"recvonly"}
		]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if cfg.NodeName != "node-a" || !cfg.Discovery.DHT || cfg.Discovery.Local {
		t.Fatalf("unexpected top-level config: %+v", cfg)
	}
	if got := len(cfg.Peers); got != 1 {
		t.Fatalf("expected 1 peer, got %d", got)
	}
	if cfg.Folders[0].BlockSize != DefaultBlockSize {
		t.Fatalf("default block size = %d, want %d", cfg.Folders[0].BlockSize, DefaultBlockSize)
	}
	if cfg.Folders[1].BlockSize != 65536 {
		t.Fatalf("explicit block size not preserved")
	}
}

func TestLoadConfigRejectsDuplicateFoldersAndInvalidModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"folders":[
			{"id":"docs","path":"./docs","mode":"sendrecv"},
			{"id":"docs","path":"./docs2","mode":"bogus"}
		]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestLoadConfigRejectsInvalidEndpointNetworkHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"peers":[{"id":"peer-a","apiKey":"secret","endpoints":[{"kind":"manual","address":"http://172.17.0.1:22000","networkHint":"nearby-ish"}]}],
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid endpoint network hint to be rejected")
	}
}

func TestLoadConfigAcceptsSidecarEndpointKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"peers":[{"id":"peer-a","apiKey":"secret","endpoints":[{"kind":"sidecar","address":"http://172.18.0.1:32200"}]}],
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("expected sidecar endpoint kind to load, got %v", err)
	}
	if got := cfg.Peers[0].Endpoints[0].Kind; got != "sidecar" {
		t.Fatalf("expected sidecar endpoint kind to be preserved, got %q", got)
	}
}

func TestLoadConfigAcceptsDiscoveryNetworkHints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"networkHints":{"localContainerGatewayIPs":["172.17.0.1"," 10.0.2.2 "],"localCIDRs":["172.20.0.0/16"," 192.168.44.0/24 "],"publishedPortMappings":[{"hostIP":" 172.18.0.1 ","hostPort":32200,"containerIP":" 172.18.0.5 ","containerPort":22000}]}},
		"peers":[{"id":"peer-a","apiKey":"secret","endpoints":[{"kind":"manual","address":"http://172.17.0.1:22000"}]}],
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := cfg.Discovery.NetworkHints.LocalContainerGatewayIPs; len(got) != 2 || got[0] != "172.17.0.1" || got[1] != "10.0.2.2" {
		t.Fatalf("network hints not preserved: %+v", cfg.Discovery.NetworkHints)
	}
	if got := cfg.Discovery.NetworkHints.LocalCIDRs; len(got) != 2 || got[0] != "172.20.0.0/16" || got[1] != "192.168.44.0/24" {
		t.Fatalf("local CIDR hints not preserved: %+v", cfg.Discovery.NetworkHints)
	}
	if got := cfg.Discovery.NetworkHints.PublishedPortMappings; len(got) != 1 || got[0].HostIP != "172.18.0.1" || got[0].HostPort != 32200 || got[0].ContainerIP != "172.18.0.5" || got[0].ContainerPort != 22000 {
		t.Fatalf("published port mappings not preserved: %+v", cfg.Discovery.NetworkHints.PublishedPortMappings)
	}
}

func TestLoadConfigRejectsInvalidPublishedPortMapping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"networkHints":{"publishedPortMappings":[{"hostIP":"172.18.0.1","hostPort":70000}]}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "discovery.networkHints.publishedPortMappings[0].hostPort") {
		t.Fatalf("expected invalid published port mapping error, got %v", err)
	}
}

func TestLoadConfigRejectsInvalidDiscoveryNetworkHintIP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"networkHints":{"localContainerGatewayIPs":["definitely-not-an-ip"]}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid discovery network hint IP to be rejected")
	}
}

func TestLoadConfigRejectsInvalidDiscoveryNetworkHintCIDR(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"networkHints":{"localCIDRs":["not-a-cidr"]}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "discovery.networkHints.localCIDRs[0] must be a CIDR") {
		t.Fatalf("expected invalid discovery local CIDR error, got %v", err)
	}
}

func TestLoadConfigAcceptsMetadataStoreBackendSelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"metadata":{"backend":"badger","path":"./state.badger","perFolder":true},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Metadata.Backend != MetadataBackendBadger || cfg.Metadata.Path != "./state.badger" || !cfg.Metadata.PerFolder {
		t.Fatalf("metadata store config not preserved: %+v", cfg.Metadata)
	}
}

func TestLoadConfigRejectsPerFolderJSONMetadataStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"metadata":{"backend":"json","perFolder":true},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Fatalf("expected per-folder JSON metadata validation error")
	}
}

func TestLoadConfigAcceptsLoggingLevelAndOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"logging":{"level":"warn","output":"./fse.log"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Logging.Level != LogLevelWarn || cfg.Logging.Output != "./fse.log" {
		t.Fatalf("logging config not preserved: %+v", cfg.Logging)
	}
}

func TestLoadConfigAcceptsWebGUIPackageTrustContract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"webGUI":{"enabled":true,"version":"1.2.3","packagePath":"./web/fse-web-1.2.3.zip","installDir":"./web/current","updateURL":"https://updates.example.test/fse-web/manifest.json","checksumSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.WebGUI.Enabled || cfg.WebGUI.Version != "1.2.3" || cfg.WebGUI.PackagePath == "" || cfg.WebGUI.InstallDir == "" || cfg.WebGUI.UpdateURL == "" || cfg.WebGUI.ChecksumSHA256 == "" {
		t.Fatalf("web GUI package contract not preserved: %+v", cfg.WebGUI)
	}
}

func TestLoadConfigRejectsEnabledWebGUIWithoutTrustedPackageSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"webGUI":{"enabled":true,"version":"1.2.3","installDir":"./web/current"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected enabled web GUI without packagePath/updateURL and checksum to be rejected")
	}
}

func TestLoadConfigDefaultsAPIEncryptionPolicyForLoopback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"api":{"listen":"127.0.0.1:22420","key":"test-key"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.API.Encryption.Mode != APIEncryptionAuto {
		t.Fatalf("default API encryption mode = %q, want %q", cfg.API.Encryption.Mode, APIEncryptionAuto)
	}
	if cfg.API.RequiresTLS() {
		t.Fatalf("loopback auto API should not require TLS")
	}
}

func TestLoadConfigRejectsRemotePlaintextAPI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"api":{"listen":"0.0.0.0:22420","key":"test-key","encryption":{"mode":"off"}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected remote plaintext API listener to be rejected")
	}
}

func TestLoadConfigRejectsUnspecifiedHostPlaintextAPI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"api":{"listen":":22420","key":"test-key","encryption":{"mode":"off"}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected unspecified-host plaintext API listener to be rejected")
	}
}

func TestLoadConfigAcceptsManualAPITLSCertificatePolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"api":{"listen":"0.0.0.0:22420","key":"test-key","encryption":{"mode":"manual-tls","certFile":"./api.crt","keyFile":"./api.key","trustedCertificateSha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.API.Encryption.Mode != APIEncryptionManualTLS || cfg.API.Encryption.CertFile != "./api.crt" || cfg.API.Encryption.KeyFile != "./api.key" || cfg.API.Encryption.TrustedCertificateSHA256 == "" {
		t.Fatalf("API encryption policy not preserved: %+v", cfg.API.Encryption)
	}
	if !cfg.API.RequiresTLS() {
		t.Fatalf("manual TLS API policy should require TLS")
	}
}

func TestLoadConfigRejectsInvalidAPICertificateFingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"api":{"listen":"0.0.0.0:22420","key":"test-key","encryption":{"mode":"manual-tls","certFile":"./api.crt","keyFile":"./api.key","trustedCertificateSha256":"ABC"}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "trustedCertificateSha256") {
		t.Fatalf("expected invalid trusted certificate fingerprint to be rejected, got %v", err)
	}
}

func TestEnsureAPITLSAssetsGeneratesAutoCertificateForRemoteListener(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"api":{"listen":"0.0.0.0:22420","key":"test-key","encryption":{"mode":"auto"}},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if err := EnsureAPITLSAssets(&cfg, path); err != nil {
		t.Fatalf("EnsureAPITLSAssets: %v", err)
	}
	if cfg.API.Encryption.CertFile == "" || cfg.API.Encryption.KeyFile == "" {
		t.Fatalf("auto TLS paths were not populated: %+v", cfg.API.Encryption)
	}
	if !filepath.IsAbs(cfg.API.Encryption.CertFile) || !filepath.IsAbs(cfg.API.Encryption.KeyFile) {
		t.Fatalf("auto TLS paths should be absolute for runtime use: %+v", cfg.API.Encryption)
	}
	certInfo, err := os.Stat(cfg.API.Encryption.CertFile)
	if err != nil {
		t.Fatalf("stat generated cert: %v", err)
	}
	keyInfo, err := os.Stat(cfg.API.Encryption.KeyFile)
	if err != nil {
		t.Fatalf("stat generated key: %v", err)
	}
	if certInfo.Mode().Perm() != 0o600 || keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("generated TLS files should be private: cert=%o key=%o", certInfo.Mode().Perm(), keyInfo.Mode().Perm())
	}
	if _, err := tls.LoadX509KeyPair(cfg.API.Encryption.CertFile, cfg.API.Encryption.KeyFile); err != nil {
		t.Fatalf("generated TLS pair is not loadable: %v", err)
	}
}

func TestLoadConfigAcceptsGlobalAndPeerTransferRateLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"transfer":{"sendBytesPerSecond":1048576,"receiveBytesPerSecond":2097152},
		"peers":[{"id":"node-b","sendBytesPerSecond":524288,"receiveBytesPerSecond":262144}],
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Transfer.SendBytesPerSecond != 1048576 || cfg.Transfer.ReceiveBytesPerSecond != 2097152 {
		t.Fatalf("global transfer limits not preserved: %+v", cfg.Transfer)
	}
	if cfg.Peers[0].SendBytesPerSecond != 524288 || cfg.Peers[0].ReceiveBytesPerSecond != 262144 {
		t.Fatalf("peer transfer limits not preserved: %+v", cfg.Peers[0])
	}
}

func TestLoadConfigRejectsNegativeTransferRateLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"transfer":{"sendBytesPerSecond":-1},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected negative transfer rate limit to be rejected")
	}
}

func TestLoadConfigAcceptsBackupDestinationModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"backup":{"enabled":true,"mode":"mirror-plus-archive","mirrorPath":"./backup-mirror","archivePath":"./block-archive","checkpointPath":"./offline-db-checkpoints"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.Backup.Enabled || cfg.Backup.Mode != BackupModeMirrorPlusArchive || cfg.Backup.MirrorPath != "./backup-mirror" || cfg.Backup.ArchivePath != "./block-archive" || cfg.Backup.CheckpointPath != "./offline-db-checkpoints" {
		t.Fatalf("backup destination mode not preserved: %+v", cfg.Backup)
	}
}

func TestLoadConfigRejectsInvalidBackupDestinationMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"backup":{"enabled":true,"mode":"full-copy"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid backup mode to be rejected")
	}
}

func TestLoadConfigRejectsInvalidLoggingLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"logging":{"level":"chatty"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid logging level to be rejected")
	}
}

func TestLoadConfigAcceptsMaintenanceBudgetsPerFolder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"maintenance":{"enabled":true,"frequency":"6h","idleOnly":true,"maxFilesPerRun":10,"maxBytesPerRun":2048,"maxFilesPerDay":100,"maxBytesPerDay":4096},
		"folders":[{
			"id":"docs",
			"path":"/tmp/docs",
			"maintenance":{"enabled":true,"frequency":"1h30m","idleOnly":false,"maxFilesPerRun":3,"maxBytesPerRun":512,"maxFilesPerDay":30,"maxBytesPerDay":2048}
		}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.Maintenance.Enabled || cfg.Maintenance.Frequency != "6h" || !cfg.Maintenance.IdleOnly {
		t.Fatalf("global maintenance config not preserved: %+v", cfg.Maintenance)
	}
	folderMaintenance := cfg.Folders[0].Maintenance
	if folderMaintenance.Frequency != "1h30m" || folderMaintenance.MaxFilesPerRun != 3 || folderMaintenance.MaxBytesPerRun != 512 || folderMaintenance.MaxFilesPerDay != 30 || folderMaintenance.MaxBytesPerDay != 2048 {
		t.Fatalf("folder maintenance budgets not preserved: %+v", folderMaintenance)
	}
}

func TestLoadConfigDefaultsMaintenanceAutoRepairDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"maintenance":{"enabled":true},
		"folders":[{"id":"docs","path":"/tmp/docs","maintenance":{"enabled":true}}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Maintenance.AutoRepair {
		t.Fatalf("global maintenance auto repair defaulted enabled: %+v", cfg.Maintenance)
	}
	if cfg.Folders[0].Maintenance.AutoRepair {
		t.Fatalf("folder maintenance auto repair defaulted enabled: %+v", cfg.Folders[0].Maintenance)
	}
}

func TestLoadConfigAcceptsMaintenanceScrubModeAndSampling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"maintenance":{"enabled":true,"scrubMode":"sampled-blocks","sampleEveryNBlocks":16},
		"folders":[{"id":"docs","path":"/tmp/docs","maintenance":{"enabled":true,"scrubMode":"light-metadata"}}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Maintenance.ScrubMode != MaintenanceScrubSampledBlocks || cfg.Maintenance.SampleEveryNBlocks != 16 {
		t.Fatalf("global maintenance scrub mode not preserved: %+v", cfg.Maintenance)
	}
	if cfg.Folders[0].Maintenance.ScrubMode != MaintenanceScrubLightMetadata {
		t.Fatalf("folder maintenance scrub mode not preserved: %+v", cfg.Folders[0].Maintenance)
	}
}

func TestLoadConfigRejectsInvalidMaintenanceScrubMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"maintenance":{"scrubMode":"chaos","sampleEveryNBlocks":0},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid maintenance scrub mode and sampling to be rejected")
	}
}

func TestLoadConfigRejectsUnknownMetadataStoreBackend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"metadata":{"backend":"sqlite"},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected unknown metadata backend to be rejected")
	}
}

func TestLoadConfigRejectsInvalidMaintenanceBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"maintenance":{"frequency":"later","maxFilesPerRun":-1},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid maintenance budget to be rejected")
	}
}

func TestLoadConfigAcceptsPublicDHTBootstrapServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"dht":true,"local":false,"dhtNamespace":"fse-prod","dhtBootstrapPeers":["/dnsaddr/bootstrap.libp2p.io"]},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Discovery.DHTNamespace != "fse-prod" {
		t.Fatalf("DHTNamespace = %q, want fse-prod", cfg.Discovery.DHTNamespace)
	}
	if len(cfg.Discovery.DHTBootstrapPeers) != 1 || cfg.Discovery.DHTBootstrapPeers[0] != "/dnsaddr/bootstrap.libp2p.io" {
		t.Fatalf("DHTBootstrapPeers not preserved: %+v", cfg.Discovery.DHTBootstrapPeers)
	}
}

func TestLoadConfigRejectsEmptyPublicDHTBootstrapPeer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"dht":true,"dhtBootstrapPeers":[""]},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected empty DHT bootstrap peer to be rejected")
	}
}

func TestLoadConfigAcceptsDiscoveryDisabledSwitchForManualOnlyMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[{"id":"peer-b","addresses":["/ip4/127.0.0.1/tcp/22001/p2p/peer-b"]}],
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.Discovery.Disabled || !cfg.Discovery.AllDisabled() || len(cfg.Peers) != 1 {
		t.Fatalf("manual-only discovery config not preserved: discovery=%+v peers=%+v", cfg.Discovery, cfg.Peers)
	}
}

func TestLoadConfigRejectsDisabledDiscoveryWithActiveDiscoveryModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"discovery":{"disabled":true,"dht":true,"local":false},
		"folders":[{"id":"docs","path":"/tmp/docs"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil {
		t.Fatalf("expected disabled discovery with dht/local enabled to be rejected")
	}
}

func TestLoadConfigAcceptsPeerIdentityAndEncryptionLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"identity":{"privateKey":"dev-private-key","publicKey":"dev-public-key","encryptionLevel":5,"groups":[{"id":"family-sync","token":"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefghij","enabled":true}],"revoked":[{"groupId":"family-sync","discoveryId":"remote-public-key","bootstrapProofKeyHash":"hash-record","revokedAt":"2026-05-27T00:00:00Z"}]},
		"peers":[{"id":"node-b","identityPublicKey":"peer-public-key","encryptionLevel":4}],
		"folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if cfg.Identity.EncryptionLevel != 5 || cfg.Identity.PublicKey != "dev-public-key" {
		t.Fatalf("identity config not preserved: %+v", cfg.Identity)
	}
	if len(cfg.Identity.Groups) != 1 || cfg.Identity.Groups[0].ID != "family-sync" || !cfg.Identity.Groups[0].Enabled {
		t.Fatalf("identity group config not preserved: %+v", cfg.Identity.Groups)
	}
	if len(cfg.Identity.Revoked) != 1 || cfg.Identity.Revoked[0].GroupID != "family-sync" || cfg.Identity.Revoked[0].DiscoveryID != "remote-public-key" || cfg.Identity.Revoked[0].BootstrapProofKeyHash != "hash-record" {
		t.Fatalf("revoked identity config not preserved: %+v", cfg.Identity.Revoked)
	}
	if cfg.Peers[0].EncryptionLevel != 4 || cfg.Peers[0].IdentityPublicKey != "peer-public-key" {
		t.Fatalf("peer identity config not preserved: %+v", cfg.Peers[0])
	}
}

func TestLoadConfigAcceptsDisabledAdvertisedFolderWithoutPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"folders":[{"id":"photos","enabled":false,"advertisedBy":"node-c","identityGroup":"family-sync","mode":"recvonly"}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	folder := cfg.Folders[0]
	if folder.Enabled || folder.Path != "" || folder.AdvertisedBy != "node-c" || folder.IdentityGroup != "family-sync" {
		t.Fatalf("disabled learned folder not preserved: %+v", folder)
	}
}

func TestLoadConfigRejectsPeerEncryptionLevelOutsideRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{"nodeName":"node-a","identity":{"encryptionLevel":11},"folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected encryption level outside 0-10 to be rejected")
	}
}

func TestLoadConfigAcceptsFolderPermissionPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"folders":[{
			"id":"shared",
			"path":"./shared",
			"mode":"sendrecv",
			"permissions":{
				"mode":"fixed",
				"fileMode":"0666",
				"dirMode":"0777",
				"preserveOwner":false,
				"preserveGroup":true,
				"preserveACL":false
			}
		}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	perms := cfg.Folders[0].Permissions
	if perms.Mode != PermissionFixed {
		t.Fatalf("permission mode = %q, want %q", perms.Mode, PermissionFixed)
	}
	if perms.FileMode != "0666" || perms.DirMode != "0777" {
		t.Fatalf("permission modes not preserved: %+v", perms)
	}
	if !perms.PreserveGroup || perms.PreserveOwner || perms.PreserveACL {
		t.Fatalf("permission preserve flags not preserved: %+v", perms)
	}
}

func TestLoadConfigDefaultsPermissionPolicyToIgnore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if cfg.Folders[0].Permissions.Mode != PermissionIgnore {
		t.Fatalf("default permission mode = %q, want %q", cfg.Folders[0].Permissions.Mode, PermissionIgnore)
	}
}

func TestLoadConfigRejectsInvalidPermissionPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"folders":[{
			"id":"docs",
			"path":"./docs",
			"mode":"sendrecv",
			"permissions":{"mode":"fixed","fileMode":"bad","dirMode":"0777"}
		}]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(path); err == nil {
		t.Fatalf("expected invalid permission fileMode to be rejected")
	}
}

func TestLoadConfigAcceptsWebGUIHTTPSListenerWithAutoTLS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	json := `{
		"nodeName":"node-a",
		"webGUI":{
			"enabled":true,
			"version":"container-default",
			"packagePath":"/opt/fse/web/fse-web-container-default.zip",
			"installDir":"/config/web/current",
			"listen":"0.0.0.0:8385",
			"httpsListen":"0.0.0.0:8943",
			"checksumSHA256":"9f65e8d0ad7bff683a81a9ca081fd8aae53ed43df896b65f1b9c6fd56e0610ab"
		},
		"folders":[]
	}`
	if err := os.WriteFile(path, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if cfg.WebGUI.Listen != "0.0.0.0:8385" || cfg.WebGUI.HTTPSListen != "0.0.0.0:8943" {
		t.Fatalf("web GUI listeners not preserved: %+v", cfg.WebGUI)
	}
}

func TestManagerReloadIfChangedKeepsLastGoodConfigOnInvalidUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	good := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]}`
	bad := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"bogus"}]}`
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.Current().Folders[0].Mode != ModeSendReceive {
		t.Fatalf("initial mode wrong")
	}
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := mgr.ReloadIfChanged()
	if err == nil {
		t.Fatalf("expected invalid reload error")
	}
	if changed {
		t.Fatalf("invalid config must not be applied")
	}
	if mgr.Current().Folders[0].Mode != ModeSendReceive {
		t.Fatalf("last good config was not preserved")
	}
}
