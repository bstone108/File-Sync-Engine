package advertise

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

const tailscaleStatusURL = "http://100.100.100.100/localapi/v0/status"

type tailscaleStatus struct {
	Self struct {
		TailscaleIPs []string `json:"TailscaleIPs"`
	} `json:"Self"`
}

func queryTailscaleLocalAPI(ctx context.Context) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tailscaleStatusURL, nil)
	if err != nil {
		return nil
	}
	req.Host = "100.100.100.100"
	client := &http.Client{Timeout: 700 * time.Millisecond}
	response, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	var status tailscaleStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return nil
	}
	out := make([]string, 0, len(status.Self.TailscaleIPs))
	for _, raw := range status.Self.TailscaleIPs {
		ip := net.ParseIP(raw)
		if ip == nil || !isTailscaleIP(ip) {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}
