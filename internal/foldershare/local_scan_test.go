package foldershare

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestLocalIndexAdvancesWhenNoPeerIsReachable(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		NodeName: "kiki",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendReceive,
		}},
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	Reconcile(cfg, configPath, nil)
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("local-only"), 0o644); err != nil {
		t.Fatal(err)
	}
	RequestFullScan("shared")
	Reconcile(cfg, configPath, nil)
	entries, err := LoadJournal(configPath, "shared")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Path == "new.txt" && !entry.Deleted && string(entry.SHA256) != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("local file was not indexed without a peer: %+v", entries)
	}
}
