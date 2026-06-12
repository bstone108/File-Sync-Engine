package apicontrol

import (
	"fmt"

	"filesyncengine/internal/api"
	"filesyncengine/internal/transfercontrol"
)

// HandleTransferCommand mutates the daemon's runtime-only transfer control state
// for authenticated API pause/resume/cancel requests.
func HandleTransferCommand(control *transfercontrol.Control, req api.TransferCommandRequest) (api.TransferCommandResponse, error) {
	if control == nil {
		return api.TransferCommandResponse{}, fmt.Errorf("transfer control is not configured")
	}
	if req.FolderID == "" && req.PeerID == "" {
		return api.TransferCommandResponse{}, fmt.Errorf("transfer command requires folderID or peerID")
	}
	switch req.Action {
	case "pause":
		control.Pause(req.FolderID, req.PeerID)
	case "resume":
		control.Resume(req.FolderID, req.PeerID)
	case "cancel":
		control.Cancel(req.FolderID, req.PeerID)
	default:
		return api.TransferCommandResponse{}, fmt.Errorf("unsupported transfer command action %q", req.Action)
	}
	return api.TransferCommandResponse{Action: req.Action, FolderID: req.FolderID, PeerID: req.PeerID, Status: "accepted", Message: fmt.Sprintf("transfer %s accepted for runtime scope", req.Action)}, nil
}
