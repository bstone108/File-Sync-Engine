package maintenance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/block"
)

func TestApplyVerifiedReplacementWithQuarantineMovesOriginalBeforePlacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nested", "data.bin")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target parent: %v", err)
	}
	if err := os.WriteFile(target, []byte("damaged"), 0o644); err != nil {
		t.Fatalf("write damaged target: %v", err)
	}
	replacement := filepath.Join(root, ".sync", "repair", "data.bin.replacement")
	if err := os.MkdirAll(filepath.Dir(replacement), 0o755); err != nil {
		t.Fatalf("mkdir replacement parent: %v", err)
	}
	if err := os.WriteFile(replacement, []byte("trusted"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	desired, err := block.BuildManifest(replacement, 4)
	if err != nil {
		t.Fatalf("build desired manifest: %v", err)
	}
	desired.Path = "nested/data.bin"

	record, err := ApplyVerifiedReplacementWithQuarantine(RepairReplacementPlan{
		Root:            root,
		RelativePath:    "nested/data.bin",
		ReplacementPath: replacement,
		DesiredManifest: desired,
		Now:             func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedReplacementWithQuarantine: %v", err)
	}

	if string(mustReadFile(t, target)) != "trusted" {
		t.Fatalf("target was not atomically replaced with verified bytes")
	}
	if _, err := os.Stat(replacement); !os.IsNotExist(err) {
		t.Fatalf("replacement staging file still exists or stat failed: %v", err)
	}
	if record.OriginalPath != "nested/data.bin" || record.QuarantinePath == "" {
		t.Fatalf("unexpected quarantine record: %+v", record)
	}
	quarantined := filepath.Join(root, filepath.FromSlash(record.QuarantinePath))
	if string(mustReadFile(t, quarantined)) != "damaged" {
		t.Fatalf("quarantine did not preserve damaged original bytes")
	}
	if !strings.HasPrefix(record.QuarantinePath, ".sync/quarantine/nested/") {
		t.Fatalf("quarantine path %q not under engine-owned .sync/quarantine mirror", record.QuarantinePath)
	}
}

func TestApplyVerifiedReplacementWithQuarantineRejectsUnverifiedReplacementWithoutMovingOriginal(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "data.bin")
	if err := os.WriteFile(target, []byte("damaged"), 0o644); err != nil {
		t.Fatalf("write damaged target: %v", err)
	}
	trusted := filepath.Join(root, ".sync", "repair", "trusted.bin")
	if err := os.MkdirAll(filepath.Dir(trusted), 0o755); err != nil {
		t.Fatalf("mkdir replacement parent: %v", err)
	}
	if err := os.WriteFile(trusted, []byte("trusted"), 0o644); err != nil {
		t.Fatalf("write trusted replacement: %v", err)
	}
	desired, err := block.BuildManifest(trusted, 4)
	if err != nil {
		t.Fatalf("build desired manifest: %v", err)
	}
	badReplacement := filepath.Join(root, ".sync", "repair", "bad.bin")
	if err := os.WriteFile(badReplacement, []byte("evil"), 0o644); err != nil {
		t.Fatalf("write bad replacement: %v", err)
	}

	_, err = ApplyVerifiedReplacementWithQuarantine(RepairReplacementPlan{
		Root:            root,
		RelativePath:    "data.bin",
		ReplacementPath: badReplacement,
		DesiredManifest: desired,
	})
	if err == nil {
		t.Fatalf("ApplyVerifiedReplacementWithQuarantine succeeded with unverified replacement")
	}
	if string(mustReadFile(t, target)) != "damaged" {
		t.Fatalf("original was moved or replaced after verification failure")
	}
	matches, err := filepath.Glob(filepath.Join(root, ".sync", "quarantine", "*"))
	if err != nil {
		t.Fatalf("glob quarantine: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("quarantine artifacts created after verification failure: %v", matches)
	}
}
