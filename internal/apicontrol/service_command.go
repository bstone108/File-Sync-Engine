package apicontrol

import (
	"fmt"

	"filesyncengine/internal/api"
	"filesyncengine/internal/service"
)

// HandleServiceCommand renders reviewable platform service-manager handoff
// commands for authenticated API callers without executing privileged actions.
func HandleServiceCommand(req api.ServiceCommandRequest) (api.ServiceCommandResponse, error) {
	switch req.Action {
	case "status", "start", "stop", "restart":
	default:
		return api.ServiceCommandResponse{}, fmt.Errorf("unsupported service command action %q", req.Action)
	}
	if req.Platform == "" {
		return api.ServiceCommandResponse{}, fmt.Errorf("service command requires platform")
	}
	if req.ServiceName == "" {
		return api.ServiceCommandResponse{}, fmt.Errorf("service command requires serviceName")
	}
	handoff, err := service.ControlHandoff(service.ControlOptions{
		Platform:    service.Platform(req.Platform),
		ServiceName: req.ServiceName,
		Domain:      req.Domain,
		Action:      service.ControlAction(req.Action),
	})
	if err != nil {
		return api.ServiceCommandResponse{}, err
	}
	return api.ServiceCommandResponse{Action: req.Action, Platform: req.Platform, ServiceName: req.ServiceName, Status: "accepted", Message: "service command handoff rendered for review; privileged service-manager commands are not executed by the daemon", Handoff: handoff}, nil
}
