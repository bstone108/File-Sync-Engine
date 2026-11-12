package nat

import (
	"bufio"
	"os"
	"strings"
)

// TunnelInterface reports names that carry a VPN or other tunnel, not a LAN NIC.
func TunnelInterface(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case n == "tun" || strings.HasPrefix(n, "tun"):
		return true
	case n == "tap" || strings.HasPrefix(n, "tap"):
		return true
	case strings.HasPrefix(n, "wg") || strings.Contains(n, "wireguard") || strings.HasPrefix(n, "awg"):
		return true
	case strings.HasPrefix(n, "tailscale") || strings.HasPrefix(n, "utun"):
		return true
	case strings.HasPrefix(n, "ipsec") || strings.HasPrefix(n, "ppp"):
		return true
	case strings.HasPrefix(n, "zt") || strings.HasPrefix(n, "nordlynx"):
		return true
	case strings.Contains(n, "proton") || strings.Contains(n, "mullvad") || strings.Contains(n, "outline"):
		return true
	default:
		return false
	}
}

// DefaultRouteIsTunnel reports whether the IPv4 default route leaves through a
// VPN or tunnel interface. A missing route table is not a tunnel.
func DefaultRouteIsTunnel() bool {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if fields[1] != "00000000" {
			continue
		}
		return TunnelInterface(fields[0])
	}
	return false
}
