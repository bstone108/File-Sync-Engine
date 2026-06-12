package clioutput

import (
	"testing"

	"filesyncengine/internal/metadataops"
	"filesyncengine/internal/state"
)

func TestMetadataCompactionOutputRendersFolderRowsAndSummary(t *testing.T) {
	out := MetadataCompactionOutput([]state.MetadataCompactionResult{{
		Plan: state.MetadataCompactionPlan{
			FolderID:           "docs",
			SafeCursor:         42,
			RetainedTombstones: 3,
		},
		CompactedTombstones: 7,
	}}, "/var/lib/fse/state.badger")

	want := "metadata compacted: folder=docs safeCursor=42 compactedTombstones=7 retainedTombstones=3\n" +
		"metadata compaction summary: folders=1 state=/var/lib/fse/state.badger\n"
	if out != want {
		t.Fatalf("unexpected metadata compaction output:\nwant %q\n got %q", want, out)
	}
}

func TestMetadataImportOutputRendersRollbackAwareSummary(t *testing.T) {
	out := MetadataImportOutput("import-json", metadataops.Result{
		SourcePath:        "/tmp/state.json",
		TargetPath:        "/var/lib/fse/state.badger",
		Folders:           2,
		ImportedManifests: 5,
		BackupPath:        "/var/lib/fse/state.badger.backup",
	})

	want := "metadata import-json summary: source=/tmp/state.json target=/var/lib/fse/state.badger folders=2 manifests=5 backup=/var/lib/fse/state.badger.backup\n"
	if out != want {
		t.Fatalf("unexpected metadata import output:\nwant %q\n got %q", want, out)
	}
}

func TestMetadataSplitBadgerOutputRendersRollbackAwareSummary(t *testing.T) {
	out := MetadataImportOutput("split-badger", metadataops.Result{
		SourcePath:        "/tmp/source.badger",
		TargetPath:        "/var/lib/fse/metadata",
		Folders:           3,
		ImportedManifests: 8,
		BackupPath:        "/var/lib/fse/metadata.backup",
	})

	want := "metadata split-badger summary: source=/tmp/source.badger target=/var/lib/fse/metadata folders=3 manifests=8 backup=/var/lib/fse/metadata.backup\n"
	if out != want {
		t.Fatalf("unexpected metadata split output:\nwant %q\n got %q", want, out)
	}
}
