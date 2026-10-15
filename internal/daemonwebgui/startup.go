package daemonwebgui

import (
	"context"
	"os"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/apicontrol"
	"filesyncengine/internal/config"
	"filesyncengine/internal/webgui"
	"filesyncengine/internal/webguiupdate"
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

// StartAutoUpdate checks the GUI catalog on startup and again at the configured
// interval. It returns without a download when the GUI environment is unset or
// auto-update is off. Desktop Sparkle, Windows, and AppImage updaters are not
// part of this loop.
func StartAutoUpdate(ctx context.Context, configPath string, report func(api.WebGUIState)) {
	if ctx == nil {
		ctx = context.Background()
	}
	if webguiupdate.ExplicitPackagePinned(os.Getenv("FSE_WEB_GUI_VERSION"), os.Getenv("FSE_WEB_GUI_PACKAGE"), os.Getenv("FSE_WEB_GUI_INSTALL_DIR"), os.Getenv("FSE_WEB_GUI_LISTEN"), os.Getenv("FSE_WEB_GUI_CHECKSUM")) {
		if report != nil {
			report(api.WebGUIState{Enabled: true, Version: os.Getenv("FSE_WEB_GUI_VERSION"), LastUpdateCheck: time.Now().UTC(), LastUpdateResult: "pinned", LastUpdateMessage: "An explicit web GUI package is pinned. Auto-update did not download a replacement.", AutoUpdate: false})
		}
		return
	}
	interval := webguiupdate.ParseInterval(os.Getenv(webguiupdate.EnvInterval))
	run := func() {
		cfg, err := config.LoadFile(configPath)
		version, sum, installDir, listen := "", "", "", ""
		if err == nil {
			version = cfg.WebGUI.Version
			sum = cfg.WebGUI.ChecksumSHA256
			installDir = cfg.WebGUI.InstallDir
			listen = cfg.WebGUI.Listen
		}
		result := webguiupdate.Check(ctx, webguiupdate.Options{
			GUIEnabledEnv:  os.Getenv(webguiupdate.EnvEnabled),
			AutoUpdateEnv:  os.Getenv(webguiupdate.EnvAuto),
			PublicKey:      os.Getenv(webguiupdate.EnvPublic),
			ConfigPath:     configPath,
			InstallDir:     installDir,
			Listen:         listen,
			CurrentVersion: version,
			CurrentSum:     sum,
		})
		if report != nil {
			report(api.WebGUIState{
				Enabled:           result.Enabled,
				Version:           result.Version,
				LastUpdateCheck:   result.CheckedAt,
				LastUpdateResult:  result.Outcome,
				LastUpdateMessage: result.Message,
				AutoUpdate:        result.Auto,
			})
		}
	}
	run()
	if !webguiupdate.AutoUpdateEnabled(os.Getenv(webguiupdate.EnvEnabled), os.Getenv(webguiupdate.EnvAuto)) {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
