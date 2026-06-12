package apicontrol

import (
	"filesyncengine/internal/api"
	"filesyncengine/internal/backup"
)

// BackupArchiveScrubState converts detailed archive scrub findings into the compact
// authenticated API status shape used by CLI/API/GUI callers.
func BackupArchiveScrubState(result backup.BackupArchiveScrubResult) api.BackupArchiveScrubState {
	return api.BackupArchiveScrubState{
		CheckedJobs:     result.CheckedJobs,
		ProtectedBlocks: result.ProtectedBlocks,
		MissingBlocks:   result.MissingBlocks,
		CorruptBlocks:   result.CorruptBlocks,
		IncompleteJobs:  result.IncompleteJobs,
		OrphanBlocks:    result.OrphanBlocks,
		Issues:          len(result.Issues),
	}
}

// BackupCheckpointScrubState converts checkpoint scrub details into compact API status.
func BackupCheckpointScrubState(result backup.BackupCheckpointScrubResult) api.BackupCheckpointScrubState {
	return api.BackupCheckpointScrubState{
		CheckedSnapshots:     result.CheckedSnapshots,
		AvailableCheckpoints: result.AvailableCheckpoints,
		MissingCheckpoints:   result.MissingCheckpoints,
		CorruptCheckpoints:   result.CorruptCheckpoints,
		DegradedSnapshots:    result.DegradedSnapshots,
		Issues:               len(result.Issues),
	}
}

// BackupRepairPlanState converts a backup archive repair plan into compact API status.
func BackupRepairPlanState(plan backup.BackupArchiveRepairPlan) api.BackupRepairPlanState {
	return api.BackupRepairPlanState{
		RepairableBlocks: plan.RepairableBlocks,
		UnresolvedBlocks: plan.UnresolvedBlocks,
		Actions:          len(plan.Actions),
		Unresolved:       len(plan.Unresolved),
	}
}
