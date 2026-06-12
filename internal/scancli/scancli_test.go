package scancli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/state"
)

func TestRunConfiguredLoadsConfigScansAndReturnsCLIOutput(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "docs")
	if err := writeFile(filepath.Join(folderPath, "seed.txt"), []byte("seed-data")); err != nil {
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

	output, err := RunConfigured(configPath, "docs")
	if err != nil {
		t.Fatal(err)
	}
	want := "scan finished: folder=docs changed=1 deleted=0\nscan summary: folders=1 state=" + metadatastore.DefaultStatePath(configPath) + "\n"
	if output != want {
		t.Fatalf("output = %q, want %q", output, want)
	}

	store := state.NewJSONStore(metadatastore.DefaultStatePath(configPath))
	if _, ok, err := store.LoadManifest("docs", "seed.txt"); err != nil || !ok {
		t.Fatalf("stored manifest missing after configured scan: ok=%v err=%v", ok, err)
	}
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
