package apicontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/state"
)

func TestHandlePeerFolderDiscoveryCommandsMutateConfig(t *testing.T) {
	configPath := writeCommandConfig(t)

	peerResp, err := HandlePeerCommand(configPath, api.PeerCommandRequest{Action: "add", ID: "peer-a", Endpoint: "manual:http://127.0.0.1:22420"})
	if err != nil {
		t.Fatalf("add peer command: %v", err)
	}
	if peerResp.Status != "accepted" || peerResp.Action != "add" || peerResp.ID != "peer-a" || !strings.Contains(peerResp.Message, "hot reload") {
		t.Fatalf("unexpected peer response: %+v", peerResp)
	}

	folderResp, err := HandleFolderCommand(configPath, api.FolderCommandRequest{Action: "add", ID: "docs", Path: filepath.Join(t.TempDir(), "docs"), Mode: "sendonly"})
	if err != nil {
		t.Fatalf("add folder command: %v", err)
	}
	if folderResp.Status != "accepted" || folderResp.Action != "add" || folderResp.ID != "docs" || !strings.Contains(folderResp.Message, "hot reload") {
		t.Fatalf("unexpected folder response: %+v", folderResp)
	}

	discoveryResp, err := HandleDiscoveryCommand(configPath, api.DiscoveryCommandRequest{
		Action:            "update",
		DHT:               true,
		Local:             true,
		DHTNamespace:      "fse-test/v1",
		DHTBootstrapPeers: []string{"/ip4/127.0.0.1/tcp/4001/p2p/test"},
		NetworkHints: config.NetworkHintsConfig{
			LocalContainerGatewayIPs: []string{"172.17.0.1"},
			LocalCIDRs:               []string{"192.168.12.0/24"},
			PublishedPortMappings: []config.PublishedPortMappingConfig{{
				HostIP:        "192.168.12.10",
				HostPort:      22420,
				ContainerIP:   "172.17.0.2",
				ContainerPort: 22420,
			}},
		},
	})
	if err != nil {
		t.Fatalf("update discovery command: %v", err)
	}
	if discoveryResp.Status != "accepted" || discoveryResp.Action != "update" || !strings.Contains(discoveryResp.Message, "hot reload") {
		t.Fatalf("unexpected discovery response: %+v", discoveryResp)
	}

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("reload mutated config: %v", err)
	}
	if len(cfg.Peers) != 1 || cfg.Peers[0].ID != "peer-a" || len(cfg.Peers[0].Endpoints) != 1 || cfg.Peers[0].Endpoints[0].Kind != "manual" {
		t.Fatalf("peer command did not persist expected peer: %+v", cfg.Peers)
	}
	if len(cfg.Folders) != 1 || cfg.Folders[0].ID != "docs" || cfg.Folders[0].Mode != config.ModeSendOnly {
		t.Fatalf("folder command did not persist expected folder: %+v", cfg.Folders)
	}
	if !cfg.Discovery.DHT || !cfg.Discovery.Local || cfg.Discovery.DHTNamespace != "fse-test/v1" || len(cfg.Discovery.NetworkHints.PublishedPortMappings) != 1 {
		t.Fatalf("discovery command did not persist expected discovery config: %+v", cfg.Discovery)
	}
}

func TestFolderCommandEditsIgnorePeersAndPause(t *testing.T) {
	configPath := writeCommandConfig(t)
	folderPath := filepath.Join(t.TempDir(), "docs")
	ignore := []string{"*.tmp", "cache/"}
	shared := []string{"peer-a", "peer-a", " peer-b "}
	if _, err := HandleFolderCommand(configPath, api.FolderCommandRequest{
		Action: "add", ID: "docs", Path: folderPath, Mode: "send", Ignore: &ignore, SharedWith: &shared,
	}); err != nil {
		t.Fatalf("add folder: %v", err)
	}
	cleared := []string{}
	if _, err := HandleFolderCommand(configPath, api.FolderCommandRequest{
		Action: "update", ID: "docs", Mode: "recv", SharedWith: &cleared, Ignore: &ignore,
	}); err != nil {
		t.Fatalf("update folder: %v", err)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	folder := cfg.Folders[0]
	if folder.Mode != config.ModeReceiveOnly || len(folder.SharedWith) != 0 || len(folder.Ignore) != 2 || folder.Ignore[0] != "*.tmp" {
		t.Fatalf("folder edit = %+v", folder)
	}
	if _, err := HandleFolderCommand(configPath, api.FolderCommandRequest{Action: "pause", ID: "docs"}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	cfg, err = config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("reload paused: %v", err)
	}
	if cfg.Folders[0].Enabled {
		t.Fatal("paused folder is still enabled")
	}
	if _, err := HandleFolderCommand(configPath, api.FolderCommandRequest{Action: "resume", ID: "docs"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	cfg, err = config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("reload resumed: %v", err)
	}
	if !cfg.Folders[0].Enabled || cfg.Folders[0].Mode != config.ModeReceiveOnly {
		t.Fatalf("resume changed the folder: %+v", cfg.Folders[0])
	}
}

func TestFolderCommandRescanIndexesTheFolder(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "docs")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderPath, "seed.txt"), []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	cfg := config.Config{
		NodeName: "node-a",
		Folders:  []config.FolderConfig{{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive, BlockSize: 4096}},
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	resp, err := HandleFolderCommand(configPath, api.FolderCommandRequest{Action: "rescan", ID: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Action != "rescan" || resp.ID != "docs" || resp.Status != "accepted" || resp.Message != "folder rescan accepted" {
		t.Fatalf("response = %+v", resp)
	}
	store := state.NewJSONStore(metadatastore.DefaultStatePath(configPath))
	defer store.Close()
	if _, ok, err := store.LoadManifest("docs", "seed.txt"); err != nil || !ok {
		t.Fatalf("stored manifest missing after rescan: ok=%v err=%v", ok, err)
	}
}

func TestHandleConfigCommandsRejectUnsupportedActions(t *testing.T) {
	configPath := writeCommandConfig(t)
	if _, err := HandlePeerCommand(configPath, api.PeerCommandRequest{Action: "bogus", ID: "peer-a"}); err == nil || !strings.Contains(err.Error(), "unsupported peer command action") {
		t.Fatalf("unsupported peer action should fail with clear error, got %v", err)
	}
	if _, err := HandleFolderCommand(configPath, api.FolderCommandRequest{Action: "bogus", ID: "docs"}); err == nil || !strings.Contains(err.Error(), "unsupported folder command action") {
		t.Fatalf("unsupported folder action should fail with clear error, got %v", err)
	}
	if _, err := HandleDiscoveryCommand(configPath, api.DiscoveryCommandRequest{Action: "add"}); err == nil || !strings.Contains(err.Error(), "unsupported discovery command action") {
		t.Fatalf("unsupported discovery action should fail with clear error, got %v", err)
	}
}

func writeCommandConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{NodeName: "node-a"}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := config.WriteFileAtomic(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
