package maintenance

import (
	"path/filepath"
	"testing"
)

func TestFileCheckpointPersistsCursorAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance-cursor.json")
	checkpoint := FileCheckpoint{Path: path}
	if err := checkpoint.SaveMaintenanceCursor(Cursor{Position: 42}); err != nil {
		t.Fatalf("SaveMaintenanceCursor: %v", err)
	}

	reloaded := FileCheckpoint{Path: path}
	cursor, err := reloaded.LoadMaintenanceCursor()
	if err != nil {
		t.Fatalf("LoadMaintenanceCursor: %v", err)
	}
	if cursor.Position != 42 {
		t.Fatalf("position=%d, want 42", cursor.Position)
	}
}

func TestFileCheckpointMissingFileStartsAtZero(t *testing.T) {
	checkpoint := FileCheckpoint{Path: filepath.Join(t.TempDir(), "missing.json")}
	cursor, err := checkpoint.LoadMaintenanceCursor()
	if err != nil {
		t.Fatalf("LoadMaintenanceCursor missing: %v", err)
	}
	if cursor.Position != 0 {
		t.Fatalf("position=%d, want 0", cursor.Position)
	}
}
