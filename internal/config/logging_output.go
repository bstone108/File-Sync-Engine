package config

import (
	"encoding/json"
	"os"
	"strings"
)

// DefaultLogOutput is where structured logs go when a settings patch or an
// older config left logging.output empty. The container environment wins.
// A machine without that environment and without /config keeps stderr.
func DefaultLogOutput() string {
	if value := strings.TrimSpace(os.Getenv("FSE_LOG_OUTPUT")); value != "" {
		return value
	}
	if info, err := os.Stat("/config"); err == nil && info.IsDir() {
		return "/config/logs/fse.jsonl"
	}
	return "stderr"
}

// PersistLoggingOutput writes the default log file path when output was dropped.
func PersistLoggingOutput(path string, cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Logging.Output) != "" {
		return cfg, nil
	}
	cfg.Logging.Output = DefaultLogOutput()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Config{}, err
	}
	if err := WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
