package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDebouncedManagerWaitsForQuietPeriodBeforeApplying(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	first := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]}`
	second := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendonly"}]}`
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewDebouncedManager(path, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := mgr.ReloadIfQuiet(time.Now().Add(14 * time.Second))
	if err != nil {
		t.Fatalf("unexpected error before quiet deadline: %v", err)
	}
	if changed {
		t.Fatalf("config should not apply before quiet period")
	}
	if mgr.Current().Folders[0].Mode != ModeSendReceive {
		t.Fatalf("config changed too early")
	}
	changed, err = mgr.ReloadIfQuiet(time.Now().Add(16 * time.Second))
	if err != nil {
		t.Fatalf("reload after quiet period: %v", err)
	}
	if !changed || mgr.Current().Folders[0].Mode != ModeSendOnly {
		t.Fatalf("config did not apply after quiet period")
	}
}

func TestDebouncedManagerTreatsInvalidConfigAsPartialWriteAndRetries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	first := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]}`
	bad := `{"nodeName":"node-a","folders":[`
	second := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"recvonly"}]}`
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewDebouncedManager(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := mgr.ReloadIfQuiet(time.Now().Add(2 * time.Second))
	if err == nil {
		t.Fatalf("invalid partial config should report error")
	}
	if changed || mgr.Current().Folders[0].Mode != ModeSendReceive {
		t.Fatalf("invalid config must not apply")
	}
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err = mgr.ReloadIfQuiet(time.Now().Add(4 * time.Second))
	if err != nil || !changed {
		t.Fatalf("valid retry not adopted: changed=%v err=%v", changed, err)
	}
	if mgr.Current().Folders[0].Mode != ModeReceiveOnly {
		t.Fatalf("retry config not adopted")
	}
}
