package advertise

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/routing"
	"github.com/huin/goupnp/dcps/internetgateway2"
	natpmp "github.com/jackpal/go-nat-pmp"
)

// GUIHostHeader carries the browser Host that reached the local web GUI.
// The proxy sets it from the request that opened the console. Direct API
// clients cannot supply it.
const GUIHostHeader = "X-FSE-GUI-Host"

// Observations are reachable-address hints learned after startup.
// GUIHost is verified by a TCP dial. ObservedIPs were reported by peers.
// ExternalIP and ExternalPort come from a successful UPnP or NAT-PMP mapping.
type Observations struct {
	GUIHost      string
	ObservedIPs  []string
	ExternalIP   string
	ExternalPort int
	Probe        func(context.Context, string, int) error
}

var (
	observationMu sync.Mutex
	refreshMu     sync.Mutex
	guiHost       string
	learnedIPs    []string
	natIP         string
	natPort       int
	boundPath     string
	probeFn       func(context.Context, string, int) error
	changeFn      func(string)
	lastAttempt   time.Time
)

var mapExternalPort = discoverMappedPort

// BindConfigPath is the config file refreshed when a verified address appears.
func BindConfigPath(path string) {
	observationMu.Lock()
	boundPath = path
	observationMu.Unlock()
}

// SetOnChange receives a newly stored advertise URL.
func SetOnChange(fn func(string)) {
	observationMu.Lock()
	changeFn = fn
	observationMu.Unlock()
}

// SetReachabilityProbe replaces the GUI-host TCP check. Production uses a dial.
func SetReachabilityProbe(fn func(context.Context, string, int) error) {
	observationMu.Lock()
	probeFn = fn
	observationMu.Unlock()
}

// CurrentObservations is the set Apply uses while choosing an advertise URL.
func CurrentObservations() Observations {
	observationMu.Lock()
	defer observationMu.Unlock()
	return Observations{
		GUIHost:      guiHost,
		ObservedIPs:  append([]string(nil), learnedIPs...),
		ExternalIP:   natIP,
		ExternalPort: natPort,
		Probe:        probeFn,
	}
}

// NoteGUIRequest records the console host only for requests that came through
// the local web GUI proxy. requestHost is the native request Host.
func NoteGUIRequest(requestHost, guiHostHeader string) {
	if !strings.EqualFold(strings.TrimSpace(requestHost), "fse-native") {
		return
	}
	NoteGUIHost(guiHostHeader)
}

// NoteGUIHost records the host the local GUI used. Loopback, bridge, and the
// in-process proxy name are ignored. A stored host-reachable URL is kept.
func NoteGUIHost(raw string) {
	host := hostname(raw)
	if !acceptableAutoHost(host) {
		return
	}
	observationMu.Lock()
	changed := guiHost != host
	guiHost = host
	observationMu.Unlock()
	scheduleRefresh(changed)
}

// NoteObservedIP records an address a remote peer observed for this node.
func NoteObservedIP(ip string) {
	ip = strings.TrimSpace(ip)
	if !acceptableAutoHost(ip) {
		return
	}
	observationMu.Lock()
	for _, existing := range learnedIPs {
		if existing == ip {
			observationMu.Unlock()
			scheduleRefresh(false)
			return
		}
	}
	learnedIPs = append(learnedIPs, ip)
	observationMu.Unlock()
	scheduleRefresh(true)
}

// NoteNAT records a mapping created by UPnP or NAT-PMP.
func NoteNAT(ip string, port int) {
	ip = strings.TrimSpace(ip)
	if port < 1 || port > 65535 || !acceptableAutoHost(ip) {
		return
	}
	observationMu.Lock()
	changed := natIP != ip || natPort != port
	natIP = ip
	natPort = port
	observationMu.Unlock()
	scheduleRefresh(changed)
}

func scheduleRefresh(force bool) {
	observationMu.Lock()
	path := boundPath
	if path == "" {
		observationMu.Unlock()
		return
	}
	if !force && time.Since(lastAttempt) < 2*time.Second {
		observationMu.Unlock()
		return
	}
	lastAttempt = time.Now()
	observationMu.Unlock()
	go func() { _ = RefreshNow(path) }()
}

// RefreshNow stores a better advertise URL when the current one is empty or a
// Docker bridge address. An explicit override and a saved host-reachable URL win.
func RefreshNow(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	refreshMu.Lock()
	url, err := persistRefreshedURL(path)
	refreshMu.Unlock()
	if url == "" {
		return err
	}
	observationMu.Lock()
	fn := changeFn
	observationMu.Unlock()
	if fn != nil {
		fn(url)
	}
	return err
}

func persistRefreshedURL(path string) (string, error) {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return "", err
	}
	before := cfg.API.AdvertiseURL
	applyErr := Apply(&cfg, os.Getenv(EnvVar))
	if cfg.API.AdvertiseURL == before {
		return "", applyErr
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := config.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return cfg.API.AdvertiseURL, applyErr
}

// StartAutoDetect asks the local gateway for a port mapping onto the published
// API port. Failure leaves the other detectors in place.
func StartAutoDetect(ctx context.Context, cfg config.Config) {
	internal := listenPortNumber(cfg.API.Listen)
	if internal == 0 {
		internal = 22420
	}
	external := PublishedAPIPort()
	if external == 0 {
		external = internal
	}
	go func() {
		if ctx == nil {
			ctx = context.Background()
		}
		detectCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		ip, port, err := mapExternalPort(detectCtx, internal, external)
		if err == nil && ip != "" && port != 0 {
			NoteNAT(ip, port)
			if mappingHook != nil {
				mappingHook(ip, port)
			}
			return
		}
		stunIP, _, stunErr := stunDiscover(detectCtx)
		if stunErr != nil || stunIP == "" {
			return
		}
		NoteObservedIP(stunIP)
	}()
}

var (
	mappingHook  func(string, int)
	stunDiscover = DiscoverExternalIP
)

// SetExternalMappingHook receives a successful UPnP or NAT-PMP mapping.
func SetExternalMappingHook(fn func(ip string, port int)) {
	mappingHook = fn
}

func observationURLs(cfg config.Config, obs Observations) []string {
	port := reachPort(cfg)
	out := []string{}
	if host := hostname(obs.GUIHost); acceptableAutoHost(host) {
		probe := obs.Probe
		if probe == nil {
			probe = dialProbe
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		err := probe(ctx, host, port)
		cancel()
		if err == nil {
			out = append(out, net.JoinHostPort(host, strconv.Itoa(port)))
		}
	}
	for _, ip := range obs.ObservedIPs {
		if acceptableAutoHost(ip) {
			out = append(out, net.JoinHostPort(ip, strconv.Itoa(port)))
		}
	}
	if obs.ExternalPort > 0 && obs.ExternalPort <= 65535 && acceptableAutoHost(obs.ExternalIP) {
		out = append(out, net.JoinHostPort(obs.ExternalIP, strconv.Itoa(obs.ExternalPort)))
	}
	return out
}

func reachPort(cfg config.Config) int {
	if port := PublishedAPIPort(); port > 0 {
		return port
	}
	if port := listenPortNumber(cfg.API.Listen); port > 0 {
		return port
	}
	return 22420
}

func acceptableAutoHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "fse-native") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return HostReachable(host)
	}
	if ip.IsLoopback() || !usableIP(ip) || routing.IsContainerBridgeIP(ip) {
		return false
	}
	return true
}

func dialProbe(ctx context.Context, host string, port int) error {
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func validMappingPorts(internalPort, externalPort int) error {
	if internalPort < 1 || internalPort > 65535 || externalPort < 1 || externalPort > 65535 {
		return fmt.Errorf("NAT port mapping %d->%d is outside 1-65535", internalPort, externalPort)
	}
	return nil
}

func discoverMappedPort(ctx context.Context, internalPort, externalPort int) (string, int, error) {
	if ip, port, err := natPMPMap(ctx, internalPort, externalPort); err == nil {
		return ip, port, nil
	}
	return upnpMap(ctx, internalPort, externalPort)
}

func natPMPMap(ctx context.Context, internalPort, externalPort int) (string, int, error) {
	if err := validMappingPorts(internalPort, externalPort); err != nil {
		return "", 0, err
	}
	gateway := defaultGateway()
	if gateway == nil {
		return "", 0, errNoGateway
	}
	timeout := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	client := natpmp.NewClientWithTimeout(gateway, timeout)
	type mapped struct {
		ip   string
		port int
		err  error
	}
	ch := make(chan mapped, 1)
	go func() {
		ext, err := client.GetExternalAddress()
		if err != nil {
			ch <- mapped{err: err}
			return
		}
		result, err := client.AddPortMapping("tcp", internalPort, externalPort, 3600)
		if err != nil {
			ch <- mapped{err: err}
			return
		}
		port := externalPort
		if result != nil && result.MappedExternalPort > 0 {
			port = int(result.MappedExternalPort)
		}
		ip := net.IPv4(ext.ExternalIPAddress[0], ext.ExternalIPAddress[1], ext.ExternalIPAddress[2], ext.ExternalIPAddress[3]).String()
		ch <- mapped{ip: ip, port: port}
	}()
	select {
	case <-ctx.Done():
		return "", 0, ctx.Err()
	case result := <-ch:
		return result.ip, result.port, result.err
	}
}

func upnpMap(ctx context.Context, internalPort, externalPort int) (string, int, error) {
	if err := validMappingPorts(internalPort, externalPort); err != nil {
		return "", 0, err
	}
	clients, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx)
	if err != nil && len(clients) == 0 {
		return "", 0, err
	}
	var last error = errNoGateway
	for _, client := range clients {
		if client == nil {
			continue
		}
		internal := ""
		if client.Location != nil {
			if dst := net.ParseIP(client.Location.Hostname()); dst != nil {
				internal = localIPToward(dst)
			}
		}
		if internal == "" {
			continue
		}
		// Reject values outside the protocol field before narrowing to uint16.
		if externalPort < 1 || externalPort > 65535 || internalPort < 1 || internalPort > 65535 {
			return "", 0, fmt.Errorf("NAT port mapping %d->%d is outside 1-65535", internalPort, externalPort)
		}
		mappedExternal := uint16(externalPort)
		mappedInternal := uint16(internalPort)
		if err := client.AddPortMappingCtx(ctx, "", mappedExternal, "TCP", mappedInternal, internal, true, "File Sync Engine", 3600); err != nil {
			last = err
			continue
		}
		ip, err := client.GetExternalIPAddressCtx(ctx)
		if err != nil {
			last = err
			continue
		}
		if net.ParseIP(strings.TrimSpace(ip)) == nil {
			continue
		}
		return strings.TrimSpace(ip), externalPort, nil
	}
	return "", 0, last
}

func localIPToward(dst net.IP) string {
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: dst, Port: 9})
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return ""
	}
	return addr.IP.String()
}

func defaultGateway() net.IP {
	data, err := readRouteTable()
	if err != nil {
		return nil
	}
	return gatewayFromRouteTable(string(data))
}

var readRouteTable = func() ([]byte, error) {
	return os.ReadFile("/proc/net/route")
}

func gatewayFromRouteTable(table string) net.IP {
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		ip := parseLittleEndianIPv4(fields[2])
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		return ip
	}
	return nil
}

func parseLittleEndianIPv4(hexword string) net.IP {
	if len(hexword) != 8 {
		return nil
	}
	var raw [4]byte
	for i := 0; i < 4; i++ {
		value, err := strconv.ParseUint(hexword[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil
		}
		raw[3-i] = byte(value)
	}
	return net.IPv4(raw[0], raw[1], raw[2], raw[3])
}

var errNoGateway = errString("default gateway not found")

type errString string

func (e errString) Error() string { return string(e) }

func resetObservations() {
	observationMu.Lock()
	defer observationMu.Unlock()
	guiHost = ""
	learnedIPs = nil
	natIP = ""
	natPort = 0
	boundPath = ""
	probeFn = nil
	changeFn = nil
	lastAttempt = time.Time{}
}
