package scancli

import (
	"filesyncengine/internal/clioutput"
	"filesyncengine/internal/config"
	"filesyncengine/internal/scancontrol"
)

// RunConfigured loads the active config, runs a one-shot quick metadata scan,
// and returns the stable human-readable CLI output for the scan command.
func RunConfigured(configPath string, folderID string) (string, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return "", err
	}
	result, err := scancontrol.RunQuickIndex(cfg, configPath, folderID)
	if err != nil {
		return "", err
	}
	return clioutput.ScanOutput(result, cfg.Metadata.PerFolder), nil
}
