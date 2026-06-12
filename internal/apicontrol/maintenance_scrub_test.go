package apicontrol

import (
	"testing"
	"time"

	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/maintenancecontrol"
)

func TestMaintenanceScrubResponseSummarizesFolderResults(t *testing.T) {
	started := time.Unix(100, 0).UTC()
	finished := time.Unix(101, 0).UTC()

	response := MaintenanceScrubResponse(started, finished, []MaintenanceScrubResult{
		{FolderID: "docs", Mode: maintenance.FileScrubFullBlocks, FilesScanned: 2, BytesScanned: 64, Reported: 1, Quarantined: 0, Complete: true, Cursor: maintenance.Cursor{Position: 4}},
		{FolderID: "photos", Mode: maintenance.FileScrubSampledBlocks, FilesScanned: 3, BytesScanned: 128, Reported: 0, Quarantined: 1, Complete: false, Cursor: maintenance.Cursor{Position: 7}},
	})

	if !response.StartedAt.Equal(started) || !response.FinishedAt.Equal(finished) || response.Folders != 2 || response.FilesScanned != 5 || response.BytesScanned != 192 || response.Reported != 1 || response.Quarantined != 1 || response.Complete {
		t.Fatalf("summary response mismatch: %+v", response)
	}
	if len(response.Results) != 2 || response.Results[0].FolderID != "docs" || response.Results[0].Mode != "full-blocks" || response.Results[0].Cursor != 4 || response.Results[1].FolderID != "photos" || response.Results[1].Mode != "sampled-blocks" || response.Results[1].Cursor != 7 {
		t.Fatalf("folder results mismatch: %+v", response.Results)
	}
}

func TestMaintenanceScrubResultsFromControlKeepsFolderCounters(t *testing.T) {
	results := MaintenanceScrubResultsFromControl([]maintenancecontrol.ScrubResult{
		{FolderID: "docs", Mode: maintenance.FileScrubFullBlocks, FilesScanned: 2, BytesScanned: 64, Reported: 1, Quarantined: 0, Complete: true, Yielded: false, Cursor: maintenance.Cursor{Position: 4}},
		{FolderID: "photos", Mode: maintenance.FileScrubSampledBlocks, FilesScanned: 3, BytesScanned: 128, Reported: 0, Quarantined: 1, Complete: false, Yielded: true, Cursor: maintenance.Cursor{Position: 7}},
	})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].FolderID != "docs" || results[0].Mode != maintenance.FileScrubFullBlocks || results[0].FilesScanned != 2 || results[0].BytesScanned != 64 || results[0].Reported != 1 || results[0].Quarantined != 0 || !results[0].Complete || results[0].Yielded || results[0].Cursor.Position != 4 {
		t.Fatalf("first conversion mismatch: %+v", results[0])
	}
	if results[1].FolderID != "photos" || results[1].Mode != maintenance.FileScrubSampledBlocks || results[1].FilesScanned != 3 || results[1].BytesScanned != 128 || results[1].Reported != 0 || results[1].Quarantined != 1 || results[1].Complete || !results[1].Yielded || results[1].Cursor.Position != 7 {
		t.Fatalf("second conversion mismatch: %+v", results[1])
	}
}
