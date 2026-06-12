package daemoncontrol

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestRequestWebGUIUsesDaemonCommandEndpoint(t *testing.T) {
	configPath := writeDaemonControlConfig(t)

	var methodPath string
	var bodyText string
	requester := func(cfg config.Config, method, path string, body io.Reader) ([]byte, error) {
		methodPath = method + " " + path
		bodyBytes, err := io.ReadAll(body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		bodyText = string(bodyBytes)
		if cfg.API.Key == "" {
			t.Fatalf("API key was not generated")
		}
		return []byte(`{"status":"ok"}`), nil
	}

	out, err := RequestWebGUI(configPath, "start", requester)
	if err != nil {
		t.Fatalf("RequestWebGUI: %v", err)
	}
	if string(out) != `{"status":"ok"}` {
		t.Fatalf("unexpected response: %s", out)
	}
	if methodPath != http.MethodPost+" /v1/web-gui-command" {
		t.Fatalf("unexpected request: %s", methodPath)
	}
	if !strings.Contains(bodyText, `"action":"start"`) {
		t.Fatalf("web GUI request body did not include action: %s", bodyText)
	}
}

func TestRequestIdentityExportAndImportUseDaemonEndpoints(t *testing.T) {
	configPath := writeDaemonControlConfig(t)
	packagePath := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(packagePath, []byte(`{"version":"fse-identity-package-v1","groupId":"family-sync"}`), 0o600); err != nil {
		t.Fatalf("write identity package: %v", err)
	}

	calls := []string{}
	bodies := []map[string]any{}
	requester := func(cfg config.Config, method, path string, body io.Reader) ([]byte, error) {
		calls = append(calls, method+" "+path)
		bodyBytes, err := io.ReadAll(body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
			t.Fatalf("decode body %q: %v", bodyBytes, err)
		}
		bodies = append(bodies, decoded)
		return []byte(`{"status":"accepted"}`), nil
	}

	if _, err := RequestIdentityExport(configPath, "family-sync", requester); err != nil {
		t.Fatalf("RequestIdentityExport: %v", err)
	}
	if _, err := RequestIdentityImport(configPath, packagePath, requester); err != nil {
		t.Fatalf("RequestIdentityImport: %v", err)
	}

	wantCalls := []string{http.MethodPost + " /v1/identity-package", http.MethodPost + " /v1/identity-import"}
	if len(calls) != len(wantCalls) || calls[0] != wantCalls[0] || calls[1] != wantCalls[1] {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if bodies[0]["groupId"] != "family-sync" {
		t.Fatalf("export body missing groupId: %#v", bodies[0])
	}
	pkg, ok := bodies[1]["package"].(map[string]any)
	if !ok || pkg["groupId"] != "family-sync" || pkg["version"] != "fse-identity-package-v1" {
		t.Fatalf("import body missing package: %#v", bodies[1])
	}
}

func writeDaemonControlConfig(t *testing.T) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"nodeName":"node-a","api":{"listen":"127.0.0.1:22420"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}
