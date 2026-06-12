package apicontrol

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
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
