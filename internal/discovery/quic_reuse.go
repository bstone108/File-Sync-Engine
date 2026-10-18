package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ws "github.com/libp2p/go-libp2p/p2p/transport/websocket"
	webtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"
	quicgo "github.com/quic-go/quic-go"
	"go.uber.org/fx"

	"filesyncengine/internal/advertise"
	"filesyncengine/internal/structuredlog"
)

// quicBuildMu keeps host construction single-threaded while the QUIC reuse
// constructor records the manager that owns the listen socket.
var (
	quicBuildMu sync.Mutex
	quicBuilt   atomic.Pointer[quicreuse.ConnManager]
)

// libp2pListenOptions replaces the default transport list for an explicit
// libp2p port. WebRTC is omitted because it would read the non-QUIC packets
// on the QUIC socket, including the STUN binding responses for that port.
func libp2pListenOptions(listenPort int) []libp2p.Option {
	if listenPort <= 0 {
		return nil
	}
	return []libp2p.Option{
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(quic.NewTransport),
		libp2p.Transport(ws.New),
		libp2p.Transport(webtransport.New),
		libp2p.QUICReuse(newQUICConnManager),
	}
}

// newQUICConnManager is the libp2p QUIC reuse constructor. It keeps the
// resource-manager scope the default constructor installs, and records the
// manager so STUN can send from the same UDP socket as QUIC.
func newQUICConnManager(key quicgo.StatelessResetKey, tokenKey quicgo.TokenGeneratorKey, rcmgr network.ResourceManager, lifecycle fx.Lifecycle) (*quicreuse.ConnManager, error) {
	opts := []quicreuse.Option{
		quicreuse.EnableMetrics(prometheus.NewRegistry()),
	}
	if rcmgr != nil {
		opts = append(opts,
			quicreuse.ConnContext(func(ctx context.Context, clientInfo *quicgo.ClientInfo) (context.Context, error) {
				addr, err := quicreuse.ToQuicMultiaddr(clientInfo.RemoteAddr, quicgo.Version1)
				if err != nil {
					addr = nil
				}
				scope, err := rcmgr.OpenConnection(network.DirInbound, false, addr)
				if err != nil {
					return ctx, err
				}
				ctx = network.WithConnManagementScope(ctx, scope)
				context.AfterFunc(ctx, func() {
					scope.Done()
				})
				return ctx, nil
			}),
			quicreuse.VerifySourceAddress(rcmgr.VerifySourceAddress),
		)
	}
	cm, err := quicreuse.NewConnManager(key, tokenKey, opts...)
	if err != nil {
		return nil, err
	}
	if lifecycle != nil {
		lifecycle.Append(fx.StopHook(cm.Close))
	}
	quicBuilt.Store(cm)
	return cm, nil
}

func takeQUICConnManager() *quicreuse.ConnManager {
	cm := quicBuilt.Load()
	quicBuilt.Store(nil)
	return cm
}

// startLibp2pSTUN queries FSE_STUN_SERVERS from the QUIC socket at startup and
// on advertise.STUNInterval. Unit tests call ProbeSTUNNow instead of dialing
// the public servers during go test.
func startLibp2pSTUN(h host.Host, cm *quicreuse.ConnManager) func() {
	if testing.Testing() || h == nil || cm == nil || !NATEnabled() || len(advertise.STUNServers()) == 0 {
		if !testing.Testing() {
			advertise.SetNATStatus(advertise.NATStatus{Type: advertise.NATDisabled})
			structuredlog.Event("info", "nat.stun.disabled", "STUN is not querying the libp2p UDP port", map[string]any{
				"natType": advertise.NATDisabled,
			})
		}
		return func() {}
	}
	conn, laddr, err := shareQUICSocket(h, cm)
	if err != nil {
		structuredlog.Event("warn", "nat.stun.failed", "libp2p UDP socket is not available for STUN", map[string]any{
			"error": err.Error(),
		})
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	structuredlog.Event("info", "nat.stun.start", "STUN is querying the libp2p UDP socket", map[string]any{
		"listen":   laddr.String(),
		"interval": advertise.STUNInterval.String(),
		"servers":  advertise.STUNServers(),
	})
	go func() {
		defer close(done)
		defer conn.Close()
		go func() {
			<-ctx.Done()
			_ = conn.SetReadDeadline(time.Now())
		}()
		probe := func() {
			cycle, cancelCycle := context.WithTimeout(ctx, 15*time.Second)
			advertise.ObserveSTUN(cycle, conn)
			cancelCycle()
		}
		probe()
		ticker := time.NewTicker(advertise.STUNInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe()
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func shareQUICSocket(h host.Host, cm *quicreuse.ConnManager) (net.PacketConn, *net.UDPAddr, error) {
	laddr, err := quicListenUDP(h)
	if err != nil {
		return nil, nil, err
	}
	conn, err := cm.SharedNonQUICPacketConn("udp4", laddr)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", laddr.String(), err)
	}
	return conn, laddr, nil
}

func quicListenUDP(h host.Host) (*net.UDPAddr, error) {
	if h == nil || h.Network() == nil {
		return nil, fmt.Errorf("libp2p host is not listening")
	}
	var fallback *net.UDPAddr
	for _, addr := range h.Network().ListenAddresses() {
		udp, ok := udpFromMultiaddr(addr)
		if !ok {
			continue
		}
		if udp.IP.IsUnspecified() {
			return udp, nil
		}
		fallback = udp
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("quic listener not found")
}

func udpFromMultiaddr(addr ma.Multiaddr) (*net.UDPAddr, bool) {
	if addr == nil {
		return nil, false
	}
	if _, err := addr.ValueForProtocol(ma.P_QUIC_V1); err != nil {
		return nil, false
	}
	portRaw, err := addr.ValueForProtocol(ma.P_UDP)
	if err != nil {
		return nil, false
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return nil, false
	}
	ipRaw, err := addr.ValueForProtocol(ma.P_IP4)
	if err != nil {
		return nil, false
	}
	ip := net.ParseIP(ipRaw)
	if ip == nil || ip.To4() == nil {
		return nil, false
	}
	return &net.UDPAddr{IP: ip.To4(), Port: port}, true
}
