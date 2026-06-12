package validatecontrol

import "filesyncengine/internal/config"

// Result describes a successfully loaded and validated config file.
type Result struct {
	ConfigPath string
}

// ValidateConfig loads a config file and runs the normal config validation contract.
func ValidateConfig(configPath string) (Result, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return Result{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}
	return Result{ConfigPath: configPath}, nil
}
