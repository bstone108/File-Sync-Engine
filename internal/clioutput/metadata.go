package clioutput

import (
	"fmt"
	"strings"

	"filesyncengine/internal/metadataops"
	"filesyncengine/internal/state"
)

func MetadataCompactionOutput(results []state.MetadataCompactionResult, statePath string) string {
	var b strings.Builder
	for _, result := range results {
		fmt.Fprintf(&b, "metadata compacted: folder=%s safeCursor=%d compactedTombstones=%d retainedTombstones=%d\n", result.Plan.FolderID, result.Plan.SafeCursor, result.CompactedTombstones, result.Plan.RetainedTombstones)
	}
	fmt.Fprintf(&b, "metadata compaction summary: folders=%d state=%s\n", len(results), statePath)
	return b.String()
}

func MetadataImportOutput(action string, result metadataops.Result) string {
	return fmt.Sprintf("metadata %s summary: source=%s target=%s folders=%d manifests=%d backup=%s\n", action, result.SourcePath, result.TargetPath, result.Folders, result.ImportedManifests, result.BackupPath)
}
