package daemonlifecycle

import (
	"testing"

	"filesyncengine/internal/api"
)

func TestApplyStopRequestUpdatesStateEventAndLogFields(t *testing.T) {
	current := api.State{NodeName: "node-a", Status: "running", ConfigVersion: 7, Folders: 2}

	result := ApplyStopRequest(current)

	if result.State.Status != "stopped" {
		t.Fatalf("expected stopped state, got %q", result.State.Status)
	}
	if result.State.NodeName != current.NodeName || result.State.ConfigVersion != current.ConfigVersion || result.State.Folders != current.Folders {
		t.Fatalf("expected non-status state fields to be preserved: %#v", result.State)
	}
	if result.Event.Type != "daemon.stopped" {
		t.Fatalf("expected daemon.stopped event, got %q", result.Event.Type)
	}
	if result.Event.Message == "" {
		t.Fatalf("expected user-visible stop event message")
	}
	if result.LogLevel != "info" || result.LogEvent != "daemon.stopped" {
		t.Fatalf("expected structured daemon stop log metadata, got %q/%q", result.LogLevel, result.LogEvent)
	}
	if result.LogMessage == "" {
		t.Fatalf("expected structured stop log message")
	}
	if result.LogFields["node"] != "node-a" {
		t.Fatalf("expected node log field, got %#v", result.LogFields)
	}
}
