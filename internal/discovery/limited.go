package discovery

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/nat"
	"filesyncengine/internal/structuredlog"
)

// holePunchWait is how long a bulk stream waits for DCUtR to replace a
// limited relay with a direct connection. A failure is remembered so the
// next block does not wait again.
var holePunchWait = 8 * time.Second

const punchFailFor = 30 * time.Second

// ErrLimitedRelay means the only path to the peer is a limited relay circuit.
// Pairing and /v1/sync-journal may use that circuit. File transfer may not.
var ErrLimitedRelay error = limitedRelayError{}

type limitedRelayError struct{}

func (limitedRelayError) Error() string {
	return "only a limited relay is available; file transfer needs a direct path or an unlimited relay"
}

func (limitedRelayError) Timeout() bool   { return true }
func (limitedRelayError) Temporary() bool { return true }

// AllowsLimitedAPI reports paths that may open a stream on a limited relay.
// Everything else is bulk file transfer and needs a direct path or an
// unlimited relay.
func AllowsLimitedAPI(path string) bool {
	switch path {
	case "/v1/sync-journal", "/v1/pair-code/claim", "/v1/pair-code/record", "/v1/link-profile", "/v1/config-sync/merge":
		return true
	default:
		return false
	}
}

func apiStreamContext(ctx context.Context, path string) context.Context {
	if AllowsLimitedAPI(path) {
		return network.WithAllowLimitedConn(ctx, "fse-control")
	}
	return ctx
}

func pairingStreamContext(ctx context.Context) context.Context {
	return network.WithAllowLimitedConn(ctx, "fse-pair")
}

type punchCacheEntry struct {
	at          time.Time
	noUnlimited bool
}

var (
	punchMu    sync.Mutex
	punchCache = map[peer.ID]punchCacheEntry{}
)

func resetPunchCache() {
	punchMu.Lock()
	punchCache = map[peer.ID]punchCacheEntry{}
	punchMu.Unlock()
}

func punchCached(id peer.ID) (waited bool, noUnlimited bool) {
	punchMu.Lock()
	defer punchMu.Unlock()
	entry, ok := punchCache[id]
	if !ok || time.Since(entry.at) > punchFailFor {
		return false, false
	}
	return true, entry.noUnlimited
}

func rememberPunch(id peer.ID, noUnlimited bool) {
	punchMu.Lock()
	defer punchMu.Unlock()
	if punchCache == nil {
		punchCache = map[peer.ID]punchCacheEntry{}
	}
	entry := punchCache[id]
	if entry.at.IsZero() || time.Since(entry.at) > punchFailFor {
		entry = punchCacheEntry{at: time.Now()}
	}
	if noUnlimited {
		entry.noUnlimited = true
	}
	punchCache[id] = entry
}

func clearPunch(id peer.ID) {
	punchMu.Lock()
	delete(punchCache, id)
	punchMu.Unlock()
}

func limitedStreamFailure(err error) bool {
	return errors.Is(err, network.ErrLimitedConn) || errors.Is(err, context.DeadlineExceeded)
}

func unlimitedPeerConn(h host.Host, id peer.ID) network.Conn {
	if h == nil {
		return nil
	}
	for _, conn := range h.Network().ConnsToPeer(id) {
		if conn != nil && !conn.Stat().Limited {
			return conn
		}
	}
	return nil
}

func connPath(conn network.Conn) (nat.Path, string) {
	if conn == nil {
		return "", ""
	}
	addr := ""
	if conn.RemoteMultiaddr() != nil {
		addr = conn.RemoteMultiaddr().String()
	}
	if conn.Stat().Limited {
		return nat.PathLimitedRelay, addr
	}
	path := nat.Classify(addr)
	if path == "" {
		path = nat.PathDirect
	}
	return path, addr
}

// openBulk opens a file-transfer stream. A limited relay is used only as the
// DCUtR coordination path: this waits for a direct connection, then for an
// unlimited same-identity or configured relay. It does not send file bytes
// over the limited circuit.
func openBulk(ctx context.Context, h host.Host, id peer.ID, open func(context.Context) (*http.Response, error)) (*http.Response, error) {
	if unlimitedPeerConn(h, id) != nil {
		clearPunch(id)
		return open(ctx)
	}
	waited, noUnlimited := punchCached(id)
	if !waited {
		waitCtx := network.WithDialPeerTimeout(ctx, holePunchWait)
		resp, err := open(waitCtx)
		if err == nil {
			clearPunch(id)
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !limitedStreamFailure(err) && !errors.Is(err, ErrLimitedRelay) {
			return nil, err
		}
		rememberPunch(id, false)
		if limitedStreamFailure(err) {
			structuredlog.Event("warn", "libp2p.dcutr.failed", "DCUtR did not open a direct path", map[string]any{
				"peer":  id.String(),
				"error": "timed out waiting for a direct path",
			})
		}
	}
	if noUnlimited {
		nat.DefaultPaths.ObserveLimited(id.String(), "")
		return nil, ErrLimitedRelay
	}
	if err := connectUnlimitedRelay(ctx, h, id); err != nil {
		rememberPunch(id, true)
		nat.DefaultPaths.ObserveLimited(id.String(), "")
		return nil, ErrLimitedRelay
	}
	clearPunch(id)
	return open(ctx)
}

func connectUnlimitedRelay(ctx context.Context, h host.Host, target peer.ID) error {
	if h == nil {
		return ErrLimitedRelay
	}
	if unlimitedPeerConn(h, target) != nil {
		return nil
	}
	// A live limited circuit makes Connect return that circuit instead of
	// dialing another relay. Keep the previous addresses so signaling can be
	// restored when every unlimited candidate fails.
	prev := append([]ma.Multiaddr(nil), h.Peerstore().Addrs(target)...)
	tried := false
	for _, raw := range UnlimitedRelayAddrs() {
		hop, circuit, err := relayCircuit(raw, target)
		if err != nil {
			continue
		}
		tried = true
		dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		hopErr := h.Connect(dialCtx, hop)
		if hopErr != nil {
			cancel()
			structuredlog.Event("warn", "libp2p.relay.limited", "skipping a limited relay for file transfer", map[string]any{
				"peer":  target.String(),
				"relay": hop.ID.String(),
				"error": hopErr.Error(),
			})
			continue
		}
		for _, conn := range h.Network().ConnsToPeer(target) {
			if conn != nil && conn.Stat().Limited {
				_ = conn.Close()
			}
		}
		h.Peerstore().SetAddrs(target, []ma.Multiaddr{circuit}, peerstore.TempAddrTTL)
		err = h.Connect(dialCtx, peer.AddrInfo{ID: target, Addrs: []ma.Multiaddr{circuit}})
		cancel()
		if conn := unlimitedPeerConn(h, target); conn != nil {
			addr := ""
			if conn.RemoteMultiaddr() != nil {
				addr = conn.RemoteMultiaddr().String()
			}
			h.Peerstore().AddAddrs(target, prev, peerstore.TempAddrTTL)
			structuredlog.Event("info", "libp2p.relay.unlimited", "using an unlimited relay for file transfer", map[string]any{
				"peer":  target.String(),
				"relay": hop.ID.String(),
				"addr":  addr,
			})
			return nil
		}
		fields := map[string]any{"peer": target.String(), "relay": hop.ID.String()}
		if err != nil {
			fields["error"] = err.Error()
		} else {
			fields["error"] = "circuit is still limited"
		}
		structuredlog.Event("warn", "libp2p.relay.limited", "skipping a limited relay for file transfer", fields)
	}
	if len(prev) > 0 && unlimitedPeerConn(h, target) == nil && len(h.Network().ConnsToPeer(target)) == 0 {
		h.Peerstore().SetAddrs(target, prev, peerstore.TempAddrTTL)
		restoreCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		_ = h.Connect(restoreCtx, peer.AddrInfo{ID: target, Addrs: prev})
		cancel()
	} else if len(prev) > 0 {
		h.Peerstore().AddAddrs(target, prev, peerstore.TempAddrTTL)
	}
	if !tried {
		return ErrLimitedRelay
	}
	return ErrLimitedRelay
}

func relayCircuit(raw string, target peer.ID) (peer.AddrInfo, ma.Multiaddr, error) {
	raw = strings.TrimSpace(raw)
	if cut := strings.Index(raw, "/p2p-circuit"); cut >= 0 {
		raw = raw[:cut]
	}
	addr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return peer.AddrInfo{}, nil, err
	}
	info, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil || info == nil || info.ID == "" || info.ID == target || len(info.Addrs) == 0 {
		return peer.AddrInfo{}, nil, errors.New("relay address is not usable for an unlimited circuit")
	}
	circuit, err := ma.NewMultiaddr(info.Addrs[0].String() + "/p2p/" + info.ID.String() + "/p2p-circuit/p2p/" + target.String())
	if err != nil {
		return peer.AddrInfo{}, nil, err
	}
	return *info, circuit, nil
}

func noteResponsePath(live *nat.Path, remote *string, stream network.Stream) {
	if stream == nil || stream.Conn() == nil {
		return
	}
	path, addr := connPath(stream.Conn())
	if live != nil && path != "" {
		*live = path
	}
	if remote != nil && addr != "" {
		*remote = addr
	}
}

func limitedRelayResponse(resp *http.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("X-FSE-Limited-Relay") == "1"
}
