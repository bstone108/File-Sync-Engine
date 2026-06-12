package daemonlifecycle

import "filesyncengine/internal/api"

type StopProjection struct {
	State      api.State
	Event      api.Event
	LogLevel   string
	LogEvent   string
	LogMessage string
	LogFields  map[string]any
}

func ApplyStopRequest(current api.State) StopProjection {
	current.Status = "stopped"
	return StopProjection{
		State: current,
		Event: api.Event{
			Type:    "daemon.stopped",
			Message: "daemon stopped after control request",
		},
		LogLevel:   "info",
		LogEvent:   "daemon.stopped",
		LogMessage: "file synchronization engine stopped after control request",
		LogFields:  map[string]any{"node": current.NodeName},
	}
}
