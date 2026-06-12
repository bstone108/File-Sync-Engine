package structuredlog

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
)

func TestConfiguredLevelFiltersLowerSeverityEvents(t *testing.T) {
	var logs bytes.Buffer
	oldLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(oldLogOutput)
		Reset()
	})

	if err := Configure(Options{Level: "warn"}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	Event("info", "daemon.started", "started", nil)
	Event("warn", "config.reload.rejected", "rejected", nil)

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, logs=%q", len(lines), logs.String())
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("structured log is not JSON: %s", lines[0])
	}
	if record["event"] != "config.reload.rejected" || record["level"] != "warn" {
		t.Fatalf("unexpected record after level filter: %+v", record)
	}
}

func TestConfiguredOutputFileReceivesStructuredEvents(t *testing.T) {
	path := t.TempDir() + "/fse.log"
	t.Cleanup(Reset)

	if err := Configure(Options{Level: "info", Output: path}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	Event("info", "daemon.started", "started", nil)
	Reset()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), `"event":"daemon.started"`) {
		t.Fatalf("configured output file missing event: %s", data)
	}
}
