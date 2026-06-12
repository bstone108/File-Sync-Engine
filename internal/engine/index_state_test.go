package engine

import (
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

func TestFolderIndexStateCountsProvisionalHashingAndDateCorrection(t *testing.T) {
	store := state.NewJSONStore(t.TempDir() + "/state.json")
	eng := New(store)
	if err := store.SaveManifest("docs", "verified.txt", block.Manifest{HashState: "complete", Blocks: []block.Block{{Size: 4}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "quick.txt", block.Manifest{HashState: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManifest("docs", "seeded.txt", block.Manifest{HashState: HashStateAssumedValidUnverified, ModTimeUnixNano: 200, SeedBaselineModTimeUnixNano: 100, Blocks: []block.Block{{Size: 5}, {Size: 6}}}); err != nil {
		t.Fatal(err)
	}

	state, err := eng.FolderIndexState("docs")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "lazy-hashing" {
		t.Fatalf("mode = %q", state.Mode)
	}
	if state.TotalFiles != 3 || state.VerifiedFiles != 1 || state.UnknownFiles != 1 || state.UnverifiedSeedFiles != 1 {
		t.Fatalf("unexpected file counts: %+v", state)
	}
	if state.KnownBlocks != 3 || state.QueuedHashJobs != 2 {
		t.Fatalf("unexpected hash counters: %+v", state)
	}
	if !state.ProvisionalReadOnly || state.DateCorrectionsPending != 1 {
		t.Fatalf("expected provisional date correction state: %+v", state)
	}
}
