package advertise

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

const tailscaleStatusURL = "http://100.100.100.100/localapi/v0/status"

type tailscaleStatus struct {
	Self struct {
		TailscaleIPs []string `json:"TailscaleIPs"`
	} `json:"Self"`
}

// TailscaleHosts returns this node's Tailscale addresses from the local API.
// The HTTP endpoint is used when the interface is hidden from the container,
// and the tailscaled socket is used when the host socket is mounted.
var (
	tailscaleCacheMu sync.Mutex
	tailscaleCached  []string
	tailscaleCacheAt time.Time
)

func TailscaleHosts(ctx context.Context) []string {
	tailscaleCacheMu.Lock()
	if !tailscaleCacheAt.IsZero() && time.Since(tailscaleCacheAt) < time.Minute {
		out := append([]string(nil), tailscaleCached...)
		tailscaleCacheMu.Unlock()
		return out
	}
	tailscaleCacheMu.Unlock()
	ips := queryTailscaleLocalAPI(ctx)
	if len(ips) == 0 {
		ips = queryTailscaleSocket(ctx)
	}
	tailscaleCacheMu.Lock()
	tailscaleCached = append([]string(nil), ips...)
	tailscaleCacheAt = time.Now()
	tailscaleCacheMu.Unlock()
	return append([]string(nil), ips...)
}

func queryTailscaleSocket(ctx context.Context) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, path := range []string{"/var/run/tailscale/tailscaled.sock", "/run/tailscale/tailscaled.sock"} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
		transport := &http.Transport{
			DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(dialCtx, "unix", path)
			},
		}
		client := &http.Client{Timeout: 700 * time.Millisecond, Transport: transport}
		req, err := http.NewRequestWithContext(attempt, http.MethodGet, "http://local-tailscaled.sock/localapi/v0/status", nil)
		if err != nil {
			cancel()
			continue
		}
		response, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}
		ips := decodeTailscaleStatus(response)
		response.Body.Close()
		cancel()
		if len(ips) > 0 {
			return ips
		}
	}
	return nil
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
	return decodeTailscaleStatus(response)
}

func decodeTailscaleStatus(response *http.Response) []string {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
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
