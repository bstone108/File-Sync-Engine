package block

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildManifestSplitsFileIntoHashedBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.bin")
	data := []byte("abcdefghij")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if manifest.Size != int64(len(data)) {
		t.Fatalf("size = %d", manifest.Size)
	}
	if got := len(manifest.Blocks); got != 3 {
		t.Fatalf("block count = %d, want 3", got)
	}
	if manifest.Blocks[0].Offset != 0 || manifest.Blocks[1].Offset != 4 || manifest.Blocks[2].Offset != 8 {
		t.Fatalf("unexpected offsets: %+v", manifest.Blocks)
	}
	if manifest.Blocks[2].Size != 2 {
		t.Fatalf("last block size = %d, want 2", manifest.Blocks[2].Size)
	}
	if bytes.Equal(manifest.Blocks[0].Hash, manifest.Blocks[1].Hash) {
		t.Fatalf("different blocks should have different hashes")
	}
}

func TestPlanDeltaRequestsOnlyMissingOrChangedBlocks(t *testing.T) {
	base := Manifest{BlockSize: 4, Blocks: []Block{
		{Index: 0, Offset: 0, Size: 4, Hash: []byte{1}},
		{Index: 1, Offset: 4, Size: 4, Hash: []byte{2}},
		{Index: 2, Offset: 8, Size: 2, Hash: []byte{3}},
	}}
	target := Manifest{BlockSize: 4, Blocks: []Block{
		{Index: 0, Offset: 0, Size: 4, Hash: []byte{1}},
		{Index: 1, Offset: 4, Size: 4, Hash: []byte{9}},
		{Index: 2, Offset: 8, Size: 2, Hash: []byte{3}},
		{Index: 3, Offset: 10, Size: 4, Hash: []byte{4}},
	}}

	plan := PlanDelta(base, target)
	if len(plan.Needed) != 2 {
		t.Fatalf("needed count = %d, want 2: %+v", len(plan.Needed), plan.Needed)
	}
	if plan.Needed[0].Index != 1 || plan.Needed[1].Index != 3 {
		t.Fatalf("unexpected block indexes: %+v", plan.Needed)
	}
}
