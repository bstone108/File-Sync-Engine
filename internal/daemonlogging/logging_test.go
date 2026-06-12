package daemonlogging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/structuredlog"
)

func TestOperationalLogHelpersEmitStructuredJSON(t *testing.T) {
	var logs bytes.Buffer
	oldLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(oldLogOutput)
		structuredlog.Reset()
	})

	DaemonStarted("node-a", 2, "/tmp/fse.json")
	APIListening("127.0.0.1:22000")
	ConfigReloadRejected(fmt.Errorf("invalid partial config"))
	DiscoveryRouterUnavailable(fmt.Errorf("bootstrap failed"))

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

func TestConfigureUsesConfigRelativeOutputAndLevel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "fse.json")
	t.Cleanup(structuredlog.Reset)

	cfg := config.Config{Logging: config.LoggingConfig{Level: config.LogLevelError, Output: "logs/fse.jsonl"}}
	if err := Configure(cfg, configPath); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	ConfigReloadRejected(fmt.Errorf("warn should be filtered"))
	ReloadedMetadataStoreOpenFailed(fmt.Errorf("boom"))
	structuredlog.Reset()

	data, err := os.ReadFile(filepath.Join(dir, "logs", "fse.jsonl"))
	if err != nil {
		t.Fatalf("read configured log: %v", err)
	}
	if strings.Contains(string(data), "config.reload.rejected") || !strings.Contains(string(data), "metadata.store.reload_failed") {
		t.Fatalf("configured logging did not honor level/output: %s", data)
	}
}
