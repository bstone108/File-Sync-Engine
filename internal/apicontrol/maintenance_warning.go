package apicontrol

import (
	"fmt"

	"filesyncengine/internal/api"
	"filesyncengine/internal/maintenance"
)

// MaintenanceWarningProjection contains the API warning/event/log payloads for a
// maintenance scrub finding. Callers own state mutation and structured log emission.
type MaintenanceWarningProjection struct {
	Message   string
	Warning   api.FolderWarning
	Event     api.Event
	LogFields map[string]any
}

func BuildMaintenanceWarning(issue maintenance.FileScrubIssue, quarantinePath string) MaintenanceWarningProjection {
	message := maintenanceScrubIssueMessage(issue, quarantinePath)
	warning := api.FolderWarning{
		Kind:    maintenanceScrubWarningKind(issue),
		Path:    issue.Path,
		Message: message,
		Repair:  maintenanceRepairWarningStatus(quarantinePath),
	}
	return MaintenanceWarningProjection{
		Message: message,
		Warning: warning,
		Event: api.Event{
			Type:     "maintenance.warning",
			FolderID: issue.FolderID,
			Path:     issue.Path,
			Message:  message,
		},
		LogFields: map[string]any{
			"folder_id":      issue.FolderID,
			"path":           issue.Path,
			"kind":           string(issue.Kind),
			"classification": string(issue.Classification),
			"quarantine":     quarantineStatus(quarantinePath),
		},
	}
}

func ApplyMaintenanceWarningProjection(state api.State, issue maintenance.FileScrubIssue, quarantinePath string) (api.State, MaintenanceWarningProjection) {
	projection := BuildMaintenanceWarning(issue, quarantinePath)
	for i, folder := range state.FoldersState {
		if folder.ID != issue.FolderID {
			continue
		}
		state.FoldersState[i].Warnings.Recent = append(state.FoldersState[i].Warnings.Recent, projection.Warning)
		break
	}
	return state, projection
}

func maintenanceScrubIssueMessage(issue maintenance.FileScrubIssue, quarantinePath string) string {
	classification := string(issue.Classification)
	if classification == "" {
		classification = "unclassified"
	}
	message := fmt.Sprintf("maintenance scrub reported %s for %s (%s)", issue.Kind, issue.Path, classification)
	if issue.Evidence != "" {
		message = fmt.Sprintf("%s evidence=%s", message, issue.Evidence)
	}
	if quarantinePath != "" {
		message = fmt.Sprintf("%s; restored copy is in place; original remains available in quarantine at %s for manual verification/restore", message, quarantinePath)
	} else {
		message = fmt.Sprintf("%s; original not moved to quarantine", message)
	}
	return message
}

func maintenanceRepairWarningStatus(quarantinePath string) *api.FolderRepairWarning {
	if quarantinePath == "" {
		return nil
	}
	return &api.FolderRepairWarning{
		Status:              "restored_with_quarantined_original",
		RestoredCopyInPlace: true,
		OriginalAvailable:   true,
		QuarantinePath:      quarantinePath,
		UserAction:          "Verify the restored copy and keep or restore the quarantined original if needed.",
	}
}

func maintenanceScrubWarningKind(issue maintenance.FileScrubIssue) string {
	if issue.Classification == maintenance.FileScrubSuspectedCorruption {
		return "maintenance_suspected_corruption"
	}
	return "maintenance_scrub_issue"
}

func quarantineStatus(path string) string {
	if path == "" {
		return "not-moved"
	}
	return path
}
