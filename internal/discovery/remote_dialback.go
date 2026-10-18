package discovery

import (
	"context"
	"errors"
	"strings"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"filesyncengine/internal/structuredlog"
)

// runRemoteDialback keeps asking a connected peer to dial an announced
// address until one of them succeeds. The host's own AutoNAT tracker only
// probes local public listen addresses, so a published DNS name never
// confirms reachability on its own.
func (r *Libp2pDHTRouter) runRemoteDialback() {
	if r == nil || r.closed == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.closed:
			return
		case <-ticker.C:
			if r.remotePublic.Load() {
				return
			}
			r.scheduleRemoteDialback()
		}
	}
}

// scheduleRemoteDialback starts one dial-back check. A self-dial is never
// scheduled: the server is a different peer, and its dialer is a separate host
// so an existing LAN connection cannot be reused as proof.
func (r *Libp2pDHTRouter) scheduleRemoteDialback() {
	if r == nil || r.remotePublic.Load() || !NATEnabled() || r.closed == nil {
		return
	}
	select {
	case <-r.closed:
		return
	default:
	}
	if !r.dialbackRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.dialbackRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r.tryRemoteDialback(ctx, nil)
	}()
}

func (r *Libp2pDHTRouter) shouldProbeRemoteDialback() bool {
	if r == nil || r.host == nil || r.remotePublic.Load() || !NATEnabled() {
		return false
	}
	return r.hasRemoteAutoNATPeer() && len(publicLibp2pProbeAddrs(r.host.Addrs())) > 0
}

// tryRemoteDialback asks one connected peer to dial addrs. Empty addrs uses
// the addresses this host is announcing. It reports whether that dial-back
// promoted the node.
func (r *Libp2pDHTRouter) tryRemoteDialback(ctx context.Context, addrs []ma.Multiaddr) bool {
	if r == nil || r.host == nil || !NATEnabled() {
		return false
	}
	if r.remotePublic.Load() {
		return true
	}
	if r.closed != nil {
		select {
		case <-r.closed:
			return false
		default:
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !r.hasRemoteAutoNATPeer() {
		return false
	}
	if len(addrs) == 0 {
		addrs = publicLibp2pProbeAddrs(r.host.Addrs())
	} else {
		addrs = publicLibp2pProbeAddrs(addrs)
	}
	if len(addrs) == 0 {
		return false
	}
	an := r.remoteAutoNAT()
	if an == nil {
		return false
	}
	reqs := make([]autonatv2.Request, 0, len(addrs))
	for _, addr := range addrs {
		reqs = append(reqs, autonatv2.Request{Addr: addr, SendDialData: true})
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var res autonatv2.Result
	var err error
	for {
		res, err = an.GetReachability(probeCtx, reqs)
		if !errors.Is(err, autonatv2.ErrNoPeers) {
			break
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-probeCtx.Done():
			timer.Stop()
			err = probeCtx.Err()
		case <-timer.C:
		}
		if probeCtx.Err() != nil {
			if err == nil {
				err = probeCtx.Err()
			}
			break
		}
	}
	if err != nil || res.Reachability != network.ReachabilityPublic {
		if err != nil && !errors.Is(err, autonatv2.ErrNoPeers) && !errors.Is(err, autonatv2.ErrPrivateAddrs) && !res.AllAddrsRefused {
			structuredlog.Event("info", "libp2p.autonat.probe", "remote AutoNAT dial-back did not confirm this node", map[string]any{
				"error": err.Error(),
			})
		}
		return false
	}
	r.notePublicReachability("autonat-v2")
	return r.remotePublic.Load()
}

func (r *Libp2pDHTRouter) hasRemoteAutoNATPeer() bool {
	if r == nil || r.host == nil {
		return false
	}
	for _, id := range r.host.Network().Peers() {
		if id == "" || id == r.host.ID() {
			continue
		}
		supported, err := r.host.Peerstore().SupportsProtocols(id, autonatv2.DialProtocol)
		if err == nil && len(supported) > 0 {
			return true
		}
	}
	return false
}

// remoteAutoNAT is the AutoNAT v2 client whose dial-back handler matches the
// nonce from our request. The dialer host is not this node, so the check
// cannot succeed by hairpinning through a socket this process already holds.
func (r *Libp2pDHTRouter) remoteAutoNAT() *autonatv2.AutoNAT {
	if r == nil || r.host == nil {
		return nil
	}
	r.mu.Lock()
	existing := r.autonat
	r.mu.Unlock()
	if existing != nil {
		return existing
	}
	dialer, err := libp2p.New(libp2p.NoListenAddrs)
	if err != nil {
		structuredlog.Event("warn", "libp2p.autonat.probe", "remote dial-back dialer did not start", map[string]any{"error": err.Error()})
		return nil
	}
	an, err := autonatv2.New(dialer)
	if err != nil {
		_ = dialer.Close()
		structuredlog.Event("warn", "libp2p.autonat.probe", "remote dial-back client did not start", map[string]any{"error": err.Error()})
		return nil
	}
	if err := an.Start(r.host); err != nil {
		an.Close()
		structuredlog.Event("warn", "libp2p.autonat.probe", "remote dial-back client did not start", map[string]any{"error": err.Error()})
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed != nil {
		select {
		case <-r.closed:
			an.Close()
			return nil
		default:
		}
	}
	if r.autonat != nil {
		an.Close()
		return r.autonat
	}
	r.autonat = an
	return an
}

// publicLibp2pProbeAddrs keeps announced TCP addresses another node can dial.
// DNS names come first so a published hostname is tried before a mapped IP.
// Private addresses, relay circuits, and the HTTPS API port are not libp2p dial-backs.
func publicLibp2pProbeAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	var dnsTCP, ipTCP []ma.Multiaddr
	for _, addr := range addrs {
		if addr == nil || !manet.IsPublicAddr(addr) {
			continue
		}
		raw := addr.String()
		if strings.Contains(raw, "p2p-circuit") || strings.Contains(raw, "/http") || strings.Contains(raw, "/quic") || strings.Contains(raw, "/ws") {
			continue
		}
		if !strings.Contains(raw, "/tcp/") {
			continue
		}
		if strings.Contains(raw, "/dns") {
			dnsTCP = append(dnsTCP, addr)
			continue
		}
		ipTCP = append(ipTCP, addr)
	}
	return append(dnsTCP, ipTCP...)
}
