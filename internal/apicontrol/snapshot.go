package apicontrol

import (
	"filesyncengine/internal/api"
	"filesyncengine/internal/backup"
	"filesyncengine/internal/state"
)

func SnapshotMarkersResponse(markers []state.SnapshotMarker) api.SnapshotResponse {
	return api.SnapshotResponse{Markers: SnapshotMarkers(markers)}
}

func SnapshotMarkers(markers []state.SnapshotMarker) []api.SnapshotMarker {
	out := make([]api.SnapshotMarker, 0, len(markers))
	for _, marker := range markers {
		out = append(out, api.SnapshotMarker{
			ID:          marker.ID,
			FolderID:    marker.FolderID,
			Cursor:      marker.Cursor,
			StateHash:   marker.StateHash,
			CreatedAt:   marker.CreatedAt,
			Description: marker.Description,
			Pinned:      marker.Pinned,
			Deprecated:  marker.Deprecated,
		})
	}
	return out
}

func RestorePlanResponse(plan backup.RestorePlan) api.RestorePlanResponse {
	out := api.RestorePlanResponse{
		SnapshotID:    plan.SnapshotID,
		FolderID:      plan.FolderID,
		Destination:   plan.Destination,
		DryRun:        plan.DryRun,
		TotalFiles:    plan.TotalFiles,
		TotalBytes:    plan.TotalBytes,
		MissingBlocks: plan.MissingBlocks,
		Files:         make([]api.RestorePlanFile, 0, len(plan.Files)),
	}
	for _, file := range plan.Files {
		out.Files = append(out.Files, api.RestorePlanFile{
			Path:             file.Path,
			DestinationPath:  file.DestinationPath,
			Size:             file.Size,
			Blocks:           file.Blocks,
			ArchiveAvailable: file.ArchiveAvailable,
			MissingBlocks:    file.MissingBlocks,
		})
	}
	return out
}

func RestoreResponse(result backup.RestoreResult) api.RestoreResponse {
	return api.RestoreResponse{
		JobID:          result.JobID,
		SnapshotID:     result.SnapshotID,
		FolderID:       result.FolderID,
		Destination:    result.Destination,
		TotalFiles:     result.TotalFiles,
		RestoredFiles:  result.RestoredFiles,
		RestoredBytes:  result.RestoredBytes,
		SkippedFiles:   result.SkippedFiles,
		RemainingFiles: result.RemainingFiles,
	}
}

func SnapshotRetentionResponse(plan backup.SnapshotRetentionPlan) api.SnapshotRetentionResponse {
	return api.SnapshotRetentionResponse{
		JobID:               plan.JobID,
		KeepLast:            plan.KeepLast,
		DeprecatedSnapshots: append([]string(nil), plan.DeprecateSnapshots...),
		DeletedSnapshots:    append([]string(nil), plan.DeleteSnapshots...),
		PromotedManifests:   len(plan.Promotions),
		SweepEligibleBlocks: len(plan.ArchiveBlocksEligibleForSweep),
	}
}
