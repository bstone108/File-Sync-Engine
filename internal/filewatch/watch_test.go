package filewatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherReportsFileChangesRecursively(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	watcher, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	if err := os.WriteFile(filepath.Join(nested, "file.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-watcher.Events():
		if event.Path == "" {
			t.Fatalf("empty event path: %+v", event)
		}
	case err := <-watcher.Errors():
		t.Fatalf("watch error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for file watch event")
	}
}
