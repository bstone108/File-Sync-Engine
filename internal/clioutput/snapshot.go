package clioutput

import (
	"fmt"
	"strings"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/state"
)

func SnapshotMarkersOutput(markers []state.SnapshotMarker) string {
	var b strings.Builder
	for _, marker := range markers {
		fmt.Fprintf(&b, "snapshot: id=%s folder=%s cursor=%d stateHash=%s pinned=%v deprecated=%v created=%s description=%q\n", marker.ID, marker.FolderID, marker.Cursor, marker.StateHash, marker.Pinned, marker.Deprecated, marker.CreatedAt, marker.Description)
	}
	fmt.Fprintf(&b, "snapshot summary: count=%d\n", len(markers))
	return b.String()
}

func SnapshotDeletedOutput(id string) string {
	return fmt.Sprintf("snapshot deleted: id=%s\n", id)
}

func RestorePlanOutput(plan backup.RestorePlan) string {
	var b strings.Builder
	for _, file := range plan.Files {
		fmt.Fprintf(&b, "restore-plan file: path=%s destination=%s size=%d blocks=%d archiveAvailable=%v missingBlocks=%d\n", file.Path, file.DestinationPath, file.Size, file.Blocks, file.ArchiveAvailable, len(file.MissingBlocks))
	}
	fmt.Fprintf(&b, "restore-plan summary: snapshot=%s folder=%s destination=%s dryRun=%v files=%d bytes=%d missingBlocks=%d\n", plan.SnapshotID, plan.FolderID, plan.Destination, plan.DryRun, plan.TotalFiles, plan.TotalBytes, plan.MissingBlocks)
	return b.String()
}

func RestoreResultOutput(result backup.RestoreResult) string {
	return fmt.Sprintf("restore summary: snapshot=%s folder=%s destination=%s files=%d restored=%d bytes=%d skipped=%d remaining=%d\n", result.SnapshotID, result.FolderID, result.Destination, result.TotalFiles, result.RestoredFiles, result.RestoredBytes, result.SkippedFiles, result.RemainingFiles)
}

func SnapshotRetentionOutput(result backup.SnapshotRetentionPlan) string {
	return fmt.Sprintf("snapshot retention summary: jobId=%s keepLast=%d deprecated=%d deleted=%d promoted=%d sweepEligibleBlocks=%d\n", result.JobID, result.KeepLast, len(result.DeprecateSnapshots), len(result.DeleteSnapshots), len(result.Promotions), len(result.ArchiveBlocksEligibleForSweep))
}
