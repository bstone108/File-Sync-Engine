package daemoncontrol

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/pairing"
)

func RequestWebGUI(configPath string, action string, requester Requester) ([]byte, error) {
	body, err := json.Marshal(api.WebGUICommandRequest{Action: action})
	if err != nil {
		return nil, err
	}
	return requestDaemon(configPath, http.MethodPost, "/v1/web-gui-command", requester, bytes.NewReader(body))
}

func RequestIdentityExport(configPath string, groupID string, requester Requester) ([]byte, error) {
	body, err := json.Marshal(api.IdentityPackageRequest{GroupID: groupID})
	if err != nil {
		return nil, err
	}
	return requestDaemon(configPath, http.MethodPost, "/v1/identity-package", requester, bytes.NewReader(body))
}

func RequestIdentityImport(configPath string, packagePath string, requester Requester) ([]byte, error) {
	packageBytes, err := os.ReadFile(packagePath)
	if err != nil {
		return nil, err
	}
	var identityPackage pairing.IdentityPackage
	if err := json.Unmarshal(packageBytes, &identityPackage); err != nil {
		return nil, err
	}
	body, err := json.Marshal(api.IdentityImportRequest{Package: identityPackage})
	if err != nil {
		return nil, err
	}
	return requestDaemon(configPath, http.MethodPost, "/v1/identity-import", requester, bytes.NewReader(body))
}

func requestDaemon(configPath string, method string, path string, requester Requester, body io.Reader) ([]byte, error) {
	cfg, _, err := config.EnsureAPIKey(configPath)
	if err != nil {
		return nil, err
	}
	if err := config.EnsureAPITLSAssets(&cfg, configPath); err != nil {
		return nil, err
	}
	return requester(cfg, method, path, body)
}
