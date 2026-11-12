package apicontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configsync"
	"filesyncengine/internal/foldershare"
)

// TestFivePeerIdentityShareAndRestart is the five-engine harness for the live
// set. Identity A is the Mac desktop client, the Windows desktop client, and
// two Unraid engines. Identity B is the third Unraid engine and joins only by
// the folder share code. CI runs five Linux engines for those roles. The Mac
// and Windows apps call this same daemon and do not keep a second policy.
func TestFivePeerIdentityShareAndRestart(t *testing.T) {
	const (
		group   = "identity-a"
		token   = "identity-a-bootstrap-token-must-be-43-characters"
		guestID = "unraid-share"
		secret  = "guest-sync-secret"
	)
	names := []string{"mac", "windows", "unraid-a", "unraid-b"}
	roots := map[string]string{}
	configs := map[string]string{}
	for _, name := range append(append([]string{}, names...), guestID) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "shared"), 0o755); err != nil {
			t.Fatal(err)
		}
		if name != guestID {
			if err := os.MkdirAll(filepath.Join(root, "private"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		roots[name] = root
		configs[name] = filepath.Join(t.TempDir(), "config.json")
	}

	type node struct {
		name   string
		key    string
		server *httptest.Server
		pin    string
	}
	nodes := map[string]*node{}
	start := func(name, key string) *node {
		body := engineConfig(t, name, key, roots[name], nil)
		if err := os.WriteFile(configs[name], body, 0o600); err != nil {
			t.Fatal(err)
		}
		server := api.NewServer(api.State{NodeName: name, ConfigPath: configs[name]}, key)
		tls := httptest.NewTLSServer(server.Router())
		t.Cleanup(tls.Close)
		sum := sha256.Sum256(tls.Certificate().Raw)
		return &node{name: name, key: key, server: tls, pin: hex.EncodeToString(sum[:])}
	}
	for _, name := range names {
		nodes[name] = start(name, "key-"+name)
	}
	nodes[guestID] = start(guestID, "key-guest")

	peer := func(id, key, address, pin, groupID, peerSecret string) config.PeerConfig {
		return config.PeerConfig{
			ID: id, APIKey: key, PeerSecret: peerSecret, IdentityGroupID: groupID,
			APICertificateSHA256: pin,
			Endpoints: []config.EndpointConfig{{
				Kind: "manual", Address: address, NetworkHint: "local", Authenticated: true,
			}},
		}
	}
	write := func(name string, peers []config.PeerConfig, folders []config.FolderConfig) {
		body := engineConfig(t, name, nodes[name].key, roots[name], &engineBodies{peers: peers, folders: folders, group: group, token: token, guest: name == guestID})
		if err := os.WriteFile(configs[name], body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var identityPeers []config.PeerConfig
	for _, name := range names {
		identityPeers = append(identityPeers, peer(name, nodes[name].key, nodes[name].server.URL, nodes[name].pin, group, ""))
	}
	guestPeer := peer(guestID, secret, nodes[guestID].server.URL, nodes[guestID].pin, "", secret)
	shared := func(path string) config.FolderConfig {
		return config.FolderConfig{ID: "shared", Path: filepath.Join(path, "shared"), Enabled: true, Mode: config.ModeSendReceive, SharedWith: []string{guestID, "mac", "windows", "unraid-a", "unraid-b"}}
	}
	private := func(path string) config.FolderConfig {
		return config.FolderConfig{ID: "private", Path: filepath.Join(path, "private"), Enabled: true, Mode: config.ModeSendReceive}
	}
	for _, name := range names {
		var peers []config.PeerConfig
		for _, other := range identityPeers {
			if other.ID != name {
				peers = append(peers, other)
			}
		}
		peers = append(peers, guestPeer)
		write(name, peers, []config.FolderConfig{shared(roots[name]), private(roots[name])})
	}
	var guestPeers []config.PeerConfig
	for _, other := range identityPeers {
		guestPeers = append(guestPeers, peer(other.ID, secret, other.Endpoints[0].Address, other.APICertificateSHA256, "", secret))
	}
	write(guestID, guestPeers, []config.FolderConfig{shared(roots[guestID])})

	load := func(name string) config.Config {
		t.Helper()
		cfg, err := config.LoadFile(configs[name])
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	if err := os.WriteFile(filepath.Join(roots["unraid-a"], "shared", "note.txt"), []byte("from-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	foldershare.RequestFullScan("shared")
	foldershare.Reconcile(load("unraid-a"), configs["unraid-a"], nil)
	for _, name := range []string{"mac", "windows", "unraid-b", guestID} {
		foldershare.Reconcile(load(name), configs[name], nil)
		got, err := os.ReadFile(filepath.Join(roots[name], "shared", "note.txt"))
		if err != nil || string(got) != "from-a" {
			t.Fatalf("%s note = %q %v", name, got, err)
		}
	}

	if err := os.Rename(filepath.Join(roots["windows"], "shared", "note.txt"), filepath.Join(roots["windows"], "shared", "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	foldershare.RequestFullScan("shared")
	foldershare.Reconcile(load("windows"), configs["windows"], nil)
	for _, name := range []string{"mac", "unraid-a", "unraid-b", guestID} {
		foldershare.Reconcile(load(name), configs[name], nil)
		if _, err := os.Stat(filepath.Join(roots[name], "shared", "note.txt")); !os.IsNotExist(err) {
			t.Fatalf("%s still has the old name: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(roots[name], "shared", "renamed.txt"))
		if err != nil || string(got) != "from-a" {
			t.Fatalf("%s rename = %q %v", name, got, err)
		}
	}

	if err := os.Remove(filepath.Join(roots["unraid-b"], "shared", "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	foldershare.RequestFullScan("shared")
	foldershare.Reconcile(load("unraid-b"), configs["unraid-b"], nil)
	for _, name := range []string{"mac", "windows", "unraid-a", guestID} {
		foldershare.Reconcile(load(name), configs[name], nil)
		if _, err := os.Stat(filepath.Join(roots[name], "shared", "renamed.txt")); !os.IsNotExist(err) {
			t.Fatalf("%s did not delete renamed.txt: %v", name, err)
		}
	}

	payload := bytes.Repeat([]byte("block-mesh-"), foldershare.TransferBlockSize/2)
	if err := os.WriteFile(filepath.Join(roots["mac"], "shared", "large.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	foldershare.RequestFullScan("shared")
	foldershare.Reconcile(load("mac"), configs["mac"], nil)
	for _, name := range []string{"windows", "unraid-a", "unraid-b", guestID} {
		foldershare.Reconcile(load(name), configs[name], nil)
		got, err := os.ReadFile(filepath.Join(roots[name], "shared", "large.bin"))
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("%s large file mismatch %v len=%d", name, err, len(got))
		}
	}

	if err := os.WriteFile(filepath.Join(roots[guestID], "shared", "from-guest.txt"), []byte("edited-on-share-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	foldershare.RequestFullScan("shared")
	foldershare.Reconcile(load(guestID), configs[guestID], nil)
	for _, name := range names {
		foldershare.Reconcile(load(name), configs[name], nil)
		got, err := os.ReadFile(filepath.Join(roots[name], "shared", "from-guest.txt"))
		if err != nil || string(got) != "edited-on-share-only" {
			t.Fatalf("%s guest edit = %q %v", name, got, err)
		}
	}

	nodes["windows"].server.Close()
	if err := os.WriteFile(filepath.Join(roots["unraid-a"], "shared", "after-restart.txt"), []byte("caught-up"), 0o644); err != nil {
		t.Fatal(err)
	}
	foldershare.RequestFullScan("shared")
	foldershare.Reconcile(load("unraid-a"), configs["unraid-a"], nil)
	foldershare.Reconcile(load("windows"), configs["windows"], nil)
	caught, err := os.ReadFile(filepath.Join(roots["windows"], "shared", "after-restart.txt"))
	if err != nil || string(caught) != "caught-up" {
		t.Fatalf("windows client after restart = %q %v", caught, err)
	}

	privateReq, err := http.NewRequest(http.MethodGet, nodes["unraid-a"].server.URL+"/v1/sync-journal?folder=private", nil)
	if err != nil {
		t.Fatal(err)
	}
	privateReq.Header.Set("X-FSE-API-Key", secret)
	privateResp, err := nodes["unraid-a"].server.Client().Do(privateReq)
	if err != nil {
		t.Fatal(err)
	}
	defer privateResp.Body.Close()
	if privateResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(privateResp.Body)
		t.Fatalf("share-only private journal = %d %s", privateResp.StatusCode, body)
	}
	configReq, err := http.NewRequest(http.MethodGet, nodes["unraid-a"].server.URL+"/v1/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	configReq.Header.Set("X-FSE-API-Key", secret)
	configResp, err := nodes["unraid-a"].server.Client().Do(configReq)
	if err != nil {
		t.Fatal(err)
	}
	defer configResp.Body.Close()
	if configResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("share-only config = %d", configResp.StatusCode)
	}

	origin, err := HandleConfigSync(configs["mac"])
	if err != nil {
		t.Fatal(err)
	}
	target, err := HandleConfigSync(configs["windows"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleConfigSyncEdit(configs["unraid-b"], []byte(`{"targetInstanceId":"`+target.LocalInstanceID+`","path":"logging.level","value":"debug"}`)); err != nil {
		t.Fatalf("identity management edit: %v", err)
	}
	originReplica := syncReplicaFile(t, configs["unraid-b"])
	merged, err := HandleConfigSyncMerge(configs["windows"], mustJSON(t, map[string]any{"replica": originReplica}))
	if err != nil {
		t.Fatalf("identity merge: %v", err)
	}
	if merged.LocalInstanceID != target.LocalInstanceID {
		t.Fatalf("merge local = %s", merged.LocalInstanceID)
	}
	if loggingLevelFile(t, configs["windows"]) != "debug" {
		t.Fatalf("managed level = %s", loggingLevelFile(t, configs["windows"]))
	}
	if loggingLevelFile(t, configs["unraid-b"]) == "debug" {
		t.Fatal("origin adopted the target setting")
	}
	guestView, err := HandleConfigSync(configs[guestID])
	if err != nil {
		t.Fatal(err)
	}
	if guestView.GroupID == origin.GroupID {
		t.Fatalf("guest group %s matched identity A", guestView.GroupID)
	}
	if _, err := HandleConfigSyncMerge(configs[guestID], mustJSON(t, map[string]any{"replica": originReplica})); err == nil || !strings.Contains(err.Error(), "group mismatch") {
		t.Fatalf("share-only adopted identity config: %v", err)
	}
}

func syncReplicaFile(t *testing.T, configPath string) configsync.Replica {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "config-sync.json"))
	if err != nil {
		t.Fatal(err)
	}
	var replica configsync.Replica
	if err := json.Unmarshal(raw, &replica); err != nil {
		t.Fatal(err)
	}
	return replica
}

func loggingLevelFile(t *testing.T, configPath string) string {
	t.Helper()
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(cfg.Logging.Level)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type engineBodies struct {
	peers   []config.PeerConfig
	folders []config.FolderConfig
	group   string
	token   string
	guest   bool
}

func engineConfig(t *testing.T, name, key, root string, extra *engineBodies) []byte {
	t.Helper()
	group := "identity-a"
	token := "identity-a-bootstrap-token-must-be-43-characters"
	if extra != nil && extra.guest {
		group = "identity-b"
		token = "identity-b-bootstrap-token-must-be-43-characters"
	}
	cfg := config.Config{
		NodeName: name,
		API:      config.APIConfig{Listen: "127.0.0.1:0", Key: key, AdvertiseURL: "https://" + name + ":22420"},
		Identity: config.IdentityConfig{
			PublicKey:       "public-" + name,
			PrivateKey:      "private-" + name,
			EncryptionLevel: 4,
			Groups:          []config.IdentityGroupConfig{{ID: group, Token: token, Enabled: true}},
		},
		Logging:   config.LoggingConfig{Level: config.LogLevelInfo, Output: "stderr"},
		Discovery: config.DiscoveryConfig{Disabled: true},
	}
	if extra != nil {
		cfg.Peers = extra.peers
		cfg.Folders = extra.folders
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
