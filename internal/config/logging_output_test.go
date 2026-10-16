package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLogOutputUsesEnvThenConfigDirThenStderr(t *testing.T) {
	t.Setenv("FSE_LOG_OUTPUT", "/tmp/from-env.jsonl")
	if got := DefaultLogOutput(); got != "/tmp/from-env.jsonl" {
		t.Fatalf("env output = %q", got)
	}
	t.Setenv("FSE_LOG_OUTPUT", "")
	if _, err := os.Stat("/config"); err == nil {
		if got := DefaultLogOutput(); got != "/config/logs/fse.jsonl" && got != "stderr" {
			t.Fatalf("output = %q", got)
		}
	}
}

func TestPersistLoggingOutputRepairsAnEmptyPath(t *testing.T) {
	t.Setenv("FSE_LOG_OUTPUT", "/tmp/persisted.jsonl")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{NodeName: "node-a", Logging: LoggingConfig{Level: LogLevelInfo}}
	repaired, err := PersistLoggingOutput(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Logging.Output != "/tmp/persisted.jsonl" {
		t.Fatalf("output = %q", repaired.Logging.Output)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Logging.Output != "/tmp/persisted.jsonl" || loaded.Logging.Level != LogLevelInfo {
		t.Fatalf("loaded = %+v", loaded.Logging)
	}
	again, err := PersistLoggingOutput(path, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if again.Logging.Output != loaded.Logging.Output {
		t.Fatalf("second persist changed output to %q", again.Logging.Output)
	}
}
