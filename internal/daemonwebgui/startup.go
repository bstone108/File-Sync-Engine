package daemonwebgui

import (
	"filesyncengine/internal/api"
	"filesyncengine/internal/apicontrol"
	"filesyncengine/internal/config"
	"filesyncengine/internal/webgui"
)

// Publisher accepts daemon events without coupling optional web GUI startup to
// the API server implementation.
type Publisher interface {
	Publish(api.Event)
}

// Start performs daemon-owned optional web GUI startup. Delivery or listener
// failures are reported as events while the caller continues core daemon setup.
func Start(cfg config.Config, manager *webgui.Server, publisher Publisher) apicontrol.WebGUIStartupResult {
	result := apicontrol.StartConfiguredWebGUI(cfg, manager, nil)
	if publisher == nil || !cfg.WebGUI.Enabled {
		return result
	}
	eventType := "webgui.startup.finished"
	if result.Err != nil {
		eventType = "webgui.startup.failed"
	}
	publisher.Publish(api.Event{Type: eventType, Message: result.Response.Message})
	return result
}
