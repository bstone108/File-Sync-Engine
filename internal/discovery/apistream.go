package discovery

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"

	"filesyncengine/internal/nat"
)

const (
	// APIProtocol carries one HTTP request over a libp2p stream.
	// File sync and pairing use it when the advertised API port is not forwarded.
	APIProtocol = "/fse/api/1.0.0"
	// StreamBaseURL is the synthetic origin link.Dial passes to HTTP callbacks
	// that are being served through APIProtocol. It is not a routable host.
	StreamBaseURL = "http://fse-stream.invalid"
)

var (
	apiMu      sync.Mutex
	apiHandler http.Handler
	apiHost    host.Host
)

// SetAPIHandler is the process HTTP API served on APIProtocol.
// The daemon installs it once the route table is registered.
func SetAPIHandler(handler http.Handler) {
	apiMu.Lock()
	apiHandler = handler
	h := apiHost
	apiMu.Unlock()
	if h == nil {
		h = sharedHost()
	}
	if h != nil && handler != nil {
		registerAPIStream(h, handler)
	}
}

// ServeAPI registers the current API handler on h without changing the host
// DialStream uses. A test serves the sender and dials from a different host.
func ServeAPI(h host.Host) {
	if h == nil {
		return
	}
	apiMu.Lock()
	handler := apiHandler
	apiMu.Unlock()
	if handler != nil {
		registerAPIStream(h, handler)
	}
}

// SetAPIHost pins the libp2p host used to dial API streams.
// Tests install a private host. The production router fills it when empty.
func SetAPIHost(h host.Host) {
	apiMu.Lock()
	apiHost = h
	handler := apiHandler
	apiMu.Unlock()
	if h != nil && handler != nil {
		registerAPIStream(h, handler)
	}
}

func noteRouterHost(h host.Host) {
	if h == nil {
		return
	}
	apiMu.Lock()
	if apiHost == nil {
		apiHost = h
	}
	handler := apiHandler
	apiMu.Unlock()
	if handler != nil {
		registerAPIStream(h, handler)
	}
}

func currentAPIHost() host.Host {
	apiMu.Lock()
	h := apiHost
	apiMu.Unlock()
	if h != nil {
		return h
	}
	return sharedHost()
}

func sharedHost() host.Host {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedRouter == nil {
		return nil
	}
	return sharedRouter.host
}

func registerAPIStream(h host.Host, handler http.Handler) {
	h.SetStreamHandler(protocol.ID(APIProtocol), func(stream network.Stream) {
		serveAPIStream(stream, rejectLimitedBulk(handler))
	})
}

func rejectLimitedBulk(next http.Handler) http.Handler {
	if next == nil {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, ok := streamFromContext(r.Context())
		if ok && stream != nil && stream.Conn() != nil && stream.Conn().Stat().Limited && !AllowsLimitedAPI(r.URL.Path) {
			w.Header().Set("X-FSE-Limited-Relay", "1")
			http.Error(w, "limited relay cannot carry file data", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveAPIStream(stream network.Stream, handler http.Handler) {
	if stream == nil || handler == nil {
		if stream != nil {
			_ = stream.Reset()
		}
		return
	}
	defer stream.Close()
	ln := newOnceListener(streamConn{Stream: stream})
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if found, ok := streamFromConn(c); ok {
				return context.WithValue(ctx, streamContextKey, found)
			}
			return ctx
		},
	}
	// Serve accepts the one connection, then blocks until that connection
	// closes. Returning here releases the stream instead of leaking a handler
	// goroutine per block.
	_ = srv.Serve(ln)
}

// StreamSession is an HTTP client whose requests travel on APIProtocol.
// Remote is the multiaddr of the live connection, so the caller can tell a
// relayed stream from a direct or hole-punched one.
type StreamSession struct {
	Client *http.Client
	Remote string
	Path   nat.Path
	live   *nat.Path
	remote *string
}

// DialStream connects to peerID using only the supplied multiaddrs and returns
// a client that does not write a request until that peer's identity is authenticated.
func DialStream(ctx context.Context, peerID string, addrs []string) (StreamSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	h := currentAPIHost()
	if h == nil {
		return StreamSession{}, errNoAPIHost
	}
	pid, err := peer.Decode(strings.TrimSpace(peerID))
	if err != nil {
		return StreamSession{}, err
	}
	parsed := make([]ma.Multiaddr, 0, len(addrs))
	for _, raw := range addrs {
		addr, addrErr := ma.NewMultiaddr(strings.TrimSpace(raw))
		if addrErr != nil {
			continue
		}
		parsed = append(parsed, addr)
	}
	if len(parsed) == 0 {
		return StreamSession{}, errNoAPIHost
	}
	direct, circuits := partitionStreamAddrs(parsed)
	// A public relay hop can sit there until the claim budget is gone. Dial a
	// direct address first, and only then spend time opening a circuit.
	if len(direct) > 0 {
		if err := h.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: direct}); err == nil {
			return newStreamSession(h, pid)
		} else if ctx.Err() != nil || len(circuits) == 0 {
			return StreamSession{}, err
		}
	}
	if len(circuits) == 0 {
		return StreamSession{}, errNoAPIHost
	}
	connectRelayHops(ctx, h, circuits)
	if err := h.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: circuits}); err != nil {
		return StreamSession{}, err
	}
	return newStreamSession(h, pid)
}

func partitionStreamAddrs(addrs []ma.Multiaddr) (direct, circuits []ma.Multiaddr) {
	for _, addr := range addrs {
		if addr == nil {
			continue
		}
		if strings.Contains(addr.String(), "p2p-circuit") {
			circuits = append(circuits, addr)
			continue
		}
		direct = append(direct, addr)
	}
	return direct, circuits
}

func newStreamSession(h host.Host, pid peer.ID) (StreamSession, error) {
	if !peerIDMatches(h, pid) {
		return StreamSession{}, errPeerMismatch
	}
	path, remote := peerSessionPath(h, pid)
	live := path
	session := StreamSession{
		Remote: remote,
		Path:   path,
		live:   &live,
		remote: &remote,
	}
	session.Client = &http.Client{Transport: streamRoundTripper{host: h, peer: pid, live: session.live, remote: session.remote}}
	return session, nil
}

func peerSessionPath(h host.Host, pid peer.ID) (nat.Path, string) {
	if conn := unlimitedPeerConn(h, pid); conn != nil {
		return connPath(conn)
	}
	conns := h.Network().ConnsToPeer(pid)
	for _, conn := range conns {
		if conn != nil {
			return connPath(conn)
		}
	}
	return nat.PathRelay, ""
}

// CurrentPath is the path of the stream that carried the last request.
func (s StreamSession) CurrentPath() nat.Path {
	if s.live != nil && *s.live != "" {
		return *s.live
	}
	return s.Path
}

// CurrentRemote is the multiaddr of the stream that carried the last request.
func (s StreamSession) CurrentRemote() string {
	if s.remote != nil && *s.remote != "" {
		return *s.remote
	}
	return s.Remote
}

// connectRelayHops opens relay hops, but only for a few seconds altogether.
// One unreachable public relay must not consume a claim or sync budget
// before Connect tries the circuit itself.
func connectRelayHops(ctx context.Context, h host.Host, addrs []ma.Multiaddr) {
	if ctx == nil || len(addrs) == 0 {
		return
	}
	budget, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	for _, addr := range addrs {
		if budget.Err() != nil {
			return
		}
		connectRelayHop(budget, h, addr)
	}
}

// connectRelayHop dials the relay named by a circuit address before the
// target. A stored reservation is useless until this hop is opened.
func connectRelayHop(ctx context.Context, h host.Host, addr ma.Multiaddr) {
	raw := addr.String()
	cut := strings.Index(raw, "/p2p-circuit")
	if cut <= 0 {
		return
	}
	head, err := ma.NewMultiaddr(raw[:cut])
	if err != nil {
		return
	}
	info, err := peer.AddrInfoFromP2pAddr(head)
	if err != nil || info.ID == "" {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_ = h.Connect(dialCtx, *info)
}

var (
	errNoAPIHost    = streamError("libp2p API host is not running")
	errPeerMismatch = streamError("peer id mismatch")
)

func peerIDMatches(h host.Host, pid peer.ID) bool {
	if h == nil || pid == "" {
		return false
	}
	for _, conn := range h.Network().ConnsToPeer(pid) {
		if conn != nil && conn.RemotePeer() == pid {
			return true
		}
	}
	return false
}

type streamError string

func (e streamError) Error() string { return string(e) }

type streamContextKeyType struct{}

var streamContextKey = streamContextKeyType{}

func streamFromConn(c net.Conn) (network.Stream, bool) {
	for c != nil {
		switch conn := c.(type) {
		case *trackConn:
			c = conn.Conn
		case streamConn:
			return conn.Stream, conn.Stream != nil
		default:
			return nil, false
		}
	}
	return nil, false
}

func streamFromContext(ctx context.Context) (network.Stream, bool) {
	if ctx == nil {
		return nil, false
	}
	stream, ok := ctx.Value(streamContextKey).(network.Stream)
	return stream, ok && stream != nil
}

type streamRoundTripper struct {
	host   host.Host
	peer   peer.ID
	live   *nat.Path
	remote *string
}

func (t streamRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, streamError("missing request")
	}
	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	path := ""
	if req.URL != nil {
		path = req.URL.Path
	}
	if AllowsLimitedAPI(path) {
		return t.exchange(apiStreamContext(ctx, path), req)
	}
	return openBulk(ctx, t.host, t.peer, func(ctx context.Context) (*http.Response, error) {
		return t.exchange(ctx, req)
	})
}

func (t streamRoundTripper) exchange(ctx context.Context, req *http.Request) (*http.Response, error) {
	stream, err := t.host.NewStream(ctx, t.peer, protocol.ID(APIProtocol))
	if err != nil {
		return nil, err
	}
	noteResponsePath(t.live, t.remote, stream)
	_ = stream.SetDeadline(time.Now().Add(30 * time.Second))
	outbound := req.Clone(ctx)
	if outbound == nil {
		outbound = req
	}
	// One request per stream. The handler returns after this response, so a
	// keep-alive read would block until the peer closed the stream.
	outbound.Close = true
	if stream.Conn() == nil || stream.Conn().RemotePeer() != t.peer {
		_ = stream.Reset()
		return nil, errPeerMismatch
	}
	if err := outbound.Write(stream); err != nil {
		_ = stream.Reset()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(stream), req)
	if err != nil {
		_ = stream.Reset()
		return nil, err
	}
	if limitedRelayResponse(resp) {
		_ = resp.Body.Close()
		nat.DefaultPaths.ObserveLimited(t.peer.String(), "")
		return nil, ErrLimitedRelay
	}
	resp.Body = &streamBody{ReadCloser: resp.Body, stream: stream}
	return resp, nil
}

type streamBody struct {
	io.ReadCloser
	stream network.Stream
}

func (b *streamBody) Read(p []byte) (int, error) {
	if b.stream != nil {
		_ = b.stream.SetReadDeadline(time.Now().Add(30 * time.Second))
	}
	return b.ReadCloser.Read(p)
}

func (b *streamBody) Close() error {
	err := b.ReadCloser.Close()
	if b.stream != nil {
		_ = b.stream.Close()
	}
	return err
}

type streamConn struct {
	network.Stream
}

func (c streamConn) LocalAddr() net.Addr  { return streamAddr{c.Conn().LocalMultiaddr()} }
func (c streamConn) RemoteAddr() net.Addr { return streamAddr{c.Conn().RemoteMultiaddr()} }

type streamAddr struct{ ma.Multiaddr }

func (a streamAddr) Network() string { return "libp2p" }
func (a streamAddr) String() string {
	if a.Multiaddr == nil {
		return "libp2p"
	}
	return a.Multiaddr.String()
}

type onceListener struct {
	conn   net.Conn
	done   chan struct{}
	finish sync.Once
	used   bool
	mu     sync.Mutex
}

func newOnceListener(conn net.Conn) *onceListener {
	return &onceListener{conn: conn, done: make(chan struct{})}
}

func (l *onceListener) signal() {
	l.finish.Do(func() { close(l.done) })
}

func (l *onceListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.used {
		l.used = true
		conn := &trackConn{Conn: l.conn, signal: l.signal}
		l.mu.Unlock()
		return conn, nil
	}
	l.mu.Unlock()
	<-l.done
	return nil, net.ErrClosed
}

func (l *onceListener) Close() error {
	l.signal()
	return nil
}

// trackConn tells the listener the HTTP exchange finished so Serve can return.
type trackConn struct {
	net.Conn
	signal func()
	once   sync.Once
}

func (c *trackConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.signal)
	return err
}

func (l *onceListener) Addr() net.Addr {
	if l.conn != nil {
		return l.conn.LocalAddr()
	}
	return streamAddr{}
}
