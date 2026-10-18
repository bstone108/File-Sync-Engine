package link

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"filesyncengine/internal/structuredlog"
)

const (
	// LocalPairPort is the TCP and UDP port engines use to hand a pairing
	// record to another engine on the same network. It does not need a
	// published host port when both engines share a Docker network or LAN.
	LocalPairPort = 22427
	localPairType = "fse.pair.v1"
	maxLocalPair  = 64 * 1024
)

type localPairMessage struct {
	Type      string `json:"type"`
	Op        string `json:"op"`
	Namespace string `json:"namespace"`
	Payload   []byte `json:"payload,omitempty"`
}

// LocalDirectory exchanges pairing records on the local network.
// Publish stores the record and answers TCP dials plus UDP queries.
// Lookup dials other hosts on this subnet and broadcasts a query.
type LocalDirectory struct {
	tcp     net.Listener
	udp     net.PacketConn
	port    int
	mu      sync.Mutex
	payload map[string][]byte
	dials   []string
	done    chan struct{}
}

// ListenLocalDirectory serves pairing records. An empty address uses
// LocalPairPort on every interface.
func ListenLocalDirectory(addr string) (*LocalDirectory, error) {
	if addr == "" {
		addr = fmt.Sprintf(":%d", LocalPairPort)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || tcpAddr.Port == 0 {
		_ = ln.Close()
		return nil, errors.New("local pairing listener has no TCP port")
	}
	udpAddr := &net.UDPAddr{IP: tcpAddr.IP, Port: tcpAddr.Port}
	if udpAddr.IP == nil || udpAddr.IP.IsUnspecified() {
		udpAddr.IP = net.IPv4zero
	}
	packet, err := listenUDP(udpAddr)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	dir := &LocalDirectory{
		tcp:     ln,
		udp:     packet,
		port:    tcpAddr.Port,
		payload: map[string][]byte{},
		done:    make(chan struct{}),
	}
	go dir.acceptTCP()
	go dir.readUDP()
	return dir, nil
}

func listenUDP(addr *net.UDPAddr) (net.PacketConn, error) {
	lc := net.ListenConfig{Control: broadcastControl}
	packet, err := lc.ListenPacket(context.Background(), "udp4", addr.String())
	if err != nil {
		return net.ListenUDP("udp4", addr)
	}
	return packet, nil
}

// SetDialTargets replaces subnet scanning. Tests pass the publisher address.
func (d *LocalDirectory) SetDialTargets(targets []string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials = append([]string(nil), targets...)
}

func (d *LocalDirectory) Addr() string {
	if d == nil || d.tcp == nil {
		return ""
	}
	return d.tcp.Addr().String()
}

func (d *LocalDirectory) Close() error {
	if d == nil {
		return nil
	}
	select {
	case <-d.done:
	default:
		close(d.done)
	}
	var err error
	if d.tcp != nil {
		err = d.tcp.Close()
	}
	if d.udp != nil {
		if udpErr := d.udp.Close(); err == nil {
			err = udpErr
		}
	}
	return err
}

func (d *LocalDirectory) Publish(ctx context.Context, namespace string, payload []byte) error {
	if namespace == "" || len(payload) == 0 {
		return errors.New("pairing record is incomplete")
	}
	if len(payload) > maxLocalPair {
		return fmt.Errorf("pairing record exceeds %d bytes", maxLocalPair)
	}
	d.mu.Lock()
	if ctx != nil && ctx.Err() != nil {
		d.mu.Unlock()
		return ctx.Err()
	}
	d.payload[namespace] = append([]byte(nil), payload...)
	d.mu.Unlock()
	d.broadcast(localPairMessage{Type: localPairType, Op: "record", Namespace: namespace, Payload: payload})
	return nil
}

func (d *LocalDirectory) Unpublish(_ context.Context, namespace string) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	delete(d.payload, namespace)
	d.mu.Unlock()
	return nil
}

func (d *LocalDirectory) Lookup(ctx context.Context, namespace string) ([][]byte, error) {
	if d == nil {
		return nil, errors.New("local pairing directory is not configured")
	}
	if namespace == "" {
		return nil, errors.New("pairing namespace is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if payload := d.stored(namespace); len(payload) > 0 {
		return [][]byte{payload}, nil
	}
	d.broadcast(localPairMessage{Type: localPairType, Op: "query", Namespace: namespace})
	targets := d.dialTargets()
	fetched := make(chan [][]byte, 1)
	go func() {
		fetched <- fetchPairingTargets(ctx, targets, namespace)
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var tcp [][]byte
	tcpDone := false
	for {
		if payload := d.stored(namespace); len(payload) > 0 {
			cancel()
			structuredlog.Event("info", "pairing.lookup.local", "pairing record found on the local network", map[string]any{"namespace": namespace})
			return [][]byte{payload}, nil
		}
		select {
		case tcp = <-fetched:
			tcpDone = true
			if len(tcp) > 0 {
				structuredlog.Event("info", "pairing.lookup.local", "pairing record found on the local network", map[string]any{"namespace": namespace})
				return tcp, nil
			}
		case <-ticker.C:
		case <-ctx.Done():
			if !tcpDone {
				tcp = <-fetched
			}
			if payload := d.stored(namespace); len(payload) > 0 {
				structuredlog.Event("info", "pairing.lookup.local", "pairing record found on the local network", map[string]any{"namespace": namespace})
				return [][]byte{payload}, nil
			}
			return tcp, nil
		}
	}
}

func (d *LocalDirectory) rememberDialed(namespace, target string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	payload, err := fetchPairingTCP(ctx, target, namespace)
	if err != nil || len(payload) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.payload[namespace]; !exists {
		d.payload[namespace] = append([]byte(nil), payload...)
	}
}

func (d *LocalDirectory) stored(namespace string) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]byte(nil), d.payload[namespace]...)
}

func (d *LocalDirectory) dialTargets() []string {
	d.mu.Lock()
	targets := append([]string(nil), d.dials...)
	d.mu.Unlock()
	if targets != nil {
		return targets
	}
	return scanLocalPairHosts(d.port)
}

func (d *LocalDirectory) acceptTCP() {
	for {
		conn, err := d.tcp.Accept()
		if err != nil {
			return
		}
		go d.serveConn(conn)
	}
}

func (d *LocalDirectory) serveConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	reader := bufio.NewReader(io.LimitReader(conn, 512))
	namespace, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	namespace = trimSpace(namespace)
	payload := d.stored(namespace)
	var lengthBuf [4]byte
	binary.BigEndian.PutUint32(lengthBuf[:], uint32(len(payload)))
	if _, err := conn.Write(lengthBuf[:]); err != nil {
		return
	}
	if len(payload) > 0 {
		_, _ = conn.Write(payload)
	}
}

func (d *LocalDirectory) readUDP() {
	buf := make([]byte, maxLocalPair+512)
	for {
		n, from, err := d.udp.ReadFrom(buf)
		if err != nil {
			return
		}
		var msg localPairMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil || msg.Type != localPairType || msg.Namespace == "" {
			continue
		}
		switch msg.Op {
		case "query":
			payload := d.stored(msg.Namespace)
			if len(payload) == 0 {
				continue
			}
			// A datagram cannot carry a full reconnect profile. The querier
			// dials this process back on the pairing port.
			op := "record"
			if len(payload) > 800 {
				op = "fetch"
				payload = nil
			}
			reply, err := json.Marshal(localPairMessage{Type: localPairType, Op: op, Namespace: msg.Namespace, Payload: payload})
			if err != nil {
				continue
			}
			_, _ = d.udp.WriteTo(reply, from)
		case "fetch":
			host, ok := from.(*net.UDPAddr)
			if !ok || host == nil || host.IP == nil {
				continue
			}
			target := net.JoinHostPort(host.IP.String(), fmt.Sprintf("%d", d.port))
			namespace := msg.Namespace
			go d.rememberDialed(namespace, target)
		case "record":
			if len(msg.Payload) == 0 || len(msg.Payload) > maxLocalPair {
				continue
			}
			d.mu.Lock()
			if _, exists := d.payload[msg.Namespace]; !exists {
				d.payload[msg.Namespace] = append([]byte(nil), msg.Payload...)
			}
			d.mu.Unlock()
		}
	}
}

func (d *LocalDirectory) broadcast(msg localPairMessage) {
	if d == nil || d.udp == nil {
		return
	}
	body, err := json.Marshal(msg)
	if err != nil || len(body) > 1200 {
		return
	}
	for _, addr := range broadcastTargets(d.port) {
		_, _ = d.udp.WriteTo(body, addr)
	}
}

func fetchPairingTargets(ctx context.Context, targets []string, namespace string) [][]byte {
	if len(targets) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		payloads [][]byte
		wg       sync.WaitGroup
		sem      = make(chan struct{}, 64)
	)
launch:
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			break launch
		case sem <- struct{}{}:
			wg.Add(1)
			go func(target string) {
				defer wg.Done()
				defer func() { <-sem }()
				payload, err := fetchPairingTCP(ctx, target, namespace)
				if err != nil || len(payload) == 0 {
					return
				}
				mu.Lock()
				payloads = append(payloads, payload)
				if len(payloads) == 1 {
					cancel()
				}
				mu.Unlock()
			}(target)
		}
	}
	wg.Wait()
	return payloads
}

func fetchPairingTCP(ctx context.Context, target, namespace string) ([]byte, error) {
	dialer := net.Dialer{Timeout: 300 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(800 * time.Millisecond))
	if _, err := io.WriteString(conn, namespace+"\n"); err != nil {
		return nil, err
	}
	var lengthBuf [4]byte
	if _, err := io.ReadFull(conn, lengthBuf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf[:])
	if length == 0 {
		return nil, nil
	}
	if length > maxLocalPair {
		return nil, fmt.Errorf("pairing record exceeds %d bytes", maxLocalPair)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func scanLocalPairHosts(port int) []string {
	ifaces, err := net.Interfaces()
	if err != nil || port <= 0 {
		return nil
	}
	var hosts []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			hosts = append(hosts, hostsInNet(ipnet, port)...)
		}
	}
	return hosts
}

func hostsInNet(ipnet *net.IPNet, port int) []string {
	ip4 := ipnet.IP.To4()
	if ip4 == nil || ipnet.Mask == nil {
		return nil
	}
	mask := ipnet.Mask
	if len(mask) == 16 {
		mask = mask[12:]
	}
	ones, bits := mask.Size()
	if bits != 32 || ones < 24 || ones > 30 {
		return nil
	}
	count := 1 << (32 - ones)
	network := ip4.Mask(mask)
	self := ip4
	hosts := make([]string, 0, count)
	for i := 1; i < count-1; i++ {
		host := append(net.IP(nil), network...)
		addToIP(host, i)
		if host.Equal(self) {
			continue
		}
		hosts = append(hosts, net.JoinHostPort(host.String(), fmt.Sprintf("%d", port)))
	}
	return hosts
}

func addToIP(ip net.IP, delta int) {
	for i := len(ip) - 1; i >= 0 && delta > 0; i-- {
		sum := int(ip[i]) + delta
		ip[i] = byte(sum)
		delta = sum >> 8
	}
}

func broadcastTargets(port int) []net.Addr {
	targets := []net.Addr{&net.UDPAddr{IP: net.IPv4bcast, Port: port}}
	ifaces, err := net.Interfaces()
	if err != nil {
		return targets
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			bcast := broadcastIP(ipnet)
			if bcast == nil {
				continue
			}
			targets = append(targets, &net.UDPAddr{IP: bcast, Port: port})
		}
	}
	return targets
}

func broadcastIP(ipnet *net.IPNet) net.IP {
	ip4 := ipnet.IP.To4()
	if ip4 == nil || len(ipnet.Mask) == 0 {
		return nil
	}
	mask := ipnet.Mask
	if len(mask) == 16 {
		mask = mask[12:]
	}
	if len(mask) != 4 {
		return nil
	}
	out := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		out[i] = ip4[i] | ^mask[i]
	}
	return out
}

func trimSpace(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r' || value[len(value)-1] == ' ') {
		value = value[:len(value)-1]
	}
	return value
}
