package metabench

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBBoltCandidateCreatesDatabaseDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "bbolt")
	store, err := BBoltCandidate{}.Open(path)
	if err != nil {
		t.Fatalf("open bbolt in missing directory: %v", err)
	}
	defer store.Close()
	if err := store.ImportFiles(context.Background(), 1, 1, 1); err != nil {
		t.Fatalf("import through bbolt store: %v", err)
	}
}
