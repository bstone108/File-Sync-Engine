package foldershare

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/config"
)

func TestJournalDeltaSendsOnlyTheChangedEntry(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "node.json")
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{NodeName: "node", Folders: []config.FolderConfig{{
		ID: "docs", Path: root, Enabled: true, Mode: config.ModeSendReceive,
	}}}
	Reconcile(cfg, configPath, nil)
	full, cursor, delta, err := JournalDelta(configPath, "docs", 0)
	if err != nil {
		t.Fatal(err)
	}
	if delta || cursor == 0 || len(full) != 1 {
		t.Fatalf("full journal cursor=%d delta=%v entries=%d", cursor, delta, len(full))
	}
	Wake()
	if err := os.WriteFile(filepath.Join(root, "two.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	Reconcile(cfg, configPath, nil)
	changed, next, delta, err := JournalDelta(configPath, "docs", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !delta || next <= cursor || len(changed) != 1 || changed[0].Path != "two.txt" {
		t.Fatalf("delta cursor=%d delta=%v entries=%+v", next, delta, changed)
	}
}

func TestUnchangedJournalIsNotRewritten(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "node.json")
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{NodeName: "node", Folders: []config.FolderConfig{{
		ID: "quiet", Path: root, Enabled: true, Mode: config.ModeSendReceive,
	}}}
	Reconcile(cfg, configPath, nil)
	info, err := os.Stat(JournalPath(configPath, "quiet"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	Reconcile(cfg, configPath, nil)
	again, err := os.Stat(JournalPath(configPath, "quiet"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(info.ModTime()) || again.Size() != info.Size() {
		t.Fatalf("journal rewritten: %s -> %s", info.ModTime(), again.ModTime())
	}
}
