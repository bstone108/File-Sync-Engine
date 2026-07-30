package apicontrol

import (
	"fmt"
	"net/http"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/webgui"
)

// WebGUIStartupResult reports daemon-owned optional web GUI startup without
// letting a GUI package or listener failure block the headless daemon.
type WebGUIStartupResult struct {
	Response api.WebGUICommandResponse
	Err      error
}

// StartConfiguredWebGUI installs and starts an explicitly enabled optional web
// GUI. It always returns a response so daemon startup can remain available when
// GUI delivery or listener setup fails.
func StartConfiguredWebGUI(cfg config.Config, manager *webgui.Server, client *http.Client) WebGUIStartupResult {
	if !cfg.WebGUI.Enabled {
		return WebGUIStartupResult{Response: api.WebGUICommandResponse{
			Action:  "startup",
			Status:  "disabled",
			Message: "web GUI is disabled; core daemon is running headless",
		}}
	}
	if _, err := HandleWebGUICommandWithManager(cfg, api.WebGUICommandRequest{Action: "install"}, manager, client); err != nil {
		return failedWebGUIStartup(err)
	}
	response, err := HandleWebGUICommandWithManager(cfg, api.WebGUICommandRequest{Action: "start"}, manager, client)
	if err != nil {
		return failedWebGUIStartup(err)
	}
	response.Action = "startup"
	return WebGUIStartupResult{Response: response}
}

func failedWebGUIStartup(err error) WebGUIStartupResult {
	return WebGUIStartupResult{
		Response: api.WebGUICommandResponse{
			Action:  "startup",
			Status:  "failed",
			Message: "optional web GUI unavailable; daemon continues headless",
		},
		Err: err,
	}
}

// HandleWebGUICommand performs authenticated optional web GUI lifecycle actions
// without leaking secrets or requiring the core daemon to run a GUI.
func HandleWebGUICommand(cfg config.Config, req api.WebGUICommandRequest) (api.WebGUICommandResponse, error) {
	return HandleWebGUICommandWithHTTPClient(cfg, req, nil)
}

// HandleWebGUICommandWithHTTPClient installs remote web GUI packages with the
// caller-provided HTTP client when tests or embedders need custom TLS trust.
func HandleWebGUICommandWithHTTPClient(cfg config.Config, req api.WebGUICommandRequest, client *http.Client) (api.WebGUICommandResponse, error) {
	return HandleWebGUICommandWithManager(cfg, req, webgui.NewServer(), client)
}

// HandleWebGUICommandWithManager uses an explicit web server manager so daemon
// runtime/tests can keep lifecycle state across status/start/stop calls.
func HandleWebGUICommandWithManager(cfg config.Config, req api.WebGUICommandRequest, manager *webgui.Server, client *http.Client) (api.WebGUICommandResponse, error) {
	if manager == nil {
		return api.WebGUICommandResponse{}, fmt.Errorf("web GUI manager is not configured")
	}
	switch req.Action {
	case "status":
		if !cfg.WebGUI.Enabled {
			return api.WebGUICommandResponse{Action: req.Action, Status: "disabled", Message: "web GUI is disabled; core daemon is running headless"}, nil
		}
		status := manager.Status(cfg.WebGUI.InstallDir)
		message := "web GUI package is installed"
		if status.Running {
			message = "web GUI server is running"
		} else if status.Status == "not_installed" {
			message = "web GUI package is not installed"
		}
		return webGUIStatusResponse(req.Action, status, message), nil
	case "install", "update":
		if !cfg.WebGUI.Enabled {
			return api.WebGUICommandResponse{}, fmt.Errorf("web GUI is disabled in config")
		}
		if cfg.WebGUI.PackagePath != "" {
			result, err := webgui.InstallLocalPackage(webgui.InstallOptions{PackagePath: cfg.WebGUI.PackagePath, InstallDir: cfg.WebGUI.InstallDir, Version: cfg.WebGUI.Version, ChecksumSHA256: cfg.WebGUI.ChecksumSHA256})
			if err != nil {
				return api.WebGUICommandResponse{}, err
			}
			return api.WebGUICommandResponse{Action: req.Action, Status: result.Status, Version: result.Version, InstallDir: result.InstallDir, Message: "web GUI package installed from trusted local bundle"}, nil
		}
		if cfg.WebGUI.UpdateURL == "" {
			return api.WebGUICommandResponse{}, fmt.Errorf("web GUI install/update requires packagePath or updateURL")
		}
		result, err := webgui.InstallRemotePackage(webgui.InstallRemoteOptions{UpdateURL: cfg.WebGUI.UpdateURL, InstallDir: cfg.WebGUI.InstallDir, Version: cfg.WebGUI.Version, ChecksumSHA256: cfg.WebGUI.ChecksumSHA256, HTTPClient: client})
		if err != nil {
			return api.WebGUICommandResponse{}, err
		}
		return api.WebGUICommandResponse{Action: req.Action, Status: result.Status, Version: result.Version, InstallDir: result.InstallDir, Message: "web GUI package installed from trusted HTTPS update"}, nil
	case "start":
		if !cfg.WebGUI.Enabled {
			return api.WebGUICommandResponse{}, fmt.Errorf("web GUI is disabled in config")
		}
		status, err := manager.Start(webgui.StartOptions{InstallDir: cfg.WebGUI.InstallDir, Listen: cfg.WebGUI.Listen, HTTPSListen: cfg.WebGUI.HTTPSListen, TLSCertFile: cfg.WebGUI.TLSCertFile, TLSKeyFile: cfg.WebGUI.TLSKeyFile})
		if err != nil {
			return api.WebGUICommandResponse{}, err
		}
		return webGUIStatusResponse(req.Action, status, "web GUI server started"), nil
	case "stop":
		status, err := manager.Stop()
		if err != nil {
			return api.WebGUICommandResponse{}, err
		}
		return webGUIStatusResponse(req.Action, status, "web GUI server stopped"), nil
	default:
		return api.WebGUICommandResponse{}, fmt.Errorf("unsupported web GUI command action %q", req.Action)
	}
}

func webGUIStatusResponse(action string, status webgui.Status, message string) api.WebGUICommandResponse {
	return api.WebGUICommandResponse{Action: action, Status: status.Status, Version: status.Version, InstallDir: status.InstallDir, Listen: status.Listen, URL: status.URL, HTTPSListen: status.HTTPSListen, HTTPSURL: status.HTTPSURL, Running: status.Running, Message: message}
}
