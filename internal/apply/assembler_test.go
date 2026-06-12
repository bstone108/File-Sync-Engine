package apply

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/block"
)

func TestAssembleFromLocalBlocksReusesShiftedBlocks(t *testing.T) {
	dir := t.TempDir()
	sourceA := filepath.Join(dir, "source-a.bin")
	sourceB := filepath.Join(dir, "source-b.bin")
	targetPath := filepath.Join(dir, "target.bin")

	if err := os.WriteFile(sourceA, []byte("aaaabbbbcccc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceB, []byte("ccccaaaabbbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceManifestA, err := block.BuildManifest(sourceA, 4)
	if err != nil {
		t.Fatal(err)
	}
	sourceManifestB, err := block.BuildManifest(sourceB, 4)
	if err != nil {
		t.Fatal(err)
	}
	targetManifest, err := block.BuildManifest(sourceB, 4)
	if err != nil {
		t.Fatal(err)
	}
	targetManifest.Path = "target.bin"

	if err := AssembleFromLocalBlocks(targetPath, targetManifest, []block.Manifest{sourceManifestA, sourceManifestB}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ccccaaaabbbb" {
		t.Fatalf("unexpected assembled data: %q", data)
	}
}

func TestAssembleFromLocalBlocksRunsBeforeRenameHookAfterVerification(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.bin")
	targetPath := filepath.Join(dir, "target.bin")

	if err := os.WriteFile(sourcePath, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old-target"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceManifest, err := block.BuildManifest(sourcePath, 4)
	if err != nil {
		t.Fatal(err)
	}
	targetManifest := sourceManifest
	targetManifest.Path = "target.bin"

	hookCalled := false
	err = AssembleFromLocalBlocksBeforeRename(targetPath, targetManifest, []block.Manifest{sourceManifest}, func() error {
		hookCalled = true
		data, err := os.ReadFile(targetPath)
		if err != nil {
			return err
		}
		if string(data) != "old-target" {
			t.Fatalf("target was replaced before hook ran: %q", data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Fatal("expected before-rename hook to run")
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "replacement" {
		t.Fatalf("target was not replaced after hook: %q", data)
	}
}

func TestAssembleFromLocalBlocksLeavesExistingFileOnMissingBlock(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.bin")
	targetPath := filepath.Join(dir, "target.bin")
	wantPath := filepath.Join(dir, "want.bin")

	if err := os.WriteFile(sourcePath, []byte("aaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wantPath, []byte("bbbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceManifest, err := block.BuildManifest(sourcePath, 4)
	if err != nil {
		t.Fatal(err)
	}
	targetManifest, err := block.BuildManifest(wantPath, 4)
	if err != nil {
		t.Fatal(err)
	}
	targetManifest.Path = "target.bin"

	if err := AssembleFromLocalBlocks(targetPath, targetManifest, []block.Manifest{sourceManifest}); err == nil {
		t.Fatal("expected missing block error")
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep-me" {
		t.Fatalf("existing target was overwritten after failed assemble: %q", data)
	}
}
