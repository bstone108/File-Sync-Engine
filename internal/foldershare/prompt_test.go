package foldershare

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestChangedPathDoesNotRescanOrDeleteTheRest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "edit.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "node.json")
	cfg := config.Config{NodeName: "node", Folders: []config.FolderConfig{{
		ID: "prompt", Path: root, Enabled: true, Mode: config.ModeSendReceive,
	}}}
	Reconcile(cfg, configPath, nil)
	_, cursor, _, err := JournalDelta(configPath, "prompt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "edit.txt"), []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	NoteChanged("prompt", filepath.Join(root, "edit.txt"))
	Reconcile(cfg, configPath, nil)
	entries, err := LoadJournal(configPath, "prompt")
	if err != nil {
		t.Fatal(err)
	}
	byPath := indexEntries(entries)
	if byPath["edit.txt"].Deleted || byPath["edit.txt"].Size != int64(len("after")) {
		t.Fatalf("edited entry = %+v", byPath["edit.txt"])
	}
	if byPath["keep.txt"].Deleted || byPath["keep.txt"].Size != int64(len("keep")) {
		t.Fatalf("untouched entry was rescanned: %+v", byPath["keep.txt"])
	}
	changed, _, delta, err := JournalDelta(configPath, "prompt", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !delta || len(changed) != 1 || changed[0].Path != "edit.txt" {
		t.Fatalf("delta = %+v", changed)
	}
}
