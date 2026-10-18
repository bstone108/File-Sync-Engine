package advertise

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pion/stun/v3"
)

// DefaultSTUNServers are the public STUN servers used when FSE_STUN_SERVERS
// and discovery.nat.stunServers are empty.
var DefaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// EnvSTUNServers overrides the STUN server list. "off" disables STUN.
const EnvSTUNServers = "FSE_STUN_SERVERS"

var configuredSTUN []string

// SetSTUNServers stores the configured server list. The environment variable
// still wins when it is set.
func SetSTUNServers(servers []string) {
	configuredSTUN = append([]string(nil), servers...)
}

// STUNServers returns the servers to query. An empty list disables STUN.
func STUNServers() []string {
	raw := strings.TrimSpace(os.Getenv(EnvSTUNServers))
	switch strings.ToLower(raw) {
	case "off", "-", "none", "false", "0":
		return nil
	case "":
		if len(configuredSTUN) > 0 {
			return append([]string(nil), configuredSTUN...)
		}
		return append([]string(nil), DefaultSTUNServers...)
	default:
		return splitServers(raw)
	}
}

func splitServers(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// DiscoverExternalIP asks public STUN servers for the mapped address of this
// socket. The mapped port belongs to the query socket, so callers use the IP
// with a published or mapped port rather than advertising the STUN port as the
// API port.
func DiscoverExternalIP(ctx context.Context) (string, int, error) {
	servers := STUNServers()
	if len(servers) == 0 {
		return "", 0, fmt.Errorf("STUN is disabled")
	}
	var last error
	for _, server := range servers {
		ip, port, err := querySTUN(ctx, server)
		if err != nil {
			last = err
			continue
		}
		return ip, port, nil
	}
	if last == nil {
		last = fmt.Errorf("STUN returned no address")
	}
	return "", 0, last
}

func querySTUN(ctx context.Context, server string) (string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "udp", server)
	if err != nil {
		return "", 0, err
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	_ = conn.SetDeadline(deadline)
	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := conn.Write(req.Raw); err != nil {
		return "", 0, err
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return "", 0, err
	}
	msg := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
	if err := msg.Decode(); err != nil {
		return "", 0, err
	}
	var mapped stun.XORMappedAddress
	if err := mapped.GetFrom(msg); err != nil {
		return "", 0, err
	}
	if mapped.IP == nil || mapped.Port == 0 {
		return "", 0, fmt.Errorf("STUN mapped address was empty")
	}
	return mapped.IP.String(), mapped.Port, nil
}
