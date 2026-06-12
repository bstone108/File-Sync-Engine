package clioutput

import (
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/maintenancecontrol"
)

func TestMaintenanceScrubOutputRendersFolderRowsAndSummary(t *testing.T) {
	results := []maintenancecontrol.ScrubResult{
		{
			FolderID:     "docs",
			Mode:         maintenance.FileScrubSampledBlocks,
			FilesScanned: 3,
			BytesScanned: 4096,
			Reported:     1,
			Quarantined:  0,
			Complete:     false,
			Cursor:       maintenance.Cursor{Position: 7},
		},
		{
			FolderID:     "media",
			Mode:         maintenance.FileScrubFullBlocks,
			FilesScanned: 2,
			BytesScanned: 8192,
			Reported:     0,
			Quarantined:  1,
			Complete:     true,
			Cursor:       maintenance.Cursor{Position: 9},
		},
	}

	got := MaintenanceScrubOutput(results)
	want := "maintenance scrub: folder=docs mode=sampled-blocks files=3 bytes=4096 reported=1 quarantined=0 complete=false cursor=7\n" +
		"maintenance scrub: folder=media mode=full-blocks files=2 bytes=8192 reported=0 quarantined=1 complete=true cursor=9\n" +
		"maintenance scrub summary: folders=2\n"
	if got != want {
		t.Fatalf("MaintenanceScrubOutput() = %q, want %q", got, want)
	}
}

func TestBackupScrubOutputRendersStableSummary(t *testing.T) {
	response := api.BackupScrubResponse{
		Archive:     api.BackupArchiveScrubState{CheckedJobs: 4, MissingBlocks: 2, CorruptBlocks: 1},
		Checkpoints: api.BackupCheckpointScrubState{CheckedSnapshots: 3, DegradedSnapshots: 1},
		RepairPlan:  api.BackupRepairPlanState{RepairableBlocks: 5, UnresolvedBlocks: 6},
	}

	got := BackupScrubOutput(response)
	want := "maintenance backup-scrub: archiveCheckedJobs=4 archiveMissingBlocks=2 archiveCorruptBlocks=1 checkpointSnapshots=3 degradedSnapshots=1 repairableBlocks=5 unresolvedBlocks=6\n"
	if got != want {
		t.Fatalf("BackupScrubOutput() = %q, want %q", got, want)
	}
}
