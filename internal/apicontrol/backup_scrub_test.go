package apicontrol

import (
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/backup"
)

func TestBackupScrubAPIStatesSummarizeIssueCounts(t *testing.T) {
	archive := BackupArchiveScrubState(backup.BackupArchiveScrubResult{
		CheckedJobs:     3,
		ProtectedBlocks: 5,
		MissingBlocks:   7,
		CorruptBlocks:   11,
		IncompleteJobs:  13,
		OrphanBlocks:    17,
		Issues: []backup.BackupArchiveScrubIssue{
			{Kind: "missing"},
			{Kind: "corrupt"},
		},
	})
	if archive != (api.BackupArchiveScrubState{CheckedJobs: 3, ProtectedBlocks: 5, MissingBlocks: 7, CorruptBlocks: 11, IncompleteJobs: 13, OrphanBlocks: 17, Issues: 2}) {
		t.Fatalf("unexpected archive state: %+v", archive)
	}

	checkpoints := BackupCheckpointScrubState(backup.BackupCheckpointScrubResult{
		CheckedSnapshots:     19,
		AvailableCheckpoints: 23,
		MissingCheckpoints:   29,
		CorruptCheckpoints:   31,
		DegradedSnapshots:    37,
		Issues: []backup.BackupCheckpointScrubIssue{
			{Kind: "missing"},
			{Kind: "corrupt"},
			{Kind: "degraded"},
		},
	})
	if checkpoints != (api.BackupCheckpointScrubState{CheckedSnapshots: 19, AvailableCheckpoints: 23, MissingCheckpoints: 29, CorruptCheckpoints: 31, DegradedSnapshots: 37, Issues: 3}) {
		t.Fatalf("unexpected checkpoint state: %+v", checkpoints)
	}

	repair := BackupRepairPlanState(backup.BackupArchiveRepairPlan{
		RepairableBlocks: 41,
		UnresolvedBlocks: 43,
		Actions: []backup.BackupArchiveRepairAction{
			{Kind: "live"},
			{Kind: "trash"},
		},
		Unresolved: []backup.BackupArchiveRepairIssue{{Kind: "missing"}},
	})
	if repair != (api.BackupRepairPlanState{RepairableBlocks: 41, UnresolvedBlocks: 43, Actions: 2, Unresolved: 1}) {
		t.Fatalf("unexpected repair state: %+v", repair)
	}
}
