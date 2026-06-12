package apicontrol

import (
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/maintenance"
)

func TestMaintenanceWarningProjectionIncludesRepairStatusAndStructuredFields(t *testing.T) {
	issue := maintenance.FileScrubIssue{
		FolderID:       "folder-a",
		Path:           "docs/report.txt",
		Kind:           maintenance.FileScrubHashMismatch,
		Classification: maintenance.FileScrubSuspectedCorruption,
		Evidence:       "unchanged metadata repeated mismatch",
	}

	projection := BuildMaintenanceWarning(issue, ".sync/quarantine/report.txt")

	if projection.Event.Type != "maintenance.warning" {
		t.Fatalf("event type = %q", projection.Event.Type)
	}
	if projection.Event.FolderID != issue.FolderID || projection.Event.Path != issue.Path {
		t.Fatalf("event target = %q/%q", projection.Event.FolderID, projection.Event.Path)
	}
	if projection.Warning.Kind != "maintenance_suspected_corruption" {
		t.Fatalf("warning kind = %q", projection.Warning.Kind)
	}
	if projection.Warning.Repair == nil || !projection.Warning.Repair.RestoredCopyInPlace || !projection.Warning.Repair.OriginalAvailable {
		t.Fatalf("repair status not populated: %#v", projection.Warning.Repair)
	}
	if projection.Warning.Repair.QuarantinePath != ".sync/quarantine/report.txt" {
		t.Fatalf("quarantine path = %q", projection.Warning.Repair.QuarantinePath)
	}
	if !strings.Contains(projection.Message, "evidence=unchanged metadata repeated mismatch") {
		t.Fatalf("message missing evidence: %q", projection.Message)
	}
	if projection.LogFields["folder_id"] != issue.FolderID || projection.LogFields["classification"] != string(issue.Classification) {
		t.Fatalf("log fields missing issue target/classification: %#v", projection.LogFields)
	}
	if projection.LogFields["quarantine"] != ".sync/quarantine/report.txt" {
		t.Fatalf("log quarantine = %#v", projection.LogFields["quarantine"])
	}
}

func TestMaintenanceWarningProjectionMarksUnquarantinedIssues(t *testing.T) {
	issue := maintenance.FileScrubIssue{
		FolderID:       "folder-a",
		Path:           "notes.txt",
		Kind:           maintenance.FileScrubUnreadableFile,
		Classification: maintenance.FileScrubNeedsUserReview,
	}

	projection := BuildMaintenanceWarning(issue, "")

	if projection.Warning.Kind != "maintenance_scrub_issue" {
		t.Fatalf("warning kind = %q", projection.Warning.Kind)
	}
	if projection.Warning.Repair != nil {
		t.Fatalf("unexpected repair status: %#v", projection.Warning.Repair)
	}
	if !strings.Contains(projection.Message, "original not moved to quarantine") {
		t.Fatalf("message should state no quarantine: %q", projection.Message)
	}
	if projection.LogFields["quarantine"] != "not-moved" {
		t.Fatalf("log quarantine = %#v", projection.LogFields["quarantine"])
	}
}

func TestApplyMaintenanceWarningProjectionAppendsToMatchingFolderOnly(t *testing.T) {
	issue := maintenance.FileScrubIssue{
		FolderID:       "folder-b",
		Path:           "docs/report.txt",
		Kind:           maintenance.FileScrubHashMismatch,
		Classification: maintenance.FileScrubSuspectedCorruption,
	}
	state := api.State{FoldersState: []api.FolderState{
		{ID: "folder-a"},
		{ID: "folder-b", Warnings: api.FolderWarningState{Recent: []api.FolderWarning{{Kind: "existing", Path: "old.txt"}}}},
	}}

	updated, projection := ApplyMaintenanceWarningProjection(state, issue, ".sync/quarantine/report.txt")

	if len(updated.FoldersState[0].Warnings.Recent) != 0 {
		t.Fatalf("unmatched folder warnings changed: %#v", updated.FoldersState[0].Warnings.Recent)
	}
	warnings := updated.FoldersState[1].Warnings.Recent
	if len(warnings) != 2 {
		t.Fatalf("matching folder warning count = %d, warnings=%#v", len(warnings), warnings)
	}
	if warnings[1].Kind != "maintenance_suspected_corruption" || warnings[1].Path != issue.Path {
		t.Fatalf("appended warning = %#v", warnings[1])
	}
	if projection.Event.Type != "maintenance.warning" || projection.Event.FolderID != issue.FolderID {
		t.Fatalf("projection event = %#v", projection.Event)
	}
}
