package clioutput

import (
	"fmt"
	"strings"

	"filesyncengine/internal/api"
	"filesyncengine/internal/maintenancecontrol"
)

func MaintenanceScrubOutput(results []maintenancecontrol.ScrubResult) string {
	var b strings.Builder
	for _, result := range results {
		fmt.Fprintf(&b, "maintenance scrub: folder=%s mode=%s files=%d bytes=%d reported=%d quarantined=%d complete=%v cursor=%d\n", result.FolderID, result.Mode, result.FilesScanned, result.BytesScanned, result.Reported, result.Quarantined, result.Complete, result.Cursor.Position)
	}
	fmt.Fprintf(&b, "maintenance scrub summary: folders=%d\n", len(results))
	return b.String()
}

func BackupScrubOutput(result api.BackupScrubResponse) string {
	return fmt.Sprintf("maintenance backup-scrub: archiveCheckedJobs=%d archiveMissingBlocks=%d archiveCorruptBlocks=%d checkpointSnapshots=%d degradedSnapshots=%d repairableBlocks=%d unresolvedBlocks=%d\n", result.Archive.CheckedJobs, result.Archive.MissingBlocks, result.Archive.CorruptBlocks, result.Checkpoints.CheckedSnapshots, result.Checkpoints.DegradedSnapshots, result.RepairPlan.RepairableBlocks, result.RepairPlan.UnresolvedBlocks)
}
