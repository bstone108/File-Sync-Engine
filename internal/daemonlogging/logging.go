package daemonlogging

import (
	"os"
	"path/filepath"

	"filesyncengine/internal/config"
	"filesyncengine/internal/structuredlog"
)

func Configure(cfg config.Config, configPath string) error {
	output := cfg.Logging.Output
	if output != "" && output != "stderr" && output != "stdout" && !filepath.IsAbs(output) {
		output = filepath.Join(filepath.Dir(configPath), output)
	}
	if output != "" && output != "stderr" && output != "stdout" {
		if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
			return err
		}
	}
	return structuredlog.Configure(structuredlog.Options{Level: string(cfg.Logging.Level), Output: output})
}

func DiscoveryRouterUnavailable(err error) {
	structuredlog.Event("warn", "discovery.dht.unavailable", "public DHT router unavailable", map[string]any{"error": err.Error()})
}

func APIListening(listen string) {
	structuredlog.Event("info", "api.listening", "API server listening", map[string]any{"listen": listen})
}

func APIServerStopped(err error) {
	structuredlog.Event("warn", "api.stopped", "API server stopped", map[string]any{"error": err.Error()})
}

func MonitorUnavailable(err error) {
	structuredlog.Event("warn", "monitor.unavailable", "folder monitor unavailable", map[string]any{"error": err.Error()})
}

func ConfigReloadRejected(err error) {
	structuredlog.Event("warn", "config.reload.rejected", "config reload pending or rejected; keeping last good config", map[string]any{"error": err.Error()})
}

func ReloadedMetadataStoreOpenFailed(err error) {
	structuredlog.Event("error", "metadata.store.reload_failed", "open reloaded metadata store failed", map[string]any{"error": err.Error()})
}

func MonitorRebuildFailed(err error) {
	structuredlog.Event("warn", "monitor.rebuild_failed", "folder monitor rebuild failed; keeping previous monitor set", map[string]any{"error": err.Error()})
}

func DaemonStarted(node string, folders int, configPath string) {
	structuredlog.Event("info", "daemon.started", "file synchronization engine started", map[string]any{"node": node, "folders": folders, "config": configPath})
}

func ConfigReloaded(folders int) {
	structuredlog.Event("info", "config.reloaded", "configuration adopted after quiet period", map[string]any{"folders": folders})
}
