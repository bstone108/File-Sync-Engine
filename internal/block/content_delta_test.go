package block

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanContentDeltaReusesShiftedBlocksByHash(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.bin")
	targetPath := filepath.Join(dir, "target.bin")
	if err := os.WriteFile(basePath, []byte("AAAABBBBCCCC"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Same blocks, shifted by one block after an inserted block. Only XXXX should be fetched.
	if err := os.WriteFile(targetPath, []byte("XXXXAAAABBBBCCCC"), 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := BuildManifest(basePath, 4)
	if err != nil {
		t.Fatal(err)
	}
	target, err := BuildManifest(targetPath, 4)
	if err != nil {
		t.Fatal(err)
	}

	plan := PlanContentDelta([]Manifest{base}, target)
	if len(plan.Needed) != 1 {
		t.Fatalf("needed blocks = %d, want 1: %+v", len(plan.Needed), plan.Needed)
	}
	if plan.Needed[0].Index != 0 {
		t.Fatalf("needed block index = %d, want inserted block 0", plan.Needed[0].Index)
	}
	if len(plan.Reused) != 3 {
		t.Fatalf("reused blocks = %d, want 3: %+v", len(plan.Reused), plan.Reused)
	}
}

func TestPlanContentDeltaReusesBlocksAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.bin")
	newPath := filepath.Join(dir, "new.bin")
	if err := os.WriteFile(oldPath, []byte("AAAABBBB"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("BBBBCCCC"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldManifest, err := BuildManifest(oldPath, 4)
	if err != nil {
		t.Fatal(err)
	}
	newManifest, err := BuildManifest(newPath, 4)
	if err != nil {
		t.Fatal(err)
	}

	plan := PlanContentDelta([]Manifest{oldManifest}, newManifest)
	if len(plan.Needed) != 1 || plan.Needed[0].Index != 1 {
		t.Fatalf("expected only CCCC to be needed: %+v", plan.Needed)
	}
	if len(plan.Reused) != 1 || plan.Reused[0].TargetIndex != 0 {
		t.Fatalf("expected BBBB to be reused for target block 0: %+v", plan.Reused)
	}
}
