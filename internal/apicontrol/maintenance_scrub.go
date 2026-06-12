package apicontrol

import (
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/maintenancecontrol"
)

// MaintenanceScrubResult is the compact daemon-owned per-folder scrub outcome
// needed to build authenticated API/CLI/GUI status without exposing crawler internals.
type MaintenanceScrubResult struct {
	FolderID     string
	Mode         maintenance.FileScrubVerifyMode
	FilesScanned int
	BytesScanned int64
	Reported     int
	Quarantined  int
	Complete     bool
	Yielded      bool
	Cursor       maintenance.Cursor
}

// MaintenanceScrubResultsFromControl converts maintenance-control scrub execution
// results into the compact API projection input used by daemon handlers.
func MaintenanceScrubResultsFromControl(results []maintenancecontrol.ScrubResult) []MaintenanceScrubResult {
	apiResults := make([]MaintenanceScrubResult, 0, len(results))
	for _, result := range results {
		apiResults = append(apiResults, MaintenanceScrubResult{
			FolderID:     result.FolderID,
			Mode:         result.Mode,
			FilesScanned: result.FilesScanned,
			BytesScanned: result.BytesScanned,
			Reported:     result.Reported,
			Quarantined:  result.Quarantined,
			Complete:     result.Complete,
			Yielded:      result.Yielded,
			Cursor:       result.Cursor,
		})
	}
	return apiResults
}

// MaintenanceScrubResponse summarizes manual maintenance scrub runs for API clients.
func MaintenanceScrubResponse(started, finished time.Time, results []MaintenanceScrubResult) api.MaintenanceScrubResponse {
	response := api.MaintenanceScrubResponse{StartedAt: started, FinishedAt: finished, Results: make([]api.MaintenanceScrubFolderResult, 0, len(results)), Complete: true}
	for _, result := range results {
		response.Results = append(response.Results, api.MaintenanceScrubFolderResult{
			FolderID:     result.FolderID,
			Mode:         string(result.Mode),
			FilesScanned: result.FilesScanned,
			BytesScanned: result.BytesScanned,
			Reported:     result.Reported,
			Quarantined:  result.Quarantined,
			Complete:     result.Complete,
			Yielded:      result.Yielded,
			Cursor:       result.Cursor.Position,
		})
		response.FilesScanned += result.FilesScanned
		response.BytesScanned += result.BytesScanned
		response.Reported += result.Reported
		response.Quarantined += result.Quarantined
		if !result.Complete {
			response.Complete = false
		}
	}
	response.Folders = len(response.Results)
	return response
}
