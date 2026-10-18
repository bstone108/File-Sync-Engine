package apicontrol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configsync"
	"filesyncengine/internal/link"
	"filesyncengine/internal/linkstats"
	"filesyncengine/internal/peeridentity"
)

func apiLogging(level config.LogLevel) api.ConfigUpdateRequest {
	return api.ConfigUpdateRequest{Logging: &config.LoggingConfig{Level: level, Output: "stderr"}}
}

func writeSyncConfig(t *testing.T, name, level string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"nodeName":"` + name + `",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret","advertiseURL":"https://` + name + `:22420"},
		"identity":{"privateKey":"private-secret","publicKey":"public-` + name + `","encryptionLevel":4},
		"logging":{"level":"` + level + `","output":"stderr"},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func syncReplica(t *testing.T, configPath string) configsync.Replica {
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

func loggingLevel(t *testing.T, configPath string) string {
	t.Helper()
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(cfg.Logging.Level)
}

func TestConfigSyncViewIncludesPeerTransferStats(t *testing.T) {
	path := writeSyncConfig(t, "engine-stats", "info")
	linkstats.Default.SetPending("engine-stats", 2048)
	t.Cleanup(func() { linkstats.Default.SetPending("engine-stats", 0) })

	view, err := HandleConfigSync(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instances) == 0 || !view.Instances[0].Local {
		t.Fatalf("instances = %+v", view.Instances)
	}
	if view.Instances[0].PendingBytes != 2048 {
		t.Fatalf("local pending bytes = %d", view.Instances[0].PendingBytes)
	}
	raw, err := json.Marshal(view.Instances[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"averageBytesPerSecond"`) || !strings.Contains(string(raw), `"pendingBytes"`) {
		t.Fatalf("instance json = %s", raw)
	}
}

func TestConfigSyncDoesNotAdoptAnotherInstancesSettings(t *testing.T) {
	originPath := writeSyncConfig(t, "engine-a", "warn")
	relayPath := writeSyncConfig(t, "engine-b", "info")
	targetPath := writeSyncConfig(t, "engine-c", "error")

	originView, err := HandleConfigSync(originPath)
	if err != nil {
		t.Fatal(err)
	}
	relayView, err := HandleConfigSync(relayPath)
	if err != nil {
		t.Fatal(err)
	}
	targetView, err := HandleConfigSync(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := configsync.ValidateInstanceID(originView.LocalInstanceID); err != nil {
		t.Fatal(err)
	}
	if originView.LocalInstanceID == relayView.LocalInstanceID || relayView.LocalInstanceID == targetView.LocalInstanceID {
		t.Fatalf("instance ids collided: %s %s %s", originView.LocalInstanceID, relayView.LocalInstanceID, targetView.LocalInstanceID)
	}

	queued, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"`+targetView.LocalInstanceID+`","path":"logging.level","value":"debug"}`))
	if err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, instance := range queued.Instances {
		if instance.InstanceID == targetView.LocalInstanceID {
			pending = instance.PendingChanges
			if instance.Online {
				t.Fatal("target was marked online before it published a connection")
			}
			if instance.Settings["logging.level"] != nil {
				t.Fatalf("origin stored the queued value in the target record: %#v", instance.Settings)
			}
		}
		if instance.Local && instance.Settings["logging.level"] != nil {
			t.Fatalf("origin own settings changed while editing the target: %#v", instance.Settings)
		}
	}
	if pending != 1 {
		t.Fatalf("origin pending count = %d view=%+v", pending, queued.Instances)
	}
	if loggingLevel(t, originPath) != "warn" {
		t.Fatalf("origin config logging = %s", loggingLevel(t, originPath))
	}

	raw, err := os.ReadFile(filepath.Join(filepath.Dir(originPath), "config-sync.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "api-secret") || strings.Contains(string(raw), "private-secret") {
		t.Fatal("config sync database stored a secret")
	}
	relayed, err := HandleConfigSyncMerge(relayPath, json.RawMessage(`{"replica":`+string(raw)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, relayPath) != "info" {
		t.Fatalf("relay adopted the queued logging level: %s", loggingLevel(t, relayPath))
	}
	relayReplica := syncReplica(t, relayPath)
	if relayReplica.OwnRecord().Fields["logging.level"].Value != nil && relayReplica.OwnRecord().Fields["logging.level"].Value != "" {
		t.Fatalf("relay own record = %#v", relayReplica.OwnRecord())
	}
	if _, ok := relayReplica.Records[targetView.LocalInstanceID]; ok {
		t.Fatalf("relay wrote the target record before the target applied the edit: %#v", relayReplica.Records[targetView.LocalInstanceID])
	}

	relayRaw, err := json.Marshal(map[string]any{"replica": relayed.Replica})
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := HandleConfigSyncMerge(targetPath, relayRaw)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.LocalInstanceID != targetView.LocalInstanceID {
		t.Fatalf("target local id = %s", delivered.LocalInstanceID)
	}
	if loggingLevel(t, targetPath) != "debug" {
		t.Fatalf("target did not apply its own queued edit, logging = %s", loggingLevel(t, targetPath))
	}
	if loggingLevel(t, originPath) != "warn" || loggingLevel(t, relayPath) != "info" {
		t.Fatalf("another engine's config changed: origin=%s relay=%s", loggingLevel(t, originPath), loggingLevel(t, relayPath))
	}
	targetReplica := syncReplica(t, targetPath)
	if targetReplica.OwnRecord().Fields["logging.level"].Value != "debug" {
		t.Fatalf("target own record = %#v", targetReplica.OwnRecord())
	}

	published, err := json.Marshal(map[string]any{"replica": delivered.Replica})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleConfigSyncMerge(relayPath, published); err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, relayPath) != "info" {
		t.Fatalf("relay adopted the target setting after it was published: %s", loggingLevel(t, relayPath))
	}
	relayReplica = syncReplica(t, relayPath)
	if relayReplica.Records[targetView.LocalInstanceID].Fields["logging.level"].Value != "debug" {
		t.Fatalf("relay did not keep the target record for its GUI: %#v", relayReplica.Records[targetView.LocalInstanceID])
	}
	if value, ok := relayReplica.OwnRecord().Fields["logging.level"]; ok && value.Value != nil {
		t.Fatalf("relay own record changed after storing the target: %#v", relayReplica.OwnRecord())
	}

	if _, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"`+targetView.LocalInstanceID+`","path":"apiKey","value":"nope"}`)); err == nil {
		t.Fatal("secret path was accepted")
	}
	if _, err := HandleConfigSyncEdit(originPath, json.RawMessage(`{"targetInstanceId":"`+targetView.LocalInstanceID+`","path":"shared/logging.level","value":"debug"}`)); err == nil {
		t.Fatal("shared path was accepted")
	}
}

func TestLocalConfigUpdatePublishesOnlyTheLocalRecord(t *testing.T) {
	originPath := writeSyncConfig(t, "engine-a", "info")
	otherPath := writeSyncConfig(t, "engine-b", "warn")
	level := config.LogLevelDebug
	if _, err := HandleConfigUpdate(originPath, apiLogging(level)); err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, originPath) != "debug" {
		t.Fatalf("origin logging = %s", loggingLevel(t, originPath))
	}
	origin := syncReplica(t, originPath)
	if origin.OwnRecord().Fields["logging.level"].Value != "debug" {
		t.Fatalf("own record was not published: %#v", origin.OwnRecord())
	}
	raw, err := json.Marshal(map[string]any{"replica": origin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleConfigSyncMerge(otherPath, raw); err != nil {
		t.Fatal(err)
	}
	if loggingLevel(t, otherPath) != "warn" {
		t.Fatalf("engine B adopted engine A's logging level: %s", loggingLevel(t, otherPath))
	}
	other := syncReplica(t, otherPath)
	if other.Records[origin.InstanceID].Fields["logging.level"].Value != "debug" {
		t.Fatalf("engine B did not store engine A's record: %#v", other.Records[origin.InstanceID])
	}
	if value := other.OwnRecord().Fields["logging.level"].Value; value != nil && value != "" {
		t.Fatalf("engine B own record = %#v", other.OwnRecord())
	}
}

func TestOnboardingIssuesLocallyAndRelaysToAnotherInstance(t *testing.T) {
	identity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"nodeName":"engine-onboard",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret","advertiseURL":"http://engine-onboard:22420"},
		"identity":{"privateKey":"` + identity.PrivateKey + `","publicKey":"` + identity.PublicKey + `","encryptionLevel":4},
		"logging":{"level":"info","output":"stderr"},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	SetLinkService(link.NewService(link.NewMemoryDirectory()))
	t.Cleanup(func() { SetLinkService(link.NewService(link.UnavailableDirectory{})) })

	view, err := HandleConfigSync(path)
	if err != nil {
		t.Fatal(err)
	}
	localRaw, err := json.Marshal(map[string]string{"targetInstanceId": view.LocalInstanceID, "action": "issue"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := HandleOnboarding(context.Background(), path, localRaw)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Command.Status != configsync.OnboardingDone || strings.Count(issued.Command.Code, "-") != 3 {
		t.Fatalf("local issue = %+v", issued.Command)
	}
	if !strings.Contains(issued.Command.Message, "is ready") {
		t.Fatalf("message = %q", issued.Command.Message)
	}

	remoteID := "44444444-4444-4444-8444-444444444444"
	remoteRaw, err := json.Marshal(map[string]string{
		"targetInstanceId": remoteID,
		"action":           "accept",
		"code":             "ABCD-EFGH-JKMN-PQRS",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := HandleOnboarding(context.Background(), path, remoteRaw)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Command.Status != configsync.OnboardingPending || queued.Command.TargetInstanceID != remoteID || queued.Command.Code != "ABCD-EFGH-JKMN-PQRS" {
		t.Fatalf("remote accept = %+v", queued.Command)
	}
	stored := syncReplica(t, path)
	if stored.InstanceID == remoteID {
		t.Fatal("relay adopted the target id")
	}
	for _, command := range stored.Onboarding {
		if command.ID == queued.Command.ID && command.Status != configsync.OnboardingPending {
			t.Fatalf("relay executed another instance's onboarding: %+v", command)
		}
	}
}

func TestConfigSyncPollDoesNotIssueAgainWhileClaimIsInFlight(t *testing.T) {
	identity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"nodeName":"engine-onboard",
		"listen":[],
		"api":{"listen":"127.0.0.1:0","key":"api-secret","advertiseURL":"http://engine-onboard:22420"},
		"identity":{"privateKey":"` + identity.PrivateKey + `","publicKey":"` + identity.PublicKey + `","encryptionLevel":4},
		"logging":{"level":"info","output":"stderr"},
		"discovery":{"disabled":true,"dht":false,"local":false},
		"peers":[],
		"folders":[]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	SetLinkService(link.NewService(link.NewMemoryDirectory()))
	t.Cleanup(func() { SetLinkService(link.NewService(link.UnavailableDirectory{})) })
	if _, err := HandleConfigSync(path); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	previousRelays := claimRelays
	claimRelays = func() []string {
		calls.Add(1)
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	t.Cleanup(func() { claimRelays = previousRelays })

	view, err := HandleConfigSync(path)
	if err != nil {
		t.Fatal(err)
	}
	localRaw, err := json.Marshal(map[string]string{"targetInstanceId": view.LocalInstanceID, "action": "issue"})
	if err != nil {
		t.Fatal(err)
	}
	issued := make(chan OnboardingResponse, 1)
	issueErr := make(chan error, 1)
	go func() {
		response, err := HandleOnboarding(context.Background(), path, localRaw)
		if err != nil {
			issueErr <- err
			return
		}
		issued <- response
	}()
	select {
	case <-entered:
	case err := <-issueErr:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("pairing issue did not start")
	}
	pollErr := make(chan error, 1)
	go func() {
		_, err := HandleConfigSync(path)
		pollErr <- err
	}()
	select {
	case err := <-pollErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("config sync poll blocked on the in-flight pairing issue")
	}
	close(release)
	var response OnboardingResponse
	select {
	case response = <-issued:
	case err := <-issueErr:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("pairing issue did not finish")
	}
	if calls.Load() != 1 {
		t.Fatalf("pairing issues started = %d, want 1", calls.Load())
	}
	if response.Command.Status != configsync.OnboardingDone {
		t.Fatalf("issue = %+v", response.Command)
	}
	again, err := HandleConfigSync(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, command := range again.Onboarding {
		if command.ID != response.Command.ID {
			continue
		}
		found = true
		if command.Status != configsync.OnboardingDone || command.Code != response.Command.Code {
			t.Fatalf("poll changed the finished code: %+v", command)
		}
	}
	if !found {
		t.Fatalf("finished onboarding missing after poll: %+v", again.Onboarding)
	}
}
