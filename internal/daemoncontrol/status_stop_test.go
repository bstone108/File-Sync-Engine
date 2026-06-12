package daemoncontrol

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestRequestStatusAndStopEnsureAPIKeyTLSAssetsAndUseDaemonEndpoints(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"nodeName":"node-a","api":{"listen":"0.0.0.0:22420","encryption":{"mode":"auto"}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	calls := []string{}
	requester := func(cfg config.Config, method, path string, body io.Reader) ([]byte, error) {
		calls = append(calls, method+" "+path)
		if cfg.API.Key == "" {
			t.Fatalf("API key was not generated before daemon request")
		}
		if cfg.API.Encryption.CertFile == "" || cfg.API.Encryption.KeyFile == "" {
			t.Fatalf("TLS assets were not generated for non-loopback auto API before daemon request: %+v", cfg.API.Encryption)
		}
		return []byte(method + " " + path + " ok"), nil
	}

	statusBody, err := RequestStatus(configPath, requester)
	if err != nil {
		t.Fatalf("RequestStatus: %v", err)
	}
	stopBody, err := RequestStop(configPath, requester)
	if err != nil {
		t.Fatalf("RequestStop: %v", err)
	}

	if string(statusBody) != "GET /v1/status ok" || string(stopBody) != "POST /v1/stop ok" {
		t.Fatalf("unexpected response bodies: status=%q stop=%q", statusBody, stopBody)
	}
	if len(calls) != 2 || calls[0] != http.MethodGet+" /v1/status" || calls[1] != http.MethodPost+" /v1/stop" {
		t.Fatalf("unexpected daemon calls: %v", calls)
	}
	reloaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.API.Key == "" {
		t.Fatalf("generated API key was not persisted: %+v", reloaded.API)
	}
}
