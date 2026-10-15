// Package advertise chooses the URL other engines use to reach this node's API.
package advertise

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"filesyncengine/internal/config"
)

// EnvVar overrides auto-detection. Set it at install or first start.
const EnvVar = "FSE_ADVERTISE_URL"

// Ensure persists the reachable API URL when it is missing or overridden.
// An empty FSE_ADVERTISE_URL keeps a URL already stored in config.
func Ensure(configPath string) (config.Config, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return config.Config{}, err
	}
	before := cfg.API.AdvertiseURL
	if err := Apply(&cfg, os.Getenv(EnvVar)); err != nil {
		return cfg, err
	}
	if cfg.API.AdvertiseURL == before {
		return cfg, nil
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return config.Config{}, err
	}
	if err := config.WriteFileAtomic(configPath, append(data, '\n'), 0o600); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

// Apply sets cfg.API.AdvertiseURL from the environment override, the saved
// value, or a detected local address.
func Apply(cfg *config.Config, envOverride string) error {
	resolved, err := ResolveWithHost(*cfg, envOverride, DetectHost())
	if err != nil {
		return err
	}
	cfg.API.AdvertiseURL = resolved
	return nil
}

// ResolveWithHost picks the reachable API URL. envOverride wins, then a URL
// already stored on cfg, then detectedHost.
func ResolveWithHost(cfg config.Config, envOverride, detectedHost string) (string, error) {
	if strings.TrimSpace(envOverride) != "" {
		return Normalize(envOverride, cfg)
	}
	if strings.TrimSpace(cfg.API.AdvertiseURL) != "" {
		return Normalize(cfg.API.AdvertiseURL, cfg)
	}
	if strings.TrimSpace(detectedHost) == "" {
		return "", errors.New("could not detect a non-loopback address; set FSE_ADVERTISE_URL")
	}
	return Normalize(detectedHost, cfg)
}

// Normalize turns a host or URL into scheme://host:port using the API listen port.
func Normalize(raw string, cfg config.Config) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("advertise URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = schemeFor(cfg) + "://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("advertise URL %q is not a valid http(s) URL", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("advertise URL must be http or https")
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = apiPort(cfg.API.Listen)
	}
	return parsed.Scheme + "://" + net.JoinHostPort(host, port), nil
}

// DetectHost returns a non-loopback address for this machine.
func DetectHost() string {
	conn, err := net.Dial("udp", "192.0.2.1:80")
	if err == nil {
		defer conn.Close()
		if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok && usableIP(udp.IP) {
			return udp.IP.String()
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if usableIP(ip) && ip.To4() != nil {
				return ip.String()
			}
		}
	}
	return ""
}

func schemeFor(cfg config.Config) string {
	if cfg.API.RequiresTLS() {
		return "https"
	}
	return "http"
}

func apiPort(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "22420"
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		trimmed := strings.TrimPrefix(listen, ":")
		if trimmed != "" && trimmed != listen {
			return trimmed
		}
		return "22420"
	}
	if port == "" {
		return "22420"
	}
	return port
}

func usableIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}
