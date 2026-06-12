package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFolderMonitorEmitsWatcherAndDebouncedScanEvents(t *testing.T) {
	root := t.TempDir()
	events := make(chan Event, 8)
	mon, err := New([]Folder{{ID: "docs", Path: root}}, Options{EventDebounce: 50 * time.Millisecond, FallbackInterval: time.Hour}, func(event Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mon.Close()

	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	seenWatch := false
	seenScan := false
	deadline := time.After(2 * time.Second)
	for !(seenWatch && seenScan) {
		select {
		case event := <-events:
			if event.Type == "watch.event" && event.FolderID == "docs" {
				seenWatch = true
			}
			if event.Type == "scan.due" && event.FolderID == "docs" {
				seenScan = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for watch+scan events; watch=%v scan=%v", seenWatch, seenScan)
		}
	}
}
