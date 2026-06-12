package daemoncontrol

import (
	"io"
	"net/http"

	"filesyncengine/internal/config"
)

type Requester func(config.Config, string, string, io.Reader) ([]byte, error)

func RequestStatus(configPath string, requester Requester) ([]byte, error) {
	return requestDaemon(configPath, http.MethodGet, "/v1/status", requester, nil)
}

func RequestStop(configPath string, requester Requester) ([]byte, error) {
	return requestDaemon(configPath, http.MethodPost, "/v1/stop", requester, nil)
}
