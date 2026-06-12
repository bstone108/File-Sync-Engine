package apicontrol

import (
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/transfercontrol"
)

func TestHandleTransferCommandPausesResumesAndCancelsRuntimeScope(t *testing.T) {
	control := transfercontrol.New()

	pause, err := HandleTransferCommand(control, api.TransferCommandRequest{Action: "pause", FolderID: "docs", PeerID: "peer-a"})
	if err != nil {
		t.Fatalf("pause transfer: %v", err)
	}
	if pause.Status != "accepted" || pause.Action != "pause" || pause.FolderID != "docs" || pause.PeerID != "peer-a" || !control.IsPaused("docs", "peer-a") || control.IsPaused("docs", "peer-b") {
		t.Fatalf("unexpected pause response/control: %+v paused=%v other=%v", pause, control.IsPaused("docs", "peer-a"), control.IsPaused("docs", "peer-b"))
	}

	resume, err := HandleTransferCommand(control, api.TransferCommandRequest{Action: "resume", FolderID: "docs", PeerID: "peer-a"})
	if err != nil {
		t.Fatalf("resume transfer: %v", err)
	}
	if resume.Status != "accepted" || resume.Action != "resume" || control.IsPaused("docs", "peer-a") {
		t.Fatalf("unexpected resume response/control: %+v paused=%v", resume, control.IsPaused("docs", "peer-a"))
	}

	cancel, err := HandleTransferCommand(control, api.TransferCommandRequest{Action: "cancel", FolderID: "docs", PeerID: "peer-a"})
	if err != nil {
		t.Fatalf("cancel transfer: %v", err)
	}
	if cancel.Status != "accepted" || !control.IsCancelled("docs", "peer-a") {
		t.Fatalf("unexpected cancel response/control: %+v cancelled=%v", cancel, control.IsCancelled("docs", "peer-a"))
	}
	control.ClearCancel("docs", "peer-a")
	if control.IsCancelled("docs", "peer-a") {
		t.Fatal("cancel scope should clear after the runtime observes it")
	}
}

func TestHandleTransferCommandRejectsMissingScopeAndUnsupportedActions(t *testing.T) {
	control := transfercontrol.New()
	if _, err := HandleTransferCommand(control, api.TransferCommandRequest{Action: "pause"}); err == nil {
		t.Fatal("pause without folder or peer scope should fail")
	}
	if _, err := HandleTransferCommand(control, api.TransferCommandRequest{Action: "bogus", FolderID: "docs"}); err == nil {
		t.Fatal("unsupported transfer action should fail")
	}
	if _, err := HandleTransferCommand(nil, api.TransferCommandRequest{Action: "pause", FolderID: "docs"}); err == nil {
		t.Fatal("nil transfer control should fail")
	}
}
