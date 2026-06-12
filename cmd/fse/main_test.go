package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/apicontrol"
	"filesyncengine/internal/block"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/daemonlogging"
	"filesyncengine/internal/daemonmonitor"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/discoverycontrol"
	"filesyncengine/internal/engine"
	"filesyncengine/internal/foldersync"
	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/metadatareconcile"
	"filesyncengine/internal/monitor"
	"filesyncengine/internal/pairing"
	"filesyncengine/internal/peersync"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/state"
	"filesyncengine/internal/streamsync"
	"filesyncengine/internal/structuredlog"
	"filesyncengine/internal/transfercontrol"
	"filesyncengine/internal/webgui"
)

func TestWebGUICommandAPIResponseInstallsTrustedLocalPackage(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "fse-web.zip")
	checksum := writeMainTestZipPackage(t, pkgPath, map[string]string{"index.html": "web", "VERSION": "1.2.3"})
	installDir := filepath.Join(dir, "web", "current")
	cfg := config.Config{WebGUI: config.WebGUIConfig{Enabled: true, Version: "1.2.3", PackagePath: pkgPath, InstallDir: installDir, ChecksumSHA256: checksum}}

	response, err := webGUICommandAPIResponse(cfg, api.WebGUICommandRequest{Action: "install"})
	if err != nil {
		t.Fatalf("webGUICommandAPIResponse: %v", err)
	}
	if response.Status != "installed" || response.Version != "1.2.3" || response.InstallDir != installDir {
		t.Fatalf("unexpected response: %+v", response)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "index.html")); err != nil || string(got) != "web" {
		t.Fatalf("installed package mismatch: %q err=%v", string(got), err)
	}

	status, err := webGUICommandAPIResponse(cfg, api.WebGUICommandRequest{Action: "status"})
	if err != nil {
		t.Fatalf("web GUI status: %v", err)
	}
	if status.Status != "installed" || status.Version != "1.2.3" {
		t.Fatalf("unexpected status response: %+v", status)
	}
}

func TestWebGUICommandAPIResponseInstallsTrustedHTTPSPackage(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "fse-web-remote.zip")
	checksum := writeMainTestZipPackage(t, pkgPath, map[string]string{"index.html": "remote web", "VERSION": "2.0.0"})
	pkgBytes, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pkgBytes)
	}))
	defer server.Close()
	installDir := filepath.Join(dir, "web", "current")
	cfg := config.Config{WebGUI: config.WebGUIConfig{Enabled: true, Version: "2.0.0", UpdateURL: server.URL + "/fse-web.zip", InstallDir: installDir, ChecksumSHA256: checksum}}

	response, err := webGUICommandAPIResponseWithHTTPClient(cfg, api.WebGUICommandRequest{Action: "update"}, server.Client())
	if err != nil {
		t.Fatalf("webGUICommandAPIResponseWithHTTPClient: %v", err)
	}
	if response.Status != "installed" || response.Version != "2.0.0" || response.InstallDir != installDir {
		t.Fatalf("unexpected response: %+v", response)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "index.html")); err != nil || string(got) != "remote web" {
		t.Fatalf("installed HTTPS package mismatch: %q err=%v", string(got), err)
	}
}

func TestWebGUICommandAPIResponseReportsDisabledHeadlessStatusWithoutInstallDir(t *testing.T) {
	cfg := config.Config{WebGUI: config.WebGUIConfig{Enabled: false}}
	manager := webgui.NewServer()

	response, err := webGUICommandAPIResponseWithManager(cfg, api.WebGUICommandRequest{Action: "status"}, manager, nil)
	if err != nil {
		t.Fatalf("status disabled web GUI: %v", err)
	}
	if response.Status != "disabled" || response.Running || response.Message != "web GUI is disabled; core daemon is running headless" {
		t.Fatalf("disabled headless status not explicit: %+v", response)
	}
	if response.InstallDir != "" || response.Version != "" || response.URL != "" {
		t.Fatalf("disabled headless status should not report package/runtime details: %+v", response)
	}

	if _, err := webGUICommandAPIResponseWithManager(cfg, api.WebGUICommandRequest{Action: "start"}, manager, nil); err == nil {
		t.Fatalf("disabled web GUI start should be rejected while headless core remains usable")
	}
	stopped, err := webGUICommandAPIResponseWithManager(cfg, api.WebGUICommandRequest{Action: "stop"}, manager, nil)
	if err != nil {
		t.Fatalf("stop on disabled/non-running web GUI should remain harmless: %v", err)
	}
	if stopped.Running || stopped.Status != "stopped" {
		t.Fatalf("unexpected disabled stop status: %+v", stopped)
	}
}

func TestWebGUICommandAPIResponseStartsStopsManagedServer(t *testing.T) {
	dir := t.TempDir()
	installDir := filepath.Join(dir, "web", "current")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, ".fse-web-version"), []byte("3.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{WebGUI: config.WebGUIConfig{Enabled: true, Version: "3.1.0", InstallDir: installDir, Listen: "127.0.0.1:0"}}
	manager := webgui.NewServer()

	started, err := webGUICommandAPIResponseWithManager(cfg, api.WebGUICommandRequest{Action: "start"}, manager, nil)
	if err != nil {
		t.Fatalf("start web GUI: %v", err)
	}
	if !started.Running || started.Status != "running" || started.URL == "" {
		t.Fatalf("unexpected started response: %+v", started)
	}
	resp, err := http.Get(started.URL + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", resp.StatusCode)
	}
	status, err := webGUICommandAPIResponseWithManager(cfg, api.WebGUICommandRequest{Action: "status"}, manager, nil)
	if err != nil {
		t.Fatalf("status web GUI: %v", err)
	}
	if !status.Running || status.URL != started.URL {
		t.Fatalf("unexpected running status: %+v", status)
	}
	stopped, err := webGUICommandAPIResponseWithManager(cfg, api.WebGUICommandRequest{Action: "stop"}, manager, nil)
	if err != nil {
		t.Fatalf("stop web GUI: %v", err)
	}
	if stopped.Running || stopped.Status != "stopped" {
		t.Fatalf("unexpected stopped response: %+v", stopped)
	}
}

func TestIdentityPackageAPIResponseBuildsPackageFromCurrentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	groupToken := strings.Repeat("z", 80)
	if err := os.WriteFile(configPath, []byte(`{
		"nodeName":"node-a",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret"},
		"identity":{"privateKey":"private-secret","publicKey":"public-discovery-key","encryptionLevel":4,"groups":[{"id":"family-sync","token":"`+groupToken+`","enabled":true}]},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	pkg, err := identityPackageAPIResponse(configPath, api.IdentityPackageRequest{GroupID: "family-sync"})
	if err != nil {
		t.Fatalf("identityPackageAPIResponse returned error: %v", err)
	}
	if pkg.DiscoveryID != "public-discovery-key" || pkg.GroupID != "family-sync" || pkg.BootstrapProofKey != groupToken {
		t.Fatalf("unexpected identity package: %+v", pkg)
	}
	if pkg.BootstrapEncryptionLevel != 10 || pkg.DefaultPeerEncryptionLevel != 4 {
		t.Fatalf("unexpected identity package levels: %+v", pkg)
	}
}

func TestMeshSettingsAPIResponseFiltersRequestedNodeAndRedactsSecrets(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveNodeSettingsDocument("node-b", state.NodeSettingsDocument{
		NodeID:   "node-b",
		Revision: 7,
		Settings: map[string]any{
			"logging.level": "warn",
			"apiKey":        "secret-api-key",
			"nested": map[string]any{
				"privateToken": "nested-secret",
				"theme":        "dark",
			},
		},
		Source:      "identity-mesh-cache",
		ApplyStatus: "cached-read-only",
	}); err != nil {
		t.Fatalf("save node-b settings: %v", err)
	}
	if err := store.SaveNodeSettingsDocument("node-a", state.NodeSettingsDocument{NodeID: "node-a", Revision: 1, Settings: map[string]any{"logging.level": "info"}}); err != nil {
		t.Fatalf("save node-a settings: %v", err)
	}

	response, err := meshSettingsAPIResponse(store, api.MeshSettingsRequest{NodeID: "node-b"})
	if err != nil {
		t.Fatalf("meshSettingsAPIResponse: %v", err)
	}
	if len(response.Documents) != 1 || response.Documents[0].NodeID != "node-b" {
		t.Fatalf("unexpected filtered documents: %+v", response.Documents)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	body := string(encoded)
	for _, leaked := range []string{"secret-api-key", "nested-secret", "apiKey", "privateToken"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("mesh settings response leaked secret-looking value %q: %s", leaked, body)
		}
	}
	for _, want := range []string{"logging.level", "warn", "theme", "dark"} {
		if !strings.Contains(body, want) {
			t.Fatalf("mesh settings response missing non-secret value %q: %s", want, body)
		}
	}
}

func TestMeshSettingsCommandAPIResponsePersistsPendingRemoteChange(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	response, err := meshSettingsCommandAPIResponse(store, "node-a", api.MeshSettingsCommandRequest{
		Action:         "queue",
		TargetNodeID:   "node-b",
		OriginNodeID:   "node-a",
		IdempotencyKey: "node-a:node-b:settings-1",
		SettingsPatch: map[string]any{
			"logging.level":                  "warn",
			"transfer.receiveBytesPerSecond": float64(2048),
		},
	})
	if err != nil {
		t.Fatalf("meshSettingsCommandAPIResponse: %v", err)
	}
	if response.Status != "queued" || response.TargetNodeID != "node-b" || response.OriginNodeID != "node-a" || response.ChangeID == "" {
		t.Fatalf("unexpected command response: %+v", response)
	}
	changes, err := store.ListPendingSettingsChanges("node-b")
	if err != nil {
		t.Fatalf("ListPendingSettingsChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one pending settings change, got %+v", changes)
	}
	change := changes[0]
	if change.ID != response.ChangeID || change.Status != "pending" || change.IdempotencyKey != "node-a:node-b:settings-1" || change.SettingsPatch["logging.level"] != "warn" {
		t.Fatalf("unexpected persisted change: %+v", change)
	}
}

func TestMeshSettingsCommandAPIResponseRejectsSpoofedOriginNode(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	_, err := meshSettingsCommandAPIResponse(store, "node-a", api.MeshSettingsCommandRequest{
		Action:         "queue",
		TargetNodeID:   "node-b",
		OriginNodeID:   "node-c",
		IdempotencyKey: "node-c:node-b:settings-1",
		SettingsPatch:  map[string]any{"logging.level": "warn"},
	})
	if err == nil || !strings.Contains(err.Error(), "originNodeId must match authenticated local node") {
		t.Fatalf("expected spoofed origin rejection, got %v", err)
	}
	changes, err := store.ListPendingSettingsChanges("node-b")
	if err != nil {
		t.Fatalf("ListPendingSettingsChanges: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("spoofed origin queued changes: %+v", changes)
	}
}

func TestIdentityImportAPIResponseValidatesPackageAndCreatesRedactedPeerPairMaterial(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	groupToken := strings.Repeat("z", 80)
	if err := os.WriteFile(configPath, []byte(`{
		"nodeName":"node-b",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret"},
		"identity":{"privateKey":"private-secret","publicKey":"local-public","encryptionLevel":4,"groups":[{"id":"family-sync","token":"`+groupToken+`","enabled":true}]},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	response, err := identityImportAPIResponse(configPath, api.IdentityImportRequest{Package: pairing.IdentityPackage{
		Version:                    pairing.IdentityPackageVersion,
		DiscoveryID:                "remote-public",
		GroupID:                    "family-sync",
		BootstrapProofKey:          groupToken,
		BootstrapEncryptionLevel:   10,
		DefaultPeerEncryptionLevel: 4,
	}})
	if err != nil {
		t.Fatalf("identityImportAPIResponse returned error: %v", err)
	}
	if response.Status != "accepted" || response.GroupID != "family-sync" || response.RemoteDiscoveryID != "remote-public" {
		t.Fatalf("unexpected identity import response: %+v", response)
	}
	if response.IntroductionEncryptionLevel != 10 || response.PeerPairEncryptionLevel != 4 || !response.RequiresDedicatedPeerPairKey || response.UsesBootstrapKeyForTraffic {
		t.Fatalf("unexpected identity import levels/safety flags: %+v", response)
	}
	if response.PairID == "" || response.KeyID == "" {
		t.Fatalf("identity import should create redacted peer-pair key identifiers: %+v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	for _, leaked := range []string{groupToken, "bootstrapProofKey", "secretKey", "private-secret", "api-secret"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("identity import response leaked secret %q: %s", leaked, encoded)
		}
	}
}

func TestDiscoveryCommandAPIResponseUpdatesDiscoveryConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if _, _, err := config.EnsureFile(configPath); err != nil {
		t.Fatal(err)
	}
	response, err := discoveryCommandAPIResponse(configPath, api.DiscoveryCommandRequest{
		Action:            "update",
		Disabled:          true,
		DHT:               false,
		Local:             false,
		DHTNamespace:      "fse-test",
		DHTBootstrapPeers: []string{"/dnsaddr/bootstrap.libp2p.io"},
		NetworkHints:      config.NetworkHintsConfig{LocalContainerGatewayIPs: []string{"172.17.0.1"}},
	})
	if err != nil {
		t.Fatalf("discoveryCommandAPIResponse: %v", err)
	}
	if response.Status != "accepted" || response.Action != "update" {
		t.Fatalf("unexpected response: %+v", response)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Discovery.Disabled || cfg.Discovery.DHT || cfg.Discovery.Local || cfg.Discovery.DHTNamespace != "fse-test" || len(cfg.Discovery.DHTBootstrapPeers) != 1 || len(cfg.Discovery.NetworkHints.LocalContainerGatewayIPs) != 1 || cfg.Discovery.NetworkHints.LocalContainerGatewayIPs[0] != "172.17.0.1" {
		t.Fatalf("discovery not persisted: %+v", cfg.Discovery)
	}
}

func TestDaemonAPIRequestOptionsUseHTTPSTrustedConfiguredCertificate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		API: config.APIConfig{
			Listen: "0.0.0.0:22420",
			Key:    "secret-key",
			Encryption: config.APIEncryptionConfig{
				Mode: config.APIEncryptionAuto,
			},
		},
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		t.Fatalf("ensure API TLS assets: %v", err)
	}

	url, client, err := daemonAPIRequestOptions(cfg, http.MethodGet, "/v1/status", nil)
	if err != nil {
		t.Fatalf("daemonAPIRequestOptions: %v", err)
	}
	if url != "https://0.0.0.0:22420/v1/status" {
		t.Fatalf("url = %q, want HTTPS daemon URL", url)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatalf("HTTPS API client did not configure a trusted certificate pool: %#v", client.Transport)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("HTTPS API client must trust configured certs, not disable verification")
	}
	if got := len(transport.TLSClientConfig.RootCAs.Subjects()); got == 0 {
		t.Fatalf("HTTPS API client root pool has no configured certificate subjects")
	}
}

func TestDaemonAPITLSClientConfigPinsConfiguredCertificateFingerprint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		API: config.APIConfig{
			Listen: "0.0.0.0:22420",
			Key:    "secret-key",
			Encryption: config.APIEncryptionConfig{
				Mode: config.APIEncryptionAuto,
			},
		},
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		t.Fatalf("ensure API TLS assets: %v", err)
	}
	certPEM, err := os.ReadFile(cfg.API.Encryption.CertFile)
	if err != nil {
		t.Fatalf("read generated certificate: %v", err)
	}
	fingerprint, err := apiCertificateFingerprintSHA256(certPEM)
	if err != nil {
		t.Fatalf("fingerprint generated certificate: %v", err)
	}
	cfg.API.Encryption.TrustedCertificateSHA256 = fingerprint

	tlsConfig, err := daemonAPITLSClientConfig(cfg)
	if err != nil {
		t.Fatalf("daemonAPITLSClientConfig: %v", err)
	}
	if tlsConfig.VerifyPeerCertificate == nil {
		t.Fatalf("expected API TLS client to pin configured certificate fingerprint")
	}
	if err := tlsConfig.VerifyPeerCertificate([][]byte{[]byte("not the certificate")}, nil); err == nil || !strings.Contains(err.Error(), "api TLS certificate fingerprint mismatch") {
		t.Fatalf("wrong certificate was not rejected with fingerprint mismatch: %v", err)
	}
}

func TestAPITrustAPIResponseReportsPinnedCertificateStatus(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		NodeName: "node-a",
		API: config.APIConfig{
			Listen: "0.0.0.0:22420",
			Key:    "secret-key",
			Encryption: config.APIEncryptionConfig{
				Mode: config.APIEncryptionAuto,
			},
		},
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		t.Fatalf("ensure API TLS assets: %v", err)
	}
	certPEM, err := os.ReadFile(cfg.API.Encryption.CertFile)
	if err != nil {
		t.Fatalf("read generated certificate: %v", err)
	}
	fingerprint, err := apiCertificateFingerprintSHA256(certPEM)
	if err != nil {
		t.Fatalf("fingerprint generated certificate: %v", err)
	}
	cfg.API.Encryption.TrustedCertificateSHA256 = fingerprint
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	response, err := apiTrustAPIResponse(configPath)
	if err != nil {
		t.Fatalf("apiTrustAPIResponse: %v", err)
	}
	if response.Mode != string(config.APIEncryptionAuto) || !response.TLSEnabled || !response.TLSRequired {
		t.Fatalf("unexpected trust transport status: %+v", response)
	}
	if response.CertificateSHA256 != fingerprint || response.TrustedCertificateSHA256 != fingerprint || !response.TrustedCertificateConfigured || !response.TrustedCertificateMatches {
		t.Fatalf("unexpected certificate pinning status: %+v", response)
	}
}

func TestAPITrustCommandAPIResponsePinsActiveCertificateFingerprint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{
		NodeName: "node-a",
		API: config.APIConfig{
			Listen: "0.0.0.0:22420",
			Key:    "secret-key",
			Encryption: config.APIEncryptionConfig{
				Mode: config.APIEncryptionAuto,
			},
		},
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		t.Fatalf("ensure API TLS assets: %v", err)
	}
	certPEM, err := os.ReadFile(cfg.API.Encryption.CertFile)
	if err != nil {
		t.Fatalf("read generated certificate: %v", err)
	}
	fingerprint, err := apiCertificateFingerprintSHA256(certPEM)
	if err != nil {
		t.Fatalf("fingerprint generated certificate: %v", err)
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	response, err := apiTrustCommandAPIResponse(configPath, api.APITrustCommandRequest{Action: "pin-active-certificate"})
	if err != nil {
		t.Fatalf("apiTrustCommandAPIResponse: %v", err)
	}
	if response.Action != "pin-active-certificate" || response.Status != "accepted" || response.CertificateSHA256 != fingerprint || !response.TrustedCertificateConfigured || !response.TrustedCertificateMatches {
		t.Fatalf("unexpected trust command response: %+v", response)
	}
	updated, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	if updated.API.Encryption.TrustedCertificateSHA256 != fingerprint {
		t.Fatalf("active API certificate fingerprint was not pinned: %+v", updated.API.Encryption)
	}
	if updated.API.Key != "secret-key" {
		t.Fatalf("API key was not preserved")
	}
}

func TestConfigUpdateAPIResponsePatchesOnlyNonSecretSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"nodeName":"node-a",
		"listen":["tcp://127.0.0.1:22000"],
		"api":{"listen":"127.0.0.1:0","key":"secret-key"},
		"identity":{"privateKey":"identity-secret","publicKey":"identity-public","encryptionLevel":4},
		"peers":[{"id":"peer-a","apiKey":"peer-secret","endpoints":[{"kind":"manual","address":"http://127.0.0.1:22001"}]}],
		"folders":[]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	listen := "127.0.0.1:9090"
	name := "node-b"
	response, err := configUpdateAPIResponse(configPath, api.ConfigUpdateRequest{
		NodeName: &name,
		API:      &api.ConfigAPIUpdate{Listen: &listen},
		Logging:  &config.LoggingConfig{Level: config.LogLevelWarn, Output: "fse.log"},
		Transfer: &config.TransferConfig{SendBytesPerSecond: 1024, ReceiveBytesPerSecond: 2048},
	})
	if err != nil {
		t.Fatalf("configUpdateAPIResponse: %v", err)
	}
	if response.Status != "accepted" {
		t.Fatalf("unexpected response: %+v", response)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeName != "node-b" || cfg.API.Listen != listen || cfg.Logging.Level != config.LogLevelWarn || cfg.Transfer.ReceiveBytesPerSecond != 2048 {
		t.Fatalf("non-secret settings not patched: %+v", cfg)
	}
	if cfg.API.Key != "secret-key" || cfg.Identity.PrivateKey != "identity-secret" || len(cfg.Peers) != 1 || cfg.Peers[0].APIKey != "peer-secret" {
		t.Fatalf("secret settings were not preserved: %+v", cfg)
	}
}

func TestConfigUpdateAPIResponseMirrorsLocalSettingsDocument(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"nodeName":"node-a",
		"api":{"listen":"127.0.0.1:0","key":"secret-key"},
		"identity":{"privateKey":"identity-secret","publicKey":"identity-public","encryptionLevel":4},
		"folders":[]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	name := "node-b"
	listen := "127.0.0.1:9090"

	response, err := configUpdateAPIResponseWithStore(configPath, store, api.ConfigUpdateRequest{
		NodeName: &name,
		API:      &api.ConfigAPIUpdate{Listen: &listen},
		Logging:  &config.LoggingConfig{Level: config.LogLevelWarn, Output: "fse.log"},
		Transfer: &config.TransferConfig{SendBytesPerSecond: 1024, ReceiveBytesPerSecond: 2048},
	})
	if err != nil {
		t.Fatalf("configUpdateAPIResponseWithStore: %v", err)
	}
	if response.Status != "accepted" {
		t.Fatalf("unexpected response: %+v", response)
	}
	doc, ok, err := store.LoadNodeSettingsDocument("node-b")
	if err != nil || !ok {
		t.Fatalf("local settings document missing after config update: ok=%v err=%v", ok, err)
	}
	if doc.Source != "local-config" || doc.Settings["nodeName"] != "node-b" || doc.Settings["api.listen"] != listen || doc.Settings["logging.level"] != string(config.LogLevelWarn) || doc.Settings["transfer.receiveBytesPerSecond"] != float64(2048) {
		t.Fatalf("local settings document did not mirror non-secret patch: %+v", doc)
	}
	if _, exists := doc.Settings["api.key"]; exists {
		t.Fatalf("local settings document must not mirror API secrets: %+v", doc.Settings)
	}
	if doc.Revision == 0 || doc.UpdatedAt == "" {
		t.Fatalf("local settings document missing revision/timestamp: %+v", doc)
	}
}

func TestConfigReadAPIResponseReturnsCurrentConfigForRedactedEndpoint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"nodeName":"node-a","api":{"listen":"127.0.0.1:0","key":"secret-key"},"folders":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configReadAPIResponse(configPath)
	if err != nil {
		t.Fatalf("configReadAPIResponse: %v", err)
	}
	if cfg.NodeName != "node-a" || cfg.API.Key != "secret-key" {
		t.Fatalf("unexpected config read response: %+v", cfg)
	}
}

func TestServiceCommandAPIResponseRendersReviewableHandoff(t *testing.T) {
	resp, err := serviceCommandAPIResponse(api.ServiceCommandRequest{Action: "restart", Platform: "systemd", ServiceName: "fse"})
	if err != nil {
		t.Fatalf("service command response: %v", err)
	}
	if resp.Action != "restart" || resp.Platform != "systemd" || resp.ServiceName != "fse" || resp.Status != "accepted" {
		t.Fatalf("unexpected service command response: %+v", resp)
	}
	for _, want := range []string{"systemctl status fse", "systemctl restart fse", "Review before running"} {
		if !strings.Contains(resp.Handoff, want) {
			t.Fatalf("service handoff missing %q:\n%s", want, resp.Handoff)
		}
	}
}

func TestTransferCommandAPIResponsePausesResumesAndCancelsRuntimeScope(t *testing.T) {
	control := transfercontrol.New()
	pause, err := apicontrol.HandleTransferCommand(control, api.TransferCommandRequest{Action: "pause", FolderID: "docs", PeerID: "peer-a"})
	if err != nil {
		t.Fatalf("pause transfer: %v", err)
	}
	if pause.Status != "accepted" || !control.IsPaused("docs", "peer-a") || control.IsPaused("docs", "peer-b") {
		t.Fatalf("unexpected pause response/control: %+v paused=%v other=%v", pause, control.IsPaused("docs", "peer-a"), control.IsPaused("docs", "peer-b"))
	}
	resume, err := apicontrol.HandleTransferCommand(control, api.TransferCommandRequest{Action: "resume", FolderID: "docs", PeerID: "peer-a"})
	if err != nil {
		t.Fatalf("resume transfer: %v", err)
	}
	if resume.Status != "accepted" || control.IsPaused("docs", "peer-a") {
		t.Fatalf("unexpected resume response/control: %+v paused=%v", resume, control.IsPaused("docs", "peer-a"))
	}
	cancel, err := apicontrol.HandleTransferCommand(control, api.TransferCommandRequest{Action: "cancel", FolderID: "docs", PeerID: "peer-a"})
	if err != nil {
		t.Fatalf("cancel transfer: %v", err)
	}
	if cancel.Status != "accepted" || !control.IsCancelled("docs", "peer-a") || control.IsCancelled("docs", "peer-b") {
		t.Fatalf("unexpected cancel response/control: %+v cancelled=%v other=%v", cancel, control.IsCancelled("docs", "peer-a"), control.IsCancelled("docs", "peer-b"))
	}
	control.ClearCancel("docs", "peer-a")
	if control.IsCancelled("docs", "peer-a") {
		t.Fatal("cancel scope should clear after the runtime observes it")
	}
	if _, err := apicontrol.HandleTransferCommand(control, api.TransferCommandRequest{Action: "cancel", PeerID: "peer-a"}); err != nil {
		t.Fatalf("peer-wide cancel transfer: %v", err)
	}
	if !control.IsCancelled("docs", "peer-a") {
		t.Fatal("peer-wide cancel should match folder peer pass")
	}
	control.ClearCancel("docs", "peer-a")
	if control.IsCancelled("docs", "peer-a") {
		t.Fatal("peer-wide cancel scope should clear after the matching runtime observes it")
	}
	if _, err := apicontrol.HandleTransferCommand(control, api.TransferCommandRequest{Action: "pause"}); err == nil {
		t.Fatal("pause without folder or peer scope should fail")
	}
}

func TestScanConfiguredUsesMetadataOnlyQuickIndex(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderPath, "seed.txt"), []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	scanConfigured(cli.Options{Command: cli.CommandScan}, configPath)

	manifest, ok, err := state.NewJSONStore(defaultStatePath(configPath)).LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("stored manifest missing: ok=%v err=%v", ok, err)
	}
	if manifest.HashState != "unknown" || len(manifest.Blocks) != 0 {
		t.Fatalf("scan command should quick-index metadata only: %+v", manifest)
	}
}

func TestScanConfiguredUsesConfiguredBadgerMetadataStore(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderPath, "seed.txt"), []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, "metadata.badger")
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"metadata":{"backend":"badger","path":%q},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, storePath, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	scanConfigured(cli.Options{Command: cli.CommandScan}, configPath)

	store, err := state.NewBadgerStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifest, ok, err := store.LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("badger stored manifest missing: ok=%v err=%v", ok, err)
	}
	if manifest.HashState != "unknown" || len(manifest.Blocks) != 0 {
		t.Fatalf("scan command should quick-index metadata only into badger: %+v", manifest)
	}
	if _, err := os.Stat(defaultStatePath(configPath)); !os.IsNotExist(err) {
		t.Fatalf("scan should not create default JSON state when badger backend is configured: %v", err)
	}
}

func TestScanConfiguredCanUsePerFolderBadgerMetadataStores(t *testing.T) {
	root := t.TempDir()
	docsPath := filepath.Join(root, "docs")
	mediaPath := filepath.Join(root, "media")
	if err := os.MkdirAll(docsPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mediaPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsPath, "doc.txt"), []byte("doc-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaPath, "song.txt"), []byte("song-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(root, "metadata")
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"metadata":{"backend":"badger","path":%q,"perFolder":true},
		"folders":[
			{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]},
			{"id":"media/lib","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}
		]
	}`, storeRoot, docsPath, mediaPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	scanConfigured(cli.Options{Command: cli.CommandScan}, configPath)

	docsStore, err := state.NewBadgerStore(filepath.Join(storeRoot, "docs.badger"))
	if err != nil {
		t.Fatal(err)
	}
	defer docsStore.Close()
	if _, ok, err := docsStore.LoadManifest("docs", "doc.txt"); err != nil || !ok {
		t.Fatalf("docs per-folder store missing manifest: ok=%v err=%v", ok, err)
	}
	mediaStore, err := state.NewBadgerStore(filepath.Join(storeRoot, "media_lib.badger"))
	if err != nil {
		t.Fatal(err)
	}
	defer mediaStore.Close()
	if _, ok, err := mediaStore.LoadManifest("media/lib", "song.txt"); err != nil || !ok {
		t.Fatalf("media per-folder store missing manifest: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(storeRoot, "config.json.state.badger")); !os.IsNotExist(err) {
		t.Fatalf("per-folder scan should not create aggregate badger store: %v", err)
	}
}

func TestAPIStateFromConfigReadsPerFolderBadgerMetadataStores(t *testing.T) {
	root := t.TempDir()
	docsPath := filepath.Join(root, "docs")
	mediaPath := filepath.Join(root, "media")
	if err := os.MkdirAll(docsPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mediaPath, 0o700); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(root, "metadata")
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"metadata":{"backend":"badger","path":%q,"perFolder":true},
		"folders":[
			{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]},
			{"id":"media/lib","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}
		]
	}`, storeRoot, docsPath, mediaPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	docsStore, err := state.NewBadgerStore(filepath.Join(storeRoot, "docs.badger"))
	if err != nil {
		t.Fatal(err)
	}
	if err := docsStore.SaveManifest("docs", "doc.txt", block.Manifest{Path: "doc.txt", Size: 3, HashState: "complete"}); err != nil {
		t.Fatal(err)
	}
	if err := docsStore.Close(); err != nil {
		t.Fatal(err)
	}
	mediaStore, err := state.NewBadgerStore(filepath.Join(storeRoot, "media_lib.badger"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mediaStore.SaveManifest("media/lib", "song.txt", block.Manifest{Path: "song.txt", Size: 4, HashState: "complete"}); err != nil {
		t.Fatal(err)
	}
	if err := mediaStore.Close(); err != nil {
		t.Fatal(err)
	}

	apiState := apiStateFromConfig(config.Config{
		NodeName: "test-node",
		API:      config.APIConfig{Listen: "127.0.0.1:0", Key: "test-key"},
		Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: storeRoot, PerFolder: true},
		Folders:  []config.FolderConfig{{ID: "docs", Path: docsPath, Mode: config.ModeSendReceive}, {ID: "media/lib", Path: mediaPath, Mode: config.ModeSendReceive}},
	}, configPath, 1, "running")

	if len(apiState.FoldersState) != 2 {
		t.Fatalf("expected two folder states, got %+v", apiState.FoldersState)
	}
	if apiState.FoldersState[0].Index.TotalFiles != 1 || apiState.FoldersState[1].Index.TotalFiles != 1 {
		t.Fatalf("expected per-folder index state from physical stores, got %+v", apiState.FoldersState)
	}
	if _, err := os.Stat(filepath.Join(storeRoot, "config.json.state.badger")); !os.IsNotExist(err) {
		t.Fatalf("API state should not create aggregate badger store for per-folder metadata: %v", err)
	}
}

func TestBackupScrubConfiguredReportsArchiveCheckpointAndRepairStatus(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	archiveRoot := filepath.Join(root, "archive")
	checkpointRoot := filepath.Join(root, "checkpoints")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	liveBytes := []byte("repairable")
	if err := os.WriteFile(filepath.Join(folderPath, "live.txt"), liveBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	liveHash := sha256.Sum256(liveBytes)
	missingBytes := []byte("missing")
	missingHash := sha256.Sum256(missingBytes)
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"block-archive-only","archivePath":%q,"checkpointPath":%q},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, archiveRoot, checkpointRoot, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	marker := state.SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: 1, StateHash: "hash", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveArchiveIntakeJobs(marker.ID, []state.ArchiveIntakeJob{
		{ID: "job-live", SnapshotID: marker.ID, FolderID: "docs", Path: "live.txt", Status: "archived", Block: block.Block{Offset: 0, Size: len(liveBytes), Hash: liveHash[:]}},
		{ID: "job-missing", SnapshotID: marker.ID, FolderID: "docs", Path: "missing.txt", Status: "archived", Block: block.Block{Offset: 0, Size: len(missingBytes), Hash: missingHash[:]}},
	}); err != nil {
		t.Fatal(err)
	}
	liveArchivePath := filepath.Join(archiveRoot, "blocks", hex.EncodeToString(liveHash[:])[:2], hex.EncodeToString(liveHash[:]))
	if err := os.MkdirAll(filepath.Dir(liveArchivePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveArchivePath, liveBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := backupScrubConfigured(cli.Options{Command: cli.CommandMaintenance, Action: cli.ActionBackupScrub}, configPath)
	if err != nil {
		t.Fatalf("backup scrub configured: %v", err)
	}
	if result.Archive.CheckedJobs != 2 || result.Archive.ProtectedBlocks != 1 || result.Archive.MissingBlocks != 1 {
		t.Fatalf("unexpected archive scrub result: %+v", result.Archive)
	}
	if result.Checkpoints.CheckedSnapshots != 1 || result.Checkpoints.MissingCheckpoints != 1 || result.Checkpoints.DegradedSnapshots != 1 {
		t.Fatalf("unexpected checkpoint scrub result: %+v", result.Checkpoints)
	}
	if result.RepairPlan.RepairableBlocks != 1 || result.RepairPlan.UnresolvedBlocks != 1 {
		t.Fatalf("unexpected repair plan result: %+v", result.RepairPlan)
	}
}

func TestSnapshotConfiguredCreatesAndUpdatesMarkers(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 5}); err != nil {
		t.Fatal(err)
	}

	created, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionCreate, ID: "docs", Mode: "before cleanup"}, configPath)
	if err != nil {
		t.Fatalf("snapshot create: %v", err)
	}
	if created.ID == "" || created.FolderID != "docs" || created.Cursor == 0 || created.StateHash == "" || created.Description != "before cleanup" {
		t.Fatalf("created snapshot marker missing root metadata: %+v", created)
	}
	if _, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionPin, ID: created.ID}, configPath); err != nil {
		t.Fatalf("snapshot pin: %v", err)
	}
	if _, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionDeprecate, ID: created.ID}, configPath); err != nil {
		t.Fatalf("snapshot deprecate: %v", err)
	}
	loaded, ok, err := store.LoadSnapshotMarker(created.ID)
	if err != nil || !ok || !loaded.Pinned || !loaded.Deprecated {
		t.Fatalf("snapshot marker updates not persisted: %+v ok=%v err=%v", loaded, ok, err)
	}
	listed, err := snapshotListConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionList, ID: "docs"}, configPath)
	if err != nil {
		t.Fatalf("snapshot list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("snapshot list did not find marker: %+v", listed)
	}
	if _, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionDelete, ID: created.ID}, configPath); err != nil {
		t.Fatalf("snapshot delete: %v", err)
	}
	if _, ok, err := store.LoadSnapshotMarker(created.ID); err != nil || ok {
		t.Fatalf("snapshot marker should be deleted: ok=%v err=%v", ok, err)
	}
}

func TestSnapshotRetentionConfiguredExecutesRetentionPlan(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"block-archive-only"},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 5}); err != nil {
		t.Fatal(err)
	}
	oldSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	oldMarker := state.SnapshotMarker{ID: "snap-old", FolderID: "docs", Cursor: oldSummary.Cursor, StateHash: oldSummary.StateHash, CreatedAt: "2026-05-24T10:00:00Z", Deprecated: true}
	if err := store.SaveSnapshotMarker(oldMarker); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "beta.txt", block.Manifest{Path: "beta.txt", Size: 4}); err != nil {
		t.Fatal(err)
	}
	newSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-new", FolderID: "docs", Cursor: newSummary.Cursor, StateHash: newSummary.StateHash, CreatedAt: "2026-05-24T11:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	result, err := snapshotRetentionConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionRetention, KeepLast: 1}, configPath)
	if err != nil {
		t.Fatalf("snapshot retention: %v", err)
	}
	if result.KeepLast != 1 || len(result.DeleteSnapshots) != 1 || result.DeleteSnapshots[0] != "snap-old" || len(result.Promotions) != 1 {
		t.Fatalf("unexpected retention result: %+v", result)
	}
	if _, ok, err := store.LoadSnapshotMarker("snap-old"); err != nil || ok {
		t.Fatalf("old marker should be deleted after retention: ok=%v err=%v", ok, err)
	}
}

func TestSnapshotRestorePlanConfiguredUsesArchiveAndConfigPaths(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"block-archive-only","archivePath":"archive"},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	payload := []byte("alpha")
	payloadHash := sha256.Sum256(payload)
	manifest := block.Manifest{Path: "dir/alpha.txt", Size: int64(len(payload)), BlockSize: 4096, HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: len(payload), Hash: payloadHash[:]}}}
	if err := store.SaveManifest("docs", "dir/alpha.txt", manifest); err != nil {
		t.Fatal(err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	marker := state.SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatal(err)
	}
	hexHash := hex.EncodeToString(payloadHash[:])
	archivePath := filepath.Join(archiveRoot, "blocks", hexHash[:2], hexHash)
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := snapshotRestorePlanConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionRestorePlan, ID: "snap-001", Paths: []string{"dir/alpha.txt"}, Destination: filepath.Join(root, "restore"), Path: "picked/alpha.txt"}, configPath)
	if err != nil {
		t.Fatalf("snapshot restore plan: %v", err)
	}
	if plan.SnapshotID != "snap-001" || plan.FolderID != "docs" || !plan.DryRun || plan.TotalFiles != 1 || plan.TotalBytes != int64(len(payload)) || plan.MissingBlocks != 0 {
		t.Fatalf("unexpected restore plan summary: %+v", plan)
	}
	if len(plan.Files) != 1 || !plan.Files[0].ArchiveAvailable || plan.Files[0].DestinationPath != filepath.Join(root, "restore", "picked", "alpha.txt") {
		t.Fatalf("unexpected restore plan file: %+v", plan.Files)
	}
}

func TestSnapshotRestoreAPIResponseExecutesRestore(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"block-archive-only","archivePath":"archive"},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	payload := []byte("alpha")
	payloadHash := sha256.Sum256(payload)
	manifest := block.Manifest{Path: "dir/alpha.txt", Size: int64(len(payload)), BlockSize: 4096, HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: len(payload), Hash: payloadHash[:]}}}
	if err := store.SaveManifest("docs", "dir/alpha.txt", manifest); err != nil {
		t.Fatal(err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	marker := state.SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatal(err)
	}
	hexHash := hex.EncodeToString(payloadHash[:])
	archivePath := filepath.Join(archiveRoot, "blocks", hexHash[:2], hexHash)
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	response, err := snapshotRestoreAPIResponse(api.RestoreRequest{SnapshotID: "snap-001", Paths: []string{"dir/alpha.txt"}, DestinationRoot: filepath.Join(root, "restore"), AlternatePath: "picked/alpha.txt"}, cfg, store, configPath)
	if err != nil {
		t.Fatalf("snapshot restore api response: %v", err)
	}
	if response.SnapshotID != "snap-001" || response.FolderID != "docs" || response.RestoredFiles != 1 || response.RestoredBytes != int64(len(payload)) {
		t.Fatalf("unexpected restore response: %+v", response)
	}
	restored, err := os.ReadFile(filepath.Join(root, "restore", "picked", "alpha.txt"))
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(restored) != string(payload) {
		t.Fatalf("restored content mismatch: %q", string(restored))
	}
}

func TestSnapshotConfiguredExecutesMirrorUpdateForMirrorBackupDestination(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	mirrorPath := filepath.Join(root, "mirror")
	if err := os.MkdirAll(filepath.Join(folderPath, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderPath, "docs", "current.txt"), []byte("current bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mirrorPath, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mirrorPath, "docs", "stale.txt"), []byte("stale bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"mirror-plus-archive","mirrorPath":%q},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, mirrorPath, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	manifest := block.Manifest{Path: "docs/current.txt", Size: 13, BlockSize: 13, HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: 13, Hash: []byte("current")}}}
	if err := store.SaveManifest("docs", "docs/current.txt", manifest); err != nil {
		t.Fatal(err)
	}

	if _, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionCreate, ID: "docs"}, configPath); err != nil {
		t.Fatalf("snapshot create: %v", err)
	}

	mirrored, err := os.ReadFile(filepath.Join(mirrorPath, "docs", "docs", "current.txt"))
	if err != nil {
		t.Fatalf("mirrored current file missing: %v", err)
	}
	if string(mirrored) != "current bytes" {
		t.Fatalf("mirrored content = %q", mirrored)
	}
	if _, err := os.Stat(filepath.Join(mirrorPath, "docs", "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale mirror file should be removed, err=%v", err)
	}
}

func TestSnapshotConfiguredPersistsArchiveIntakeJobsForBackupDestination(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"block-archive-only"},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	manifest := block.Manifest{Path: "alpha.txt", Size: 2, BlockSize: 1, HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: 1, Hash: []byte("a")}, {Index: 1, Offset: 1, Size: 1, Hash: []byte("b")}}}
	if err := store.SaveManifest("docs", "alpha.txt", manifest); err != nil {
		t.Fatal(err)
	}

	created, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionCreate, ID: "docs"}, configPath)
	if err != nil {
		t.Fatalf("snapshot create: %v", err)
	}

	jobs, err := store.ListArchiveIntakeJobs(created.ID)
	if err != nil {
		t.Fatalf("list archive intake jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected one persisted archive job per planned snapshot block, got %+v", jobs)
	}
	if jobs[0].SnapshotID != created.ID || jobs[0].FolderID != "docs" || jobs[0].Path != "alpha.txt" || string(jobs[0].Block.Hash) != "a" || jobs[0].Status != "pending" {
		t.Fatalf("archive job missing snapshot/block metadata: %+v", jobs[0])
	}
}

func TestSnapshotConfiguredArchivesSnapshotBlocksWhenArchivePathConfigured(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	archivePath := filepath.Join(root, "archive")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte("archive me")
	if err := os.WriteFile(filepath.Join(folderPath, "alpha.txt"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"block-archive-only","archivePath":%q},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, archivePath, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	manifest := block.Manifest{Path: "alpha.txt", Size: int64(len(data)), BlockSize: 4096, HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: len(data), Hash: hash[:]}}}
	if err := store.SaveManifest("docs", "alpha.txt", manifest); err != nil {
		t.Fatal(err)
	}

	created, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionCreate, ID: "docs"}, configPath)
	if err != nil {
		t.Fatalf("snapshot create: %v", err)
	}

	hexHash := hex.EncodeToString(hash[:])
	archived, err := os.ReadFile(filepath.Join(archivePath, "blocks", hexHash[:2], hexHash))
	if err != nil {
		t.Fatalf("archive block missing: %v", err)
	}
	if !bytes.Equal(archived, data) {
		t.Fatalf("archive block content mismatch: %q", archived)
	}
	jobs, err := store.ListArchiveIntakeJobs(created.ID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != "archived" {
		t.Fatalf("archive job should be marked archived after intake: %+v", jobs)
	}
}

func TestSnapshotConfiguredCreatesOfflineMetadataCheckpointForBackupDestination(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	checkpointPath := filepath.Join(root, "offline-db-checkpoints")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"backup":{"enabled":true,"mode":"block-archive-only","checkpointPath":%q},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, checkpointPath, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	manifest := block.Manifest{Path: "alpha.txt", Size: 2, BlockSize: 1, HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: 1, Hash: []byte("a")}}}
	if err := store.SaveManifest("docs", "alpha.txt", manifest); err != nil {
		t.Fatal(err)
	}

	created, err := snapshotConfigured(cli.Options{Command: cli.CommandSnapshot, Action: cli.ActionCreate, ID: "docs"}, configPath)
	if err != nil {
		t.Fatalf("snapshot create: %v", err)
	}

	checkpointFile := filepath.Join(checkpointPath, "docs", created.ID+".json")
	checkpoint, err := os.ReadFile(checkpointFile)
	if err != nil {
		t.Fatalf("offline checkpoint missing: %v", err)
	}
	if !bytes.Contains(checkpoint, []byte(created.ID)) || !bytes.Contains(checkpoint, []byte("alpha.txt")) {
		t.Fatalf("checkpoint does not contain snapshot metadata and manifests: %s", checkpoint)
	}
}

func TestImportJSONMetadataConfiguredCopiesStateIntoBadgerAndBacksUpExistingStore(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source-state.json")
	sourceStore := state.NewJSONStore(sourcePath)
	if err := sourceStore.SaveManifest("docs", "imported.txt", block.Manifest{Path: "imported.txt", Size: 8, BlockSize: 4, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{1, 2, 3}}}}); err != nil {
		t.Fatal(err)
	}

	storePath := filepath.Join(root, "metadata.badger")
	existingStore, err := state.NewBadgerStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := existingStore.SaveManifest("docs", "existing.txt", block.Manifest{Path: "existing.txt", Size: 3}); err != nil {
		t.Fatal(err)
	}
	if err := existingStore.Close(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"metadata":{"backend":"badger","path":%q},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, storePath, filepath.Join(root, "docs"))
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := importJSONMetadataConfigured(cli.Options{Command: cli.CommandMetadata, Action: cli.ActionImportJSON, Path: sourcePath}, configPath)
	if err != nil {
		t.Fatalf("importJSONMetadataConfigured: %v", err)
	}
	if result.ImportedManifests != 1 || result.BackupPath == "" {
		t.Fatalf("unexpected import result: %+v", result)
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("backup path missing: %v", err)
	}
	importedStore, err := state.NewBadgerStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer importedStore.Close()
	if _, ok, err := importedStore.LoadManifest("docs", "imported.txt"); err != nil || !ok {
		t.Fatalf("imported manifest missing: ok=%v err=%v", ok, err)
	}
	if _, ok, err := importedStore.LoadManifest("docs", "existing.txt"); err != nil || ok {
		t.Fatalf("old target state should be replaced after backup, ok=%v err=%v", ok, err)
	}
}

func TestSplitBadgerMetadataConfiguredCopiesSingleStoreIntoPerFolderStoresAndBacksUpTarget(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "single.badger")
	sourceStore, err := state.NewBadgerStore(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{1, 2, 3}}}}); err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.SaveManifest("media/lib", "song.flac", block.Manifest{Path: "song.flac", Size: 9, BlockSize: 9, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 9, Hash: []byte{4, 5, 6}}}}); err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}

	targetRoot := filepath.Join(root, "per-folder")
	existingStore, err := state.NewBadgerStore(filepath.Join(targetRoot, "docs.badger"))
	if err != nil {
		t.Fatal(err)
	}
	if err := existingStore.SaveManifest("docs", "old.txt", block.Manifest{Path: "old.txt", Size: 3}); err != nil {
		t.Fatal(err)
	}
	if err := existingStore.Close(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"metadata":{"backend":"badger","path":%q,"perFolder":true},
		"folders":[
			{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]},
			{"id":"media/lib","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}
		]
	}`, targetRoot, filepath.Join(root, "docs"), filepath.Join(root, "media"))
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := splitBadgerMetadataConfigured(cli.Options{Command: cli.CommandMetadata, Action: cli.ActionSplitBadger, Path: sourcePath}, configPath)
	if err != nil {
		t.Fatalf("splitBadgerMetadataConfigured: %v", err)
	}
	if result.ImportedManifests != 2 || result.Folders != 2 || result.BackupPath == "" {
		t.Fatalf("unexpected split result: %+v", result)
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("backup path missing: %v", err)
	}
	docsStore, err := state.NewBadgerStore(filepath.Join(targetRoot, "docs.badger"))
	if err != nil {
		t.Fatal(err)
	}
	defer docsStore.Close()
	if _, ok, err := docsStore.LoadManifest("docs", "alpha.txt"); err != nil || !ok {
		t.Fatalf("docs per-folder store missing migrated manifest: ok=%v err=%v", ok, err)
	}
	if _, ok, err := docsStore.LoadManifest("docs", "old.txt"); err != nil || ok {
		t.Fatalf("old target docs state should be replaced after backup, ok=%v err=%v", ok, err)
	}
	mediaStore, err := state.NewBadgerStore(filepath.Join(targetRoot, "media_lib.badger"))
	if err != nil {
		t.Fatal(err)
	}
	defer mediaStore.Close()
	if _, ok, err := mediaStore.LoadManifest("media/lib", "song.flac"); err != nil || !ok {
		t.Fatalf("media per-folder store missing migrated manifest: ok=%v err=%v", ok, err)
	}
	if _, ok, err := mediaStore.LoadManifest("docs", "alpha.txt"); err != nil || ok {
		t.Fatalf("media per-folder store should not contain docs manifest, ok=%v err=%v", ok, err)
	}
}

func TestCompactMetadataConfiguredUsesConfiguredBadgerMetadataStore(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	folderPath := filepath.Join(root, "docs")
	storePath := filepath.Join(root, "metadata.badger")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"metadata":{"backend":"badger","path":%q},
		"peers":[{"id":"peer-a","addresses":[]}],
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[]}]
	}`, storePath, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := state.NewBadgerStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "old.txt", block.Manifest{Path: "old.txt", Size: 3}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteManifest("docs", "old.txt"); err != nil {
		t.Fatal(err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePeerFolderState("peer-a", summary); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	results, err := compactMetadataConfigured(cli.Options{Command: cli.CommandMetadata, Action: cli.ActionCompact}, configPath)
	if err != nil {
		t.Fatalf("compactMetadataConfigured: %v", err)
	}
	if len(results) != 1 || results[0].Plan.FolderID != "docs" || results[0].CompactedTombstones != 1 {
		t.Fatalf("unexpected compaction results: %+v", results)
	}
	updatedStore, err := state.NewBadgerStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer updatedStore.Close()
	updated, err := updatedStore.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Tombstones != 0 || updated.Cursor != summary.Cursor {
		t.Fatalf("compaction did not prune safe tombstone while preserving cursor: %+v", updated)
	}
	if _, err := os.Stat(defaultStatePath(configPath)); !os.IsNotExist(err) {
		t.Fatalf("metadata compact should not create default JSON state when badger backend is configured: %v", err)
	}
}

func TestMaintenanceScrubAPIResponseSummarizesFolderResults(t *testing.T) {
	started := time.Unix(100, 0).UTC()
	finished := time.Unix(101, 0).UTC()
	response := maintenanceScrubAPIResponse(started, finished, []maintenanceScrubResult{
		{FolderID: "docs", Mode: maintenance.FileScrubFullBlocks, FilesScanned: 2, BytesScanned: 64, Reported: 1, Quarantined: 0, Complete: true, Cursor: maintenance.Cursor{Position: 4}},
		{FolderID: "photos", Mode: maintenance.FileScrubSampledBlocks, FilesScanned: 3, BytesScanned: 128, Reported: 0, Quarantined: 1, Complete: false, Cursor: maintenance.Cursor{Position: 7}},
	})

	if !response.StartedAt.Equal(started) || !response.FinishedAt.Equal(finished) || response.Folders != 2 || response.FilesScanned != 5 || response.BytesScanned != 192 || response.Reported != 1 || response.Quarantined != 1 || response.Complete {
		t.Fatalf("summary response mismatch: %+v", response)
	}
	if len(response.Results) != 2 || response.Results[0].FolderID != "docs" || response.Results[0].Cursor != 4 || response.Results[1].Mode != "sampled-blocks" {
		t.Fatalf("folder results mismatch: %+v", response.Results)
	}
}

func TestMaintenanceScrubConfiguredRunsSelectedFolderWithConfiguredModeAndBudget(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "docs")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(folderPath, "data.txt")
	if err := os.WriteFile(filePath, []byte("expected-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := block.BuildManifest(filePath, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("changed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	configJSON := fmt.Sprintf(`{
		"nodeName":"test-node",
		"api":{"listen":"127.0.0.1:0","key":"test-key"},
		"maintenance":{"scrubMode":"full-blocks","maxFilesPerRun":9,"maxBytesPerRun":9999},
		"folders":[{"id":"docs","path":%q,"mode":"sendrecv","blockSize":4096,"ignore":[],"maintenance":{"scrubMode":"light-metadata","maxFilesPerRun":1,"maxBytesPerRun":16}}]
	}`, folderPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(defaultStatePath(configPath))
	if err := store.SaveManifest("docs", "data.txt", manifest); err != nil {
		t.Fatal(err)
	}

	results, err := maintenanceScrubConfigured(cli.Options{Command: cli.CommandMaintenance, Action: cli.ActionScrub, ID: "docs"}, configPath)
	if err != nil {
		t.Fatalf("maintenanceScrubConfigured: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one scrub result, got %+v", results)
	}
	result := results[0]
	if result.FolderID != "docs" || result.FilesScanned != 1 || result.Reported != 1 || result.Mode != "light-metadata" || result.MaxFiles != 1 || result.MaxBytes != 16 {
		t.Fatalf("scrub did not honor selected folder config/mode/budget: %+v", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != maintenance.FileScrubMetadataMismatch {
		t.Fatalf("manual scrub did not report bounded metadata issue: %+v", result.Issues)
	}
}

func TestProcessMetadataReconciliationRunsMetadataOnlyCatchupForLaggingPeer(t *testing.T) {
	remoteRoot := t.TempDir()
	serverStore := state.NewJSONStore(filepath.Join(t.TempDir(), "server-state.json"))
	clientStore := state.NewJSONStore(filepath.Join(t.TempDir(), "client-state.json"))
	content := []byte("remote metadata")
	hash := []byte{0x04, 0x05, 0x06}
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := serverStore.SaveManifest("docs", "remote.txt", block.Manifest{Path: "remote.txt", Size: int64(len(content)), BlockSize: len(content), HashState: "complete", Blocks: []block.Block{{Index: 0, Size: len(content), Hash: hash}}}); err != nil {
		t.Fatal(err)
	}
	if err := clientStore.SavePeerFolderState("peer-a", state.FolderSummary{FolderID: "docs", Cursor: 0, StateHash: "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := clientStore.SaveManifest("docs", "local.txt", block.Manifest{Path: "local.txt", Size: 5}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		NodeName: "node-b",
		Peers:    []config.PeerConfig{{ID: "peer-a", IdentityPublicKey: "", Endpoints: []config.EndpointConfig{{Kind: "pipe", Address: "test"}}}},
		Folders:  []config.FolderConfig{{ID: "docs", Path: t.TempDir(), Enabled: true, Mode: config.ModeSendReceive}},
	}
	apiServer := api.NewServer(api.State{PeersState: []api.PeerState{{ID: "peer-a", Status: "configured"}}}, "test-key")
	dialed := 0

	result := processMetadataReconciliation(context.Background(), apiServer, cfg, clientStore, func(ctx context.Context, peer config.PeerConfig, folder config.FolderConfig) (io.ReadWriteCloser, error) {
		dialed++
		client, serverConn := net.Pipe()
		go func() {
			defer serverConn.Close()
			_ = streamsync.NewServer(streamsync.ServerConfig{NodeID: peer.ID, Folders: map[string]string{folder.ID: remoteRoot}, MetadataStore: serverStore}).Serve(ctx, serverConn)
		}()
		return client, nil
	})

	if result.Started != 1 || result.Completed != 1 || result.Failed != 0 || dialed != 1 {
		t.Fatalf("metadata reconciliation result = %+v dialed=%d, want one completed catch-up", result, dialed)
	}
	if _, ok, err := clientStore.LoadPeerManifest("peer-a", "docs", "remote.txt"); err != nil || !ok {
		t.Fatalf("scheduled metadata reconciliation did not update peer cache: ok=%v err=%v", ok, err)
	}
	eventsRecorder := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "test-key")
	apiServer.Router().ServeHTTP(eventsRecorder, eventsReq)
	if !strings.Contains(eventsRecorder.Body.String(), "metadata.catchup.finished") {
		t.Fatalf("metadata reconciliation event not published: %s", eventsRecorder.Body.String())
	}
}

func TestAPIStateFromConfigIncludesFolderIndexAndPeerMetadataState(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	folderPath := filepath.Join(root, "docs")
	store := state.NewJSONStore(defaultStatePath(configPath))
	if err := store.SaveManifest("docs", "seeded.txt", block.Manifest{HashState: engine.HashStateAssumedValidUnverified, ModTimeUnixNano: 200, SeedBaselineModTimeUnixNano: 100}); err != nil {
		t.Fatal(err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatal(err)
	}
	staleSummary := summary
	staleSummary.Cursor = 0
	staleSummary.StateHash = "stale"
	if err := store.SavePeerFolderState("peer-a", staleSummary); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		NodeName: "node-a",
		Folders:  []config.FolderConfig{{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive}},
		Peers:    []config.PeerConfig{{ID: "peer-a", Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://127.0.0.1:22000"}}}},
	}

	apiState := apiStateFromConfig(cfg, configPath, 1, "running")
	if len(apiState.FoldersState) != 1 {
		t.Fatalf("folders = %d", len(apiState.FoldersState))
	}
	index := apiState.FoldersState[0].Index
	if !index.ProvisionalReadOnly || index.UnverifiedSeedFiles != 1 || index.DateCorrectionsPending != 1 {
		t.Fatalf("folder index state not loaded from metadata store: %+v", index)
	}
	if len(apiState.PeersState) != 1 || len(apiState.PeersState[0].Metadata.Folders) != 1 {
		t.Fatalf("peer metadata state missing: %+v", apiState.PeersState)
	}
	folderSync := apiState.PeersState[0].Metadata.Folders[0]
	if folderSync.FolderID != "docs" || folderSync.PeerCursor != 0 || folderSync.LocalCursor != summary.Cursor || folderSync.InSync {
		t.Fatalf("peer metadata status not loaded from metadata store: %+v", folderSync)
	}
}

func TestAPIStateFromConfigExposesContainerNetworkingDiagnostics(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	cfg := config.Config{
		Peers: []config.PeerConfig{{
			ID: "container-peer",
			Endpoints: []config.EndpointConfig{{
				Kind:    "manual",
				Address: "http://172.18.0.5:22000",
			}},
		}},
	}

	apiState := apiStateFromConfig(cfg, configPath, 1, "running")

	if len(apiState.PeersState) != 1 || len(apiState.PeersState[0].NetworkDiagnostics) != 1 {
		t.Fatalf("expected peer network diagnostics, got %+v", apiState.PeersState)
	}
	diagnostic := apiState.PeersState[0].NetworkDiagnostics[0]
	if diagnostic.Code != string(routing.DiagnosticContainerBridgeIsolated) || diagnostic.Network != string(routing.ContainerBridgeNetwork) {
		t.Fatalf("unexpected network diagnostic: %+v", diagnostic)
	}
	for _, want := range []string{"published-port", "sidecar"} {
		if !strings.Contains(diagnostic.Guidance, want) {
			t.Fatalf("diagnostic guidance should mention %q, got %q", want, diagnostic.Guidance)
		}
	}
}

func TestAPIStateFromConfigExposesDeferredDeleteMetadataCatchupState(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	folderPath := filepath.Join(root, "docs")
	store := state.NewJSONStore(defaultStatePath(configPath))
	if err := store.SaveManifest("docs", "current.txt", block.Manifest{Size: 7}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSkippedDelete(state.SkippedDelete{
		FolderID:                  "docs",
		Path:                      "stale.txt",
		RequiredMetadataCursor:    5,
		RequiredMetadataStateHash: "peer-current",
		Reason:                    "metadata_catchup_pending",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePendingWrite(state.PendingWrite{FolderID: "docs", Path: "locked.txt", Reason: "locked_apply_pending"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive}}}

	apiState := apiStateFromConfig(cfg, configPath, 1, "running")

	if len(apiState.FoldersState) != 1 {
		t.Fatalf("folders = %d", len(apiState.FoldersState))
	}
	sync := apiState.FoldersState[0].Sync
	if sync.LocalCursor == 0 || sync.LocalStateHash == "" {
		t.Fatalf("local metadata summary missing from folder sync state: %+v", sync)
	}
	if sync.DeferredDeletes != 1 || sync.ReadyDeferredDeletes != 0 || !sync.MetadataCatchupPending {
		t.Fatalf("deferred delete metadata catch-up state not exposed: %+v", sync)
	}
	warnings := apiState.FoldersState[0].Warnings
	if warnings.PendingLockedApplies != 1 || len(warnings.Recent) != 1 || warnings.Recent[0].Kind != "locked_apply_pending" || warnings.Recent[0].Path != "locked.txt" {
		t.Fatalf("pending locked apply warning state not exposed: %+v", warnings)
	}
}

func TestPublishFolderWarningsUpdatesStateAndEmitsEvents(t *testing.T) {
	apiServer := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Status: "idle"}}}, "test-key")
	warnings := []foldersync.InaccessibleWarning{{FolderID: "docs", Role: "source", Path: "locked.txt", Error: "open locked.txt: permission denied"}}

	publishFolderWarnings(apiServer, warnings)

	state := apiServer.CurrentState()
	if len(state.FoldersState) != 1 || state.FoldersState[0].Warnings.InaccessibleFiles != 1 || len(state.FoldersState[0].Warnings.Recent) != 1 {
		t.Fatalf("folder warning state not updated: %+v", state.FoldersState)
	}
	if state.FoldersState[0].Warnings.Recent[0].Path != "locked.txt" || state.FoldersState[0].Warnings.Recent[0].Kind != "inaccessible" {
		t.Fatalf("recent warning missing path/kind: %+v", state.FoldersState[0].Warnings.Recent)
	}
	eventsRecorder := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "test-key")
	apiServer.Router().ServeHTTP(eventsRecorder, eventsReq)
	if !strings.Contains(eventsRecorder.Body.String(), "folder.warning") || !strings.Contains(eventsRecorder.Body.String(), "locked.txt") {
		t.Fatalf("warning event not published: %s", eventsRecorder.Body.String())
	}
}

func TestCLIErrorLineUsesHumanReadablePolicyWithoutLoggerPrefix(t *testing.T) {
	line := cliErrorLine("resolve config", fmt.Errorf("permission denied"))
	if line != "fse: resolve config: permission denied" {
		t.Fatalf("cli error line = %q", line)
	}
	if strings.Contains(line, "{") || strings.Contains(line, "level=") || strings.Contains(line, "2006/") {
		t.Fatalf("cli human error policy should not emit structured JSON or logger prefixes: %q", line)
	}
}

func TestDaemonOperationalLogsAreStructuredJSON(t *testing.T) {
	var logs bytes.Buffer
	oldLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(oldLogOutput)
		structuredlog.Reset()
	})

	daemonlogging.DaemonStarted("node-a", 2, "/tmp/fse.json")
	daemonlogging.APIListening("127.0.0.1:22000")
	daemonlogging.ConfigReloadRejected(fmt.Errorf("invalid partial config"))
	daemonlogging.DiscoveryRouterUnavailable(fmt.Errorf("bootstrap failed"))

	lines := bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n"))
	if len(lines) != 4 {
		t.Fatalf("structured daemon log line count = %d, logs=%s", len(lines), logs.String())
	}
	wantEvents := []string{"daemon.started", "api.listening", "config.reload.rejected", "discovery.dht.unavailable"}
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("daemon operational log %d is not JSON: %s", i, line)
		}
		if record["event"] != wantEvents[i] || record["level"] == "" || record["message"] == "" {
			t.Fatalf("daemon operational log %d missing stable fields: %+v", i, record)
		}
	}
}

func TestConfigureStructuredLoggingUsesConfigRelativeOutputAndLevel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "fse.json")
	t.Cleanup(structuredlog.Reset)

	cfg := config.Config{Logging: config.LoggingConfig{Level: config.LogLevelError, Output: "logs/fse.jsonl"}}
	if err := daemonlogging.Configure(cfg, configPath); err != nil {
		t.Fatalf("daemonlogging.Configure: %v", err)
	}
	daemonlogging.ConfigReloadRejected(fmt.Errorf("warn should be filtered"))
	daemonlogging.ReloadedMetadataStoreOpenFailed(fmt.Errorf("boom"))
	structuredlog.Reset()

	data, err := os.ReadFile(filepath.Join(dir, "logs", "fse.jsonl"))
	if err != nil {
		t.Fatalf("read configured log: %v", err)
	}
	if strings.Contains(string(data), "config.reload.rejected") || !strings.Contains(string(data), "metadata.store.reload_failed") {
		t.Fatalf("configured logging did not honor level/output: %s", data)
	}
}

func TestPublishMaintenanceScrubIssueUpdatesAPIEventsAndLogsQuarantineStatus(t *testing.T) {
	apiServer := api.NewServer(api.State{FoldersState: []api.FolderState{{ID: "docs", Status: "idle"}}}, "test-key")
	issue := maintenance.FileScrubIssue{FolderID: "docs", Path: "data.bin", Kind: maintenance.FileScrubHashMismatch, Classification: maintenance.FileScrubSuspectedCorruption, Evidence: "repeated-verification"}
	var logs bytes.Buffer
	oldLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldLogOutput) })

	message := publishMaintenanceScrubIssue(apiServer, issue, ".sync/quarantine/data.bin.1")

	state := apiServer.CurrentState()
	if len(state.FoldersState) != 1 || len(state.FoldersState[0].Warnings.Recent) != 1 {
		t.Fatalf("maintenance warning state not updated: %+v", state.FoldersState)
	}
	warning := state.FoldersState[0].Warnings.Recent[0]
	if warning.Kind != "maintenance_suspected_corruption" || warning.Path != "data.bin" {
		t.Fatalf("maintenance warning missing kind/path: %+v", warning)
	}
	if !strings.Contains(warning.Message, "restored copy is in place") || !strings.Contains(warning.Message, "original remains available in quarantine") {
		t.Fatalf("maintenance warning does not clearly state restored/quarantine status: %q", warning.Message)
	}
	if warning.Repair == nil || !warning.Repair.RestoredCopyInPlace || warning.Repair.QuarantinePath != ".sync/quarantine/data.bin.1" || warning.Repair.OriginalAvailable != true {
		t.Fatalf("maintenance warning repair status not API/GUI friendly: %+v", warning.Repair)
	}
	if message != warning.Message {
		t.Fatalf("returned message = %q, warning message = %q", message, warning.Message)
	}
	eventsRecorder := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "test-key")
	apiServer.Router().ServeHTTP(eventsRecorder, eventsReq)
	body := eventsRecorder.Body.String()
	if !strings.Contains(body, "maintenance.warning") || !strings.Contains(body, "restored copy is in place") || !strings.Contains(body, "quarantine") {
		t.Fatalf("maintenance warning event not published with clear repair status: %s", body)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("maintenance warning log is not structured JSON: %s", logs.String())
	}
	if record["event"] != "maintenance.warning" || record["folder_id"] != "docs" || record["path"] != "data.bin" || record["classification"] != string(maintenance.FileScrubSuspectedCorruption) || record["quarantine"] != ".sync/quarantine/data.bin.1" {
		t.Fatalf("maintenance warning structured log missing fields: %+v", record)
	}
}

func TestMergeDiscoveredPeersAddsNewPeersWithoutReplacingConfigured(t *testing.T) {
	state := api.State{PeersState: []api.PeerState{{ID: "manual-peer", Status: "configured", Endpoint: "manual:http://10.0.0.2:22000"}}}
	discovered := []discovery.Peer{
		{ID: "manual-peer", Addresses: []string{"/ip4/10.0.0.2/tcp/22000"}},
		{ID: "dht-peer", Addresses: []string{"/ip4/203.0.113.9/tcp/22000/p2p/dht-peer"}},
	}

	updated, events := discoverycontrol.MergeDiscoveredPeers(state, discovered)

	if len(updated.PeersState) != 2 {
		t.Fatalf("peers = %+v, want configured + discovered", updated.PeersState)
	}
	if updated.PeersState[0].ID != "manual-peer" || updated.PeersState[0].Status != "configured" {
		t.Fatalf("configured peer was replaced: %+v", updated.PeersState[0])
	}
	if updated.PeersState[1].ID != "dht-peer" || updated.PeersState[1].Status != "discovered" || updated.PeersState[1].Endpoint != "discovered:/ip4/203.0.113.9/tcp/22000/p2p/dht-peer" {
		t.Fatalf("discovered peer not exposed correctly: %+v", updated.PeersState[1])
	}
	if len(events) != 1 || events[0].Type != "peer.discovered" || events[0].PeerID != "dht-peer" {
		t.Fatalf("discovery event not emitted for new peer: %+v", events)
	}
}

func TestPollDiscoverySourcesMergesSourcePeersAndReportsErrors(t *testing.T) {
	sources := []discovery.Source{
		discovery.NewStaticSource([]discovery.Peer{{ID: "peer-a", Addresses: []string{"/ip4/203.0.113.1/tcp/22000"}}}),
		failingDiscoverySource{},
		discovery.NewStaticSource([]discovery.Peer{{ID: "peer-b", Addresses: []string{"/ip4/203.0.113.2/tcp/22000"}}}),
	}

	peers, events := discoverycontrol.PollSources(sources)

	if len(peers) != 2 || peers[0].ID != "peer-a" || peers[1].ID != "peer-b" {
		t.Fatalf("peers = %+v, want both successful sources in order", peers)
	}
	if len(events) != 1 || events[0].Type != "discovery.error" || events[0].Message != "discovery source failed" {
		t.Fatalf("error event not reported: %+v", events)
	}
}

func TestRuntimeDiscoverySourcesInjectsPublicDHTRouterWhenEnabled(t *testing.T) {
	oldFactory := newRuntimeDHTRouter
	defer func() { newRuntimeDHTRouter = oldFactory }()
	created := 0
	newRuntimeDHTRouter = func(config.Config) discovery.DHTRouter {
		created++
		return &recordingRuntimeDHTRouter{peers: []discovery.Peer{{ID: "peer-dht", Addresses: []string{"/ip4/203.0.113.9/tcp/22000/p2p/peer-dht"}}}}
	}
	cfg := config.Config{NodeName: "node-a", Discovery: config.DiscoveryConfig{DHT: true}}

	sources := runtimeDiscoverySources(cfg)
	peers, events := discoverycontrol.PollSources(sources)

	if created != 1 {
		t.Fatalf("DHT router factory called %d times, want once", created)
	}
	if len(events) != 0 || len(peers) != 1 || peers[0].ID != "peer-dht" {
		t.Fatalf("runtime DHT source not wired into polling path: peers=%+v events=%+v", peers, events)
	}
}

func TestRuntimeDiscoverySourcesDoesNotCreateDHTRouterWhenDiscoveryDisabled(t *testing.T) {
	oldFactory := newRuntimeDHTRouter
	defer func() { newRuntimeDHTRouter = oldFactory }()
	newRuntimeDHTRouter = func(config.Config) discovery.DHTRouter {
		t.Fatalf("disabled discovery must not create public DHT router")
		return nil
	}
	cfg := config.Config{
		NodeName:  "node-a",
		Discovery: config.DiscoveryConfig{Disabled: true, DHT: true},
		Peers:     []config.PeerConfig{{ID: "manual-peer", Addresses: []string{"/ip4/10.0.0.2/tcp/22000/p2p/manual-peer"}}},
	}

	sources := runtimeDiscoverySources(cfg)
	peers, events := discoverycontrol.PollSources(sources)

	if len(events) != 0 || len(peers) != 1 || peers[0].ID != "manual-peer" {
		t.Fatalf("manual peers should remain without DHT when disabled: peers=%+v events=%+v", peers, events)
	}
}

func TestPeerSyncFinishedMessageReportsMissingIgnoreIncludes(t *testing.T) {
	message := peerSyncFinishedMessage(peersync.Result{Writes: 1, Deletes: 0, FilesMoved: 4, BlocksFetched: 2, BlocksReused: 3, MissingIgnoreIncludes: []string{"rules/missing-ignore"}})

	if !strings.Contains(message, "moves=4") || !strings.Contains(message, "missingIgnoreIncludes=1") || !strings.Contains(message, "rules/missing-ignore") {
		t.Fatalf("missing include status not surfaced in event message: %s", message)
	}
}

func TestPeerSyncFinishedMessageReportsRouteDecision(t *testing.T) {
	message := peerSyncFinishedMessageWithRoute(peersync.Result{Writes: 1, BlocksFetched: 2}, peerPull{PeerID: "peer-lan", Path: routing.DirectPath, Network: routing.LocalNetwork, RouteReason: routing.ReasonLocalPreferred})

	for _, want := range []string{"routePath=direct", "routeNetwork=local", "routeReason=local_preferred"} {
		if !strings.Contains(message, want) {
			t.Fatalf("route decision %q not surfaced in event message: %s", want, message)
		}
	}
}

func TestPeerPullsUsesRelayHTTPWhenNoDirectPeerIsAvailable(t *testing.T) {
	cfg := config.Config{
		Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{{
			ID:     "relay-peer",
			APIKey: "secret",
			Endpoints: []config.EndpointConfig{{
				Kind:    "relay",
				Address: "https://relay.example/peer/docs",
			}},
		}},
	}

	pulls := peerPulls(cfg, "docs")

	if len(pulls) != 1 || pulls[0].PeerID != "relay-peer" || pulls[0].BaseURL != "https://relay.example/peer/docs" {
		t.Fatalf("relay-only peer should remain usable for folder pulls: %+v", pulls)
	}
}

func TestPeerPullsPreferDirectHTTPPeersOverRelayPeers(t *testing.T) {
	cfg := config.Config{
		Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{
			{
				ID:     "relay-peer",
				APIKey: "relay-secret",
				Endpoints: []config.EndpointConfig{{
					Kind:    "relay",
					Address: "https://relay.example/peer/docs",
				}},
			},
			{
				ID:     "direct-peer",
				APIKey: "direct-secret",
				Endpoints: []config.EndpointConfig{{
					Kind:    "manual",
					Address: "https://direct.example/peer/docs",
				}},
			},
		},
	}

	pulls := peerPulls(cfg, "docs")

	if len(pulls) != 1 || pulls[0].PeerID != "direct-peer" || pulls[0].BaseURL != "https://direct.example/peer/docs" {
		t.Fatalf("direct peer should be selected instead of relay when both are available: %+v", pulls)
	}
	if len(pulls[0].BlockSources) != 2 {
		t.Fatalf("selected pull should retain all candidate block sources for per-block routing, got %+v", pulls[0].BlockSources)
	}
}

func TestPeerPullsPreferLocalHTTPPeerOverDirectWANPeer(t *testing.T) {
	cfg := config.Config{
		Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{
			{
				ID:        "wan-peer",
				APIKey:    "wan-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://203.0.113.10:22000"}},
			},
			{
				ID:        "lan-peer",
				APIKey:    "lan-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://192.168.1.20:22000"}},
			},
		},
	}

	pulls := peerPulls(cfg, "docs")

	if len(pulls) != 1 || pulls[0].PeerID != "lan-peer" || pulls[0].BaseURL != "http://192.168.1.20:22000" {
		t.Fatalf("LAN peer should be selected instead of direct WAN when both are available: %+v", pulls)
	}
	if pulls[0].RouteReason != routing.ReasonLocalPreferred {
		t.Fatalf("expected local-preferred route reason, got %q", pulls[0].RouteReason)
	}
}

func TestPeerPullsUseEndpointNetworkHintToPromoteContainerBridgePeer(t *testing.T) {
	cfg := config.Config{
		Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{
			{
				ID:        "wan-peer",
				APIKey:    "wan-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://203.0.113.10:22000"}},
			},
			{
				ID:     "container-peer",
				APIKey: "container-secret",
				Endpoints: []config.EndpointConfig{{
					Kind:        "manual",
					Address:     "http://172.17.0.1:22000",
					NetworkHint: "local",
				}},
			},
		},
	}

	pulls := peerPulls(cfg, "docs")

	if len(pulls) != 1 || pulls[0].PeerID != "container-peer" {
		t.Fatalf("hinted container peer should be treated as local and selected over WAN: %+v", pulls)
	}
	if pulls[0].Network != routing.LocalNetwork || pulls[0].RouteReason != routing.ReasonLocalPreferred {
		t.Fatalf("expected local network/local-preferred reason, got network=%q reason=%q", pulls[0].Network, pulls[0].RouteReason)
	}
}

func TestPeerPullsUseDiscoveryContainerGatewayHints(t *testing.T) {
	cfg := config.Config{
		Discovery: config.DiscoveryConfig{NetworkHints: config.NetworkHintsConfig{LocalContainerGatewayIPs: []string{"172.17.0.1"}}},
		Folders:   []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{
			{
				ID:        "wan-peer",
				APIKey:    "wan-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://203.0.113.10:22000"}},
			},
			{
				ID:        "container-gateway-peer",
				APIKey:    "container-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://172.17.0.1:22000"}},
			},
		},
	}

	pulls := peerPulls(cfg, "docs")

	if len(pulls) != 1 || pulls[0].PeerID != "container-gateway-peer" {
		t.Fatalf("configured container gateway hint should promote peer to local over WAN: %+v", pulls)
	}
	if pulls[0].Network != routing.LocalNetwork || pulls[0].RouteReason != routing.ReasonLocalPreferred {
		t.Fatalf("expected hinted container gateway to be local preferred, got network=%q reason=%q", pulls[0].Network, pulls[0].RouteReason)
	}
}

func TestPeerPullsUseDiscoveryLocalCIDRHints(t *testing.T) {
	cfg := config.Config{
		Discovery: config.DiscoveryConfig{NetworkHints: config.NetworkHintsConfig{LocalCIDRs: []string{"172.20.0.0/16"}}},
		Folders:   []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{
			{
				ID:        "wan-peer",
				APIKey:    "wan-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://203.0.113.10:22000"}},
			},
			{
				ID:        "published-port-peer",
				APIKey:    "container-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://172.20.4.10:22000"}},
			},
		},
	}

	pulls := peerPulls(cfg, "docs")

	if len(pulls) != 1 || pulls[0].PeerID != "published-port-peer" {
		t.Fatalf("configured local CIDR hint should promote peer to local over WAN: %+v", pulls)
	}
	if pulls[0].Network != routing.LocalNetwork || pulls[0].RouteReason != routing.ReasonLocalPreferred {
		t.Fatalf("expected hinted CIDR peer to be local preferred, got network=%q reason=%q", pulls[0].Network, pulls[0].RouteReason)
	}
}

func TestPeerPullsUseDiscoveryPublishedPortMappings(t *testing.T) {
	cfg := config.Config{
		Discovery: config.DiscoveryConfig{NetworkHints: config.NetworkHintsConfig{PublishedPortMappings: []config.PublishedPortMappingConfig{{HostIP: "172.18.0.1", HostPort: 32200, ContainerIP: "172.18.0.5", ContainerPort: 22000}}}},
		Folders:   []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{
			{
				ID:        "wan-peer",
				APIKey:    "wan-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://203.0.113.10:22000"}},
			},
			{
				ID:        "published-port-peer",
				APIKey:    "container-secret",
				Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "http://172.18.0.1:32200"}},
			},
		},
	}

	pulls := peerPulls(cfg, "docs")

	if len(pulls) != 1 || pulls[0].PeerID != "published-port-peer" {
		t.Fatalf("configured published-port mapping should promote peer to local over WAN: %+v", pulls)
	}
	if pulls[0].Network != routing.LocalNetwork || pulls[0].RouteReason != routing.ReasonLocalPreferred {
		t.Fatalf("expected published-port peer to be local preferred, got network=%q reason=%q", pulls[0].Network, pulls[0].RouteReason)
	}
}

func TestPlanRuntimeCooperativeBlockFetchesAssignsSameLANPeerToLocalSource(t *testing.T) {
	plans := planRuntimeCooperativeBlockFetches("docs", []peerPull{
		{PeerID: "lan-b", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "lan-c", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "wan-d", Network: routing.WANNetwork, Path: routing.DirectPath},
	})

	if len(plans) != 1 {
		t.Fatalf("expected one cooperative runtime plan for same-LAN peers, got %+v", plans)
	}
	if plans[0].BlockID != "docs:live-transfer-pass" || plans[0].Reason != routing.ReasonCooperativeLocalRedistribution {
		t.Fatalf("unexpected cooperative plan summary: %+v", plans[0])
	}
	assignments := map[string]routing.CooperativeFetchAssignment{}
	for _, assignment := range plans[0].Assignments {
		assignments[assignment.PeerID] = assignment
	}
	if assignments["lan-b"].Action != routing.CooperativeFetchWAN {
		t.Fatalf("deterministic first LAN peer should fetch from WAN once: %+v", assignments)
	}
	if assignments["lan-c"].Action != routing.CooperativeFetchLocal || assignments["lan-c"].SourcePeerID != "lan-b" {
		t.Fatalf("second same-LAN peer should reuse the elected local source: %+v", assignments)
	}
	if _, ok := assignments["wan-d"]; ok {
		t.Fatalf("WAN peer should not be forced into same-LAN cooperative redistribution: %+v", assignments)
	}
}

func TestPeerPullsUseLiveSidecarEndpointCandidatesOverConfiguredWAN(t *testing.T) {
	cfg := config.Config{
		Discovery: config.DiscoveryConfig{NetworkHints: config.NetworkHintsConfig{PublishedPortMappings: []config.PublishedPortMappingConfig{{HostIP: "172.18.0.1", HostPort: 32200}}}},
		Folders:   []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{{
			ID:        "container-peer",
			APIKey:    "container-secret",
			Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://203.0.113.10:22000"}},
		}},
	}

	pulls := peerPullsWithEndpointObservations(cfg, "docs", []routing.EndpointObservation{{
		PeerID:    "container-peer",
		Address:   "http://172.18.0.1:32200",
		Reachable: true,
		Path:      routing.DirectPath,
	}})

	if len(pulls) != 1 || pulls[0].BaseURL != "http://172.18.0.1:32200" {
		t.Fatalf("live sidecar candidate should be selected before configured WAN endpoint: %+v", pulls)
	}
	if pulls[0].Network != routing.LocalNetwork || pulls[0].RouteReason != routing.ReasonLocalPreferred {
		t.Fatalf("expected live sidecar candidate to be local preferred, got network=%q reason=%q", pulls[0].Network, pulls[0].RouteReason)
	}
}

func TestStreamEndpointCandidatesPreferLiveSidecarOverConfiguredWAN(t *testing.T) {
	peer := config.PeerConfig{
		ID: "container-peer",
		Endpoints: []config.EndpointConfig{{
			Kind:    "manual",
			Address: "tcp://203.0.113.10:22000",
		}},
	}

	candidates := metadatareconcile.PeerStreamEndpointCandidates(peer, []routing.EndpointObservation{{
		PeerID:    "container-peer",
		Address:   "tcp://172.18.0.1:32200",
		Reachable: true,
		Path:      routing.DirectPath,
	}}, routing.NetworkHints{PublishedPortMappings: []routing.PublishedPortMapping{{HostIP: "172.18.0.1", HostPort: 32200}}})

	if len(candidates) == 0 {
		t.Fatalf("expected stream endpoint candidates")
	}
	if candidates[0].Address != "tcp://172.18.0.1:32200" || candidates[0].Source != routing.EndpointSourceSidecar {
		t.Fatalf("live sidecar stream endpoint should sort first, got %+v", candidates)
	}
	if candidates[0].Network != routing.LocalNetwork || candidates[0].Path != routing.DirectPath {
		t.Fatalf("expected local direct sidecar candidate, got network=%q path=%q", candidates[0].Network, candidates[0].Path)
	}
}

func TestProcessDiscoveryPollUpdatesAPIStateAndPublishesEvents(t *testing.T) {
	server := api.NewServer(api.State{PeersState: []api.PeerState{{ID: "manual-peer", Status: "configured", Endpoint: "manual:http://10.0.0.2:22000"}}, Peers: 1}, "test-key")
	sources := []discovery.Source{
		discovery.NewStaticSource([]discovery.Peer{{ID: "dht-peer", Addresses: []string{"/ip4/203.0.113.9/tcp/22000/p2p/dht-peer"}}}),
		failingDiscoverySource{},
	}

	processDiscoveryPoll(server, sources)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/peers", nil)
	req.Header.Set("X-FSE-API-Key", "test-key")
	server.Router().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("peers status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var peers []api.PeerState
	if err := json.Unmarshal(recorder.Body.Bytes(), &peers); err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 || peers[0].ID != "manual-peer" || peers[1].ID != "dht-peer" || peers[1].Status != "discovered" {
		t.Fatalf("api peers not updated with discovered peer: %+v", peers)
	}

	eventsRecorder := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "test-key")
	server.Router().ServeHTTP(eventsRecorder, eventsReq)
	eventsBody := eventsRecorder.Body.String()
	if !strings.Contains(eventsBody, "peer.discovered") || !strings.Contains(eventsBody, "discovery.error") {
		t.Fatalf("discovery events not published: %s", eventsBody)
	}
}

func TestProcessDiscoveryPollReturnsLiveSidecarEndpointObservations(t *testing.T) {
	server := api.NewServer(api.State{PeersState: []api.PeerState{{ID: "container-peer", Status: "configured", Endpoint: "manual:https://203.0.113.10:22000"}}, Peers: 1}, "test-key")
	sources := []discovery.Source{discovery.NewStaticSource([]discovery.Peer{{
		ID:        "container-peer",
		Addresses: []string{"http://172.18.0.1:32200", "tcp://172.18.0.1:32201", "/ip4/203.0.113.10/tcp/22000/p2p/container-peer"},
	}})}
	cfg := config.Config{Discovery: config.DiscoveryConfig{NetworkHints: config.NetworkHintsConfig{PublishedPortMappings: []config.PublishedPortMappingConfig{{HostIP: "172.18.0.1", HostPort: 32200}}}}}

	observations := processDiscoveryPollWithEndpointObservations(server, sources, cfg)

	if len(observations) != 2 {
		t.Fatalf("expected HTTP and TCP live observations only, got %+v", observations)
	}
	if observations[0].PeerID != "container-peer" || observations[0].Address != "http://172.18.0.1:32200" || !observations[0].Reachable || observations[0].Path != routing.DirectPath {
		t.Fatalf("HTTP observation not converted correctly: %+v", observations)
	}
	if observations[0].NetworkHint != string(routing.LocalNetwork) {
		t.Fatalf("expected published-port sidecar observation to carry local hint, got %+v", observations[0])
	}
	if observations[1].Address != "tcp://172.18.0.1:32201" {
		t.Fatalf("TCP stream observation not preserved: %+v", observations)
	}
}

type recordingRuntimeDHTRouter struct {
	bootstrap []string
	peers     []discovery.Peer
}

func (r *recordingRuntimeDHTRouter) Bootstrap(ctx context.Context, peers []string) error {
	r.bootstrap = append([]string(nil), peers...)
	return nil
}

func (r *recordingRuntimeDHTRouter) FindPeers(ctx context.Context, namespace string) ([]discovery.Peer, error) {
	return append([]discovery.Peer(nil), r.peers...), nil
}

type failingDiscoverySource struct{}

func (failingDiscoverySource) Peers() ([]discovery.Peer, error) {
	return nil, fmt.Errorf("discovery source failed")
}

func TestRebuildFolderMonitorRestartsWhenFolderSetChanges(t *testing.T) {
	oldMonitor := &fakeFolderMonitor{}
	var started [][]monitor.Folder
	cfg := config.Config{Folders: []config.FolderConfig{
		{ID: "docs", Path: "/srv/docs", Mode: config.ModeSendReceive},
		{ID: "photos", Path: "/srv/photos", Mode: config.ModeSendOnly},
	}}

	next, changed, err := daemonmonitor.Rebuild(oldMonitor, []monitor.Folder{{ID: "docs", Path: "/srv/old-docs"}}, cfg, func(folders []monitor.Folder) (daemonmonitor.Closable, error) {
		started = append(started, append([]monitor.Folder(nil), folders...))
		return &fakeFolderMonitor{}, nil
	})
	if err != nil {
		t.Fatalf("rebuildFolderMonitor: %v", err)
	}
	if !changed {
		t.Fatalf("expected folder set change to rebuild monitor")
	}
	if !oldMonitor.closed {
		t.Fatalf("old monitor was not closed before replacement")
	}
	if next == nil || next == oldMonitor {
		t.Fatalf("replacement monitor not returned")
	}
	if len(started) != 1 || len(started[0]) != 2 || started[0][0].ID != "docs" || started[0][1].ID != "photos" {
		t.Fatalf("new monitor started with wrong folders: %+v", started)
	}
}

func TestRebuildFolderMonitorKeepsExistingMonitorWhenFolderSetUnchanged(t *testing.T) {
	oldMonitor := &fakeFolderMonitor{}
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeSendReceive}}}
	starts := 0

	next, changed, err := daemonmonitor.Rebuild(oldMonitor, []monitor.Folder{{ID: "docs", Path: "/srv/docs"}}, cfg, func(folders []monitor.Folder) (daemonmonitor.Closable, error) {
		starts++
		return &fakeFolderMonitor{}, nil
	})
	if err != nil {
		t.Fatalf("rebuildFolderMonitor: %v", err)
	}
	if changed {
		t.Fatalf("unchanged folder set should not rebuild monitor")
	}
	if oldMonitor.closed || next != oldMonitor || starts != 0 {
		t.Fatalf("monitor unexpectedly rebuilt: closed=%v nextSame=%v starts=%d", oldMonitor.closed, next == oldMonitor, starts)
	}
}

type fakeFolderMonitor struct {
	closed bool
}

func (f *fakeFolderMonitor) Close() error {
	f.closed = true
	return nil
}

func TestAPIStateExposesConfiguredPeerTransferLimits(t *testing.T) {
	cfg := config.Config{
		NodeName: "node-a",
		Transfer: config.TransferConfig{SendBytesPerSecond: 1000, ReceiveBytesPerSecond: 2000},
		Peers:    []config.PeerConfig{{ID: "peer-a", SendBytesPerSecond: 700, ReceiveBytesPerSecond: 0, Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "tcp://10.0.0.2:22000"}}}},
	}
	statePath := filepath.Join(t.TempDir(), "state.json")

	apiState := apiStateFromConfigWithStore(cfg, filepath.Join(t.TempDir(), "config.json"), 1, "running", state.NewJSONStore(statePath))

	if len(apiState.PeersState) != 1 {
		t.Fatalf("peers = %d", len(apiState.PeersState))
	}
	limits := apiState.PeersState[0].Transfer
	if limits.Configured.SendBytesPerSecond != 700 || limits.Configured.ReceiveBytesPerSecond != 2000 {
		t.Fatalf("configured peer transfer limits = %+v", limits.Configured)
	}
	if limits.Effective.SendBytesPerSecond != 700 || limits.Effective.ReceiveBytesPerSecond != 2000 {
		t.Fatalf("effective local transfer limits = %+v", limits.Effective)
	}
	if limits.SendCause != "local_peer" || limits.ReceiveCause != "local_global" {
		t.Fatalf("transfer causes = send %q receive %q", limits.SendCause, limits.ReceiveCause)
	}
}

func TestAPIStateExposesBackupDestinationMode(t *testing.T) {
	cfg := config.Config{NodeName: "node-a", Backup: config.BackupConfig{Enabled: true, Mode: config.BackupModeMirrorPlusFullArchive}}

	apiState := apiStateFromConfigWithStore(cfg, filepath.Join(t.TempDir(), "config.json"), 1, "running", state.NewJSONStore(filepath.Join(t.TempDir(), "state.json")))

	if !apiState.Backup.Enabled || apiState.Backup.Mode != string(config.BackupModeMirrorPlusFullArchive) {
		t.Fatalf("backup API state not exposed: %+v", apiState.Backup)
	}
}

func TestAPIStateExposesSnapshotMetadataArchiveAndCheckpointAvailability(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	archiveRoot := filepath.Join(root, "archive")
	checkpointRoot := filepath.Join(root, "checkpoints")
	cfg := config.Config{NodeName: "node-a", Backup: config.BackupConfig{Enabled: true, Mode: config.BackupModeBlockArchiveOnly, ArchivePath: "archive", CheckpointPath: "checkpoints"}}
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	createdAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	for _, marker := range []state.SnapshotMarker{
		{ID: "snap-metadata", FolderID: "docs", Cursor: 1, StateHash: "hash-1", CreatedAt: createdAt},
		{ID: "snap-archive", FolderID: "docs", Cursor: 2, StateHash: "hash-2", CreatedAt: createdAt},
	} {
		if err := store.SaveSnapshotMarker(marker); err != nil {
			t.Fatalf("save marker: %v", err)
		}
	}
	payloadHash := sha256.Sum256([]byte("payload"))
	job := state.ArchiveIntakeJob{ID: "job", SnapshotID: "snap-archive", FolderID: "docs", Path: "file.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: payloadHash[:]}, Status: "archived", CreatedAt: createdAt}
	if err := store.SaveArchiveIntakeJobs("snap-archive", []state.ArchiveIntakeJob{job}); err != nil {
		t.Fatalf("save archive job: %v", err)
	}
	hexHash := hex.EncodeToString(payloadHash[:])
	archivePath := filepath.Join(archiveRoot, "blocks", hexHash[:2], hexHash)
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write archive block: %v", err)
	}
	checkpointPath := filepath.Join(checkpointRoot, "docs", "snap-metadata.json")
	if err := os.MkdirAll(filepath.Dir(checkpointPath), 0o755); err != nil {
		t.Fatalf("mkdir checkpoint dir: %v", err)
	}
	if err := os.WriteFile(checkpointPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	apiState := apiStateFromConfigWithStore(cfg, configPath, 1, "running", store)

	if apiState.Backup.Snapshots.TotalSnapshots != 2 || apiState.Backup.Snapshots.MetadataSnapshots != 2 || apiState.Backup.Snapshots.ArchiveProtectedSnapshots != 1 || apiState.Backup.Snapshots.DBCheckpointSnapshots != 1 {
		t.Fatalf("snapshot availability not exposed separately: %+v", apiState.Backup.Snapshots)
	}
	metadataOnly := apiState.Backup.Snapshots.Items["snap-metadata"]
	if !metadataOnly.MetadataPresent || !metadataOnly.DBCheckpointAvailable || metadataOnly.ArchiveFullyProtected {
		t.Fatalf("metadata snapshot availability collapsed archive/checkpoint state: %+v", metadataOnly)
	}
}

func writeMainTestZipPackage(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(bytes))
}
