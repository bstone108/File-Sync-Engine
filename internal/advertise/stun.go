package advertise

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/structuredlog"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
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

// NAT classes learned by comparing STUN mappings of the libp2p UDP socket.
const (
	NATCone      = "cone"
	NATSymmetric = "symmetric"
	NATUnknown   = "unknown"
	NATDisabled  = "disabled"
	NATPending   = "pending"
)

// STUNInterval is how often the libp2p QUIC socket repeats the binding
// requests. The first probe runs at startup.
var STUNInterval = 2 * time.Minute

// NATStatus is the latest STUN classification of the libp2p UDP port.
// Advertise is true when the mapping is stable enough to announce as a QUIC
// candidate. A symmetric mapping is recorded and is not advertised.
type NATStatus struct {
	Type      string
	IP        string
	Port      int
	Mapped    string
	Advertise bool
}

var (
	natStatusMu sync.Mutex
	natStatus   NATStatus
)

// SetNATStatus stores a classification. Tests use it to drive announcements.
func SetNATStatus(status NATStatus) {
	natStatusMu.Lock()
	natStatus = status
	natStatusMu.Unlock()
}

// ResetNATStatus clears the stored classification.
func ResetNATStatus() {
	SetNATStatus(NATStatus{})
}

// CurrentNATStatus is the last stored classification.
func CurrentNATStatus() NATStatus {
	natStatusMu.Lock()
	defer natStatusMu.Unlock()
	return natStatus
}

// ReportedNAT is the classification shown on /v1/status. An empty store is
// pending while servers are configured, and disabled when STUN is off.
func ReportedNAT() (natType, mapped string) {
	status := CurrentNATStatus()
	if status.Type == "" {
		if len(STUNServers()) == 0 {
			return NATDisabled, ""
		}
		return NATPending, ""
	}
	return status.Type, status.Mapped
}

// STUNSample is one server's mapping of the socket that sent the request.
type STUNSample struct {
	Server string
	IP     string
	Port   int
	Err    error
}

// ClassifySTUN compares mappings. Equal ip:port values are an
// endpoint-independent (cone) NAT. Different values are symmetric. A single
// success stays unknown and may still be announced.
func ClassifySTUN(samples []STUNSample) NATStatus {
	ok := make([]STUNSample, 0, len(samples))
	for _, sample := range samples {
		if sample.Err != nil || sample.IP == "" || sample.Port < 1 || sample.Port > 65535 {
			continue
		}
		ok = append(ok, sample)
	}
	if len(ok) == 0 {
		return NATStatus{Type: NATUnknown}
	}
	status := NATStatus{
		Type:   NATUnknown,
		IP:     ok[0].IP,
		Port:   ok[0].Port,
		Mapped: net.JoinHostPort(ok[0].IP, strconv.Itoa(ok[0].Port)),
	}
	if len(ok) == 1 {
		status.Advertise = mappedIsPublic(status.IP)
		return status
	}
	same := true
	for _, sample := range ok[1:] {
		if sample.IP != ok[0].IP || sample.Port != ok[0].Port {
			same = false
			break
		}
	}
	if same {
		status.Type = NATCone
		status.Advertise = mappedIsPublic(status.IP)
		return status
	}
	status.Type = NATSymmetric
	return status
}

func mappedIsPublic(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	var raw string
	if v4 := parsed.To4(); v4 != nil {
		raw = "/ip4/" + v4.String()
	} else {
		raw = "/ip6/" + parsed.String()
	}
	addr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return false
	}
	return manet.IsPublicAddr(addr)
}

// ObserveSTUN sends a binding request to each configured server on conn and
// stores the classification. conn must be the libp2p QUIC UDP socket so the
// mapped port belongs to that listener. The mapped port is not an API port.
func ObserveSTUN(ctx context.Context, conn net.PacketConn) NATStatus {
	if ctx == nil {
		ctx = context.Background()
	}
	servers := STUNServers()
	if len(servers) == 0 {
		status := NATStatus{Type: NATDisabled}
		SetNATStatus(status)
		structuredlog.Event("info", "nat.stun.disabled", "STUN is not querying the libp2p UDP port", map[string]any{
			"natType": NATDisabled,
		})
		return status
	}
	armSTUNRead(conn)
	samples := make([]STUNSample, 0, len(servers))
	for _, server := range servers {
		if ctx.Err() != nil {
			break
		}
		ip, port, err := QuerySTUNPacket(ctx, conn, server)
		if err != nil {
			structuredlog.Event("warn", "nat.stun.failed", "STUN server did not map the libp2p UDP port", map[string]any{
				"server": server,
				"error":  err.Error(),
			})
			samples = append(samples, STUNSample{Server: server, Err: err})
			continue
		}
		mapped := net.JoinHostPort(ip, strconv.Itoa(port))
		structuredlog.Event("info", "nat.stun.ok", "STUN mapped the libp2p UDP port", map[string]any{
			"server": server,
			"mapped": mapped,
		})
		samples = append(samples, STUNSample{Server: server, IP: ip, Port: port})
		NoteObservedIP(ip)
	}
	status := ClassifySTUN(samples)
	SetNATStatus(status)
	fields := map[string]any{
		"natType":   status.Type,
		"advertise": status.Advertise,
	}
	if status.Mapped != "" {
		fields["mapped"] = status.Mapped
	}
	structuredlog.Event("info", "nat.stun.result", "STUN classified the NAT in front of the libp2p UDP port", fields)
	return status
}

// armSTUNRead turns on quic-go's non-QUIC packet queue. Packets that arrive
// before the first read are dropped, so the queue is armed before the request.
func armSTUNRead(conn net.PacketConn) {
	if conn == nil {
		return
	}
	_ = conn.SetReadDeadline(time.Unix(0, 0))
	buf := make([]byte, 1500)
	_, _, _ = conn.ReadFrom(buf)
}

func resolveSTUNIPv4(ctx context.Context, server string) (*net.UDPAddr, error) {
	host, portRaw, err := net.SplitHostPort(server)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("STUN server %s has no UDP port", server)
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return nil, fmt.Errorf("STUN server %s is not IPv4", server)
		}
		return &net.UDPAddr{IP: ip.To4(), Port: port}, nil
	}
	var resolver net.Resolver
	ips, err := resolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 || ips[0].To4() == nil {
		return nil, fmt.Errorf("STUN server %s has no IPv4 address", server)
	}
	return &net.UDPAddr{IP: ips[0].To4(), Port: port}, nil
}

// QuerySTUNPacket sends one binding request on conn and returns the mapped
// address. conn is not dialed or closed.
func QuerySTUNPacket(ctx context.Context, conn net.PacketConn, server string) (string, int, error) {
	if conn == nil {
		return "", 0, fmt.Errorf("STUN socket is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	remote, err := resolveSTUNIPv4(ctx, server)
	if err != nil {
		return "", 0, err
	}
	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	deadline := time.Now().Add(2 * time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	_ = conn.SetReadDeadline(deadline)
	if _, err := conn.WriteTo(req.Raw, remote); err != nil {
		return "", 0, err
	}
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return "", 0, err
		}
		if n < 20 {
			continue
		}
		msg := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
		if err := msg.Decode(); err != nil {
			continue
		}
		if msg.TransactionID != req.TransactionID {
			continue
		}
		if msg.Type != stun.BindingSuccess {
			return "", 0, fmt.Errorf("STUN response was %s", msg.Type)
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
}

// DiscoverExternalIP asks public STUN servers for the mapped address of a
// fresh UDP socket. That port is not the libp2p port. The daemon probes the
// libp2p QUIC socket with ObserveSTUN instead.
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
