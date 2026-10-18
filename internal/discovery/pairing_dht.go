package discovery

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	corediscovery "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"filesyncengine/internal/nat"
	"filesyncengine/internal/structuredlog"
	ma "github.com/multiformats/go-multiaddr"
)

// pairingAdvertiseTimeout bounds the public DHT provider walk. Routing
// discovery otherwise searches for up to a minute, and a completed-enough
// walk still returns context.DeadlineExceeded after storing provider records.
const pairingAdvertiseTimeout = 25 * time.Second

// pairingLookupTimeout bounds a redeemer's public DHT walk. The local
// directory answers same-LAN pairing on its own, and a second redeem must
// be rejected without waiting out a full provider search.
const pairingLookupTimeout = 20 * time.Second

// pairingStreamTimeout keeps a dead libp2p dial from consuming the lookup.
// The advertised HTTPS API is tried next.
const pairingStreamTimeout = 4 * time.Second

const (
	// PairingProtocol serves the MAC-bound pairing record after DHT provider
	// discovery. The public DHT validator does not store arbitrary values, so
	// the record stays on the publishing peer and is fetched over this stream.
	PairingProtocol = "/fse/pair-code/1.0.0"
	maxPairingBytes = 64 * 1024
)

func (r *Libp2pDHTRouter) PeerID() string {
	if r == nil || r.host == nil {
		return ""
	}
	return r.host.ID().String()
}

// PublishPairing advertises the code namespace on the existing DHT and serves
// the signed record to peers that look the namespace up.
func (r *Libp2pDHTRouter) PublishPairing(ctx context.Context, namespace string, payload []byte) error {
	if err := r.storePairing(ctx, namespace, payload); err != nil {
		return err
	}
	// Register before the walk so a relay reservation that lands during
	// bootstrap refreshes this namespace with the circuit address.
	r.mu.Lock()
	r.advertise[namespace] = struct{}{}
	r.mu.Unlock()
	if err := r.ensureBootstrapped(ctx); err != nil {
		structuredlog.Event("error", "pairing.publish.failed", "pairing DHT bootstrap failed", map[string]any{"namespace": namespace, "error": err.Error()})
		return err
	}
	// A connected peer that is still a DHT client cannot store a provider
	// record. The record stays on this node and that peer fetches it. Server
	// mode still waits for a remote dial-back.
	if r.routingTableSize() == 0 && r.connectedBootstrapIsClient() {
		structuredlog.Event("info", "pairing.publish.ok", "pairing code is served to a connected peer", map[string]any{"namespace": namespace, "routingPeers": 0})
		return nil
	}
	if err := r.advertisePairing(ctx, namespace); err != nil {
		structuredlog.Event("error", "pairing.publish.failed", "pairing code was not advertised on the DHT", map[string]any{"namespace": namespace, "error": err.Error()})
		return err
	}
	structuredlog.Event("info", "pairing.publish.ok", "pairing code advertised on the DHT", map[string]any{"namespace": namespace, "routingPeers": r.routingTableSize()})
	return nil
}

// advertisePairing publishes the namespace and treats a DHT walk that stored
// provider records before its deadline as success. An empty routing table
// still fails, because nothing was reachable to store the record.
func (r *Libp2pDHTRouter) advertisePairing(ctx context.Context, namespace string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	advCtx, cancel := context.WithTimeout(ctx, pairingAdvertiseTimeout)
	defer cancel()
	_, err := r.discovery.Advertise(advCtx, namespace)
	return acceptPartialAdvertise(err, r.routingTableSize(), ctx)
}

func (r *Libp2pDHTRouter) routingTableSize() int {
	if r == nil || r.dht == nil || r.dht.RoutingTable() == nil {
		return 0
	}
	return r.dht.RoutingTable().Size()
}

// acceptedBootstrap rejects a finished dial that left the routing table empty.
// kad-dht reports that as success, and a later advertise then fails with
// "failed to find any peer in table" without trying the peer again.
func acceptedBootstrap(err error, peers int, peerList []string) error {
	if err != nil {
		return err
	}
	if peers > 0 {
		return nil
	}
	if len(peerList) == 0 {
		return errors.New("pairing DHT bootstrap connected no peers")
	}
	return fmt.Errorf("pairing DHT bootstrap connected no peers (%s)", strings.Join(peerList, ", "))
}

func acceptPartialAdvertise(err error, peers int, parent context.Context) error {
	if err == nil {
		return nil
	}
	if parent != nil && parent.Err() != nil {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) && peers > 0 {
		structuredlog.Event("warn", "pairing.publish.partial", "pairing DHT advertise reached its deadline after contacting peers", map[string]any{"routingPeers": peers})
		return nil
	}
	return err
}

// LookupPairing finds DHT providers for the namespace and fetches each record.
// The caller does not have to be a DHT server; a client behind NAT can query
// peers it has dialed and then open the stream or the advertised API URL.
// Callers must verify the code MAC and identity signature before trusting one.
func (r *Libp2pDHTRouter) LookupPairing(ctx context.Context, namespace string) ([][]byte, error) {
	if r == nil || r.discovery == nil || r.host == nil {
		return nil, fmt.Errorf("libp2p DHT router is not initialized")
	}
	if namespace == "" {
		return nil, fmt.Errorf("pairing namespace is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, pairingLookupTimeout)
	defer cancel()
	if err := r.ensureBootstrapped(lookupCtx); err != nil && !r.hasConnectedBootstrapPeer() {
		structuredlog.Event("error", "pairing.lookup.failed", "pairing DHT bootstrap failed", map[string]any{"namespace": namespace, "error": err.Error()})
		if lookupCtx.Err() != nil && ctx.Err() == nil {
			return nil, nil
		}
		return nil, err
	}
	if direct := r.fetchConnectedPairing(lookupCtx, namespace); len(direct) > 0 {
		return direct, nil
	}
	peerCh, err := r.discovery.FindPeers(lookupCtx, namespace, corediscovery.Limit(16))
	if err != nil {
		structuredlog.Event("error", "pairing.lookup.failed", "pairing DHT provider lookup failed", map[string]any{"namespace": namespace, "error": err.Error()})
		if lookupCtx.Err() != nil && ctx.Err() == nil {
			return nil, nil
		}
		return nil, err
	}
	payloads := [][]byte{}
	var lastFetch error
	for {
		if lookupCtx.Err() != nil {
			break
		}
		var info peer.AddrInfo
		var open bool
		select {
		case <-lookupCtx.Done():
			open = false
		case info, open = <-peerCh:
		}
		if !open {
			break
		}
		if info.ID == "" || info.ID == r.host.ID() {
			continue
		}
		payload, fetchErr := r.FetchPairing(lookupCtx, info, namespace)
		if fetchErr != nil || len(payload) == 0 {
			if fetchErr != nil {
				lastFetch = fetchErr
				structuredlog.Event("warn", "pairing.lookup.fetch_failed", "pairing record fetch failed", map[string]any{
					"namespace": namespace,
					"peer":      info.ID.String(),
					"error":     fetchErr.Error(),
				})
			}
			continue
		}
		payloads = append(payloads, payload)
		if len(payloads) >= 8 {
			break
		}
	}
	if len(payloads) == 0 {
		fields := map[string]any{"namespace": namespace, "routingPeers": r.routingTableSize()}
		if lastFetch != nil {
			fields["error"] = lastFetch.Error()
		}
		structuredlog.Event("warn", "pairing.lookup.empty", "pairing code lookup found no record", fields)
		if lastFetch != nil && lookupCtx.Err() == nil {
			return nil, fmt.Errorf("pairing code lookup failed: %w", lastFetch)
		}
	}
	return payloads, nil
}

// UnpublishPairing stops serving a pairing record. Provider records already
// on the DHT expire on their own; an empty fetch fails closed.
func (r *Libp2pDHTRouter) UnpublishPairing(_ context.Context, namespace string) error {
	if r == nil {
		return fmt.Errorf("libp2p DHT router is not initialized")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.payloads, namespace)
	delete(r.advertise, namespace)
	return nil
}

// FetchPairing dials one discovered peer and reads its pairing record.
// When the libp2p stream cannot be opened, it fetches the same sealed record
// from the HTTPS API URL carried in the provider addresses.
func (r *Libp2pDHTRouter) FetchPairing(ctx context.Context, info peer.AddrInfo, namespace string) ([]byte, error) {
	if r == nil || r.host == nil {
		return nil, fmt.Errorf("libp2p DHT router is not initialized")
	}
	payload, err := r.fetchPairingStream(ctx, info, namespace)
	if err == nil {
		return payload, nil
	}
	httpPayload, httpErr := fetchPairingHTTP(ctx, info.Addrs, namespace)
	if httpErr == nil && len(httpPayload) > 0 {
		return httpPayload, nil
	}
	return nil, err
}

func (r *Libp2pDHTRouter) fetchPairingStream(ctx context.Context, info peer.AddrInfo, namespace string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, pairingStreamTimeout)
	defer cancel()
	dialInfo := info
	dialInfo.Addrs = dialablePairingAddrs(info.Addrs)
	if len(dialInfo.Addrs) == 0 {
		return nil, fmt.Errorf("pairing provider has no dialable libp2p address")
	}
	if _, err := r.connectPaths(dialCtx, dialInfo); err != nil {
		return nil, err
	}
	stream, err := r.host.NewStream(pairingStreamContext(dialCtx), info.ID, protocol.ID(PairingProtocol))
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(pairingStreamTimeout))
	if _, err := io.WriteString(stream, namespace+"\n"); err != nil {
		return nil, err
	}
	var lengthBuf [4]byte
	if _, err := io.ReadFull(stream, lengthBuf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf[:])
	if length == 0 {
		return nil, nil
	}
	if length > maxPairingBytes {
		return nil, fmt.Errorf("pairing record exceeds %d bytes", maxPairingBytes)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(stream, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// connectPaths dials direct, LAN, hole-punch, and relay addresses together.
// A relay success is replaced when a better path connects in the same attempt.
func (r *Libp2pDHTRouter) connectPaths(ctx context.Context, info peer.AddrInfo) (nat.Path, error) {
	candidates := make([]nat.Candidate, 0, len(info.Addrs))
	for _, addr := range info.Addrs {
		if addr == nil {
			continue
		}
		raw := addr.String()
		candidates = append(candidates, nat.Candidate{Address: raw, Path: nat.Classify(raw)})
	}
	used, path, err := nat.Race(ctx, candidates, func(ctx context.Context, address string) error {
		maddr, err := ma.NewMultiaddr(address)
		if err != nil {
			return err
		}
		return r.host.Connect(ctx, peer.AddrInfo{ID: info.ID, Addrs: []ma.Multiaddr{maddr}})
	}, time.Second)
	if err != nil {
		return "", err
	}
	nat.DefaultPaths.Observe(info.ID.String(), used, path)
	return path, nil
}

func (r *Libp2pDHTRouter) storePairing(ctx context.Context, namespace string, payload []byte) error {
	if r == nil || r.host == nil {
		return fmt.Errorf("libp2p DHT router is not initialized")
	}
	if namespace == "" || len(payload) == 0 {
		return fmt.Errorf("pairing record is incomplete")
	}
	if len(payload) > maxPairingBytes {
		return fmt.Errorf("pairing record exceeds %d bytes", maxPairingBytes)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if r.payloads == nil {
		r.payloads = map[string][]byte{}
	}
	r.payloads[namespace] = append([]byte(nil), payload...)
	return nil
}

// EnsureBootstrapped connects the pairing DHT. Concurrent callers share one attempt.
func (r *Libp2pDHTRouter) EnsureBootstrapped(ctx context.Context) error {
	return r.ensureBootstrapped(ctx)
}

func (r *Libp2pDHTRouter) ensureBootstrapped(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.bootstrapped {
		r.mu.Unlock()
		return nil
	}
	wait := r.bootstrapWait
	if wait == nil {
		wait = make(chan struct{})
		r.bootstrapWait = wait
		go r.finishBootstrap(wait)
	}
	r.mu.Unlock()
	select {
	case <-wait:
	case <-ctx.Done():
		return ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bootstrapped {
		return nil
	}
	if r.bootstrapErr != nil {
		return r.bootstrapErr
	}
	return errors.New("pairing DHT bootstrap did not finish")
}

// hasConnectedBootstrapPeer reports a configured bootstrap peer that completed
// a libp2p connection. The peer may still be a DHT client.
func (r *Libp2pDHTRouter) hasConnectedBootstrapPeer() bool {
	return len(r.connectedBootstrapInfos()) > 0
}

// connectedBootstrapIsClient is true when every connected bootstrap peer is
// still a DHT client. A peer that speaks the DHT protocol is left for the
// routing table instead of the direct pairing stream.
func (r *Libp2pDHTRouter) connectedBootstrapIsClient() bool {
	infos := r.connectedBootstrapInfos()
	if len(infos) == 0 {
		return false
	}
	for _, info := range infos {
		supported, err := r.host.Peerstore().SupportsProtocols(info.ID, dht.ProtocolDHT)
		if err == nil && len(supported) > 0 {
			return false
		}
	}
	return true
}

func (r *Libp2pDHTRouter) connectedBootstrapInfos() []peer.AddrInfo {
	if r == nil || r.host == nil {
		return nil
	}
	connected := map[peer.ID]struct{}{}
	for _, id := range r.host.Network().Peers() {
		if id != "" && id != r.host.ID() {
			connected[id] = struct{}{}
		}
	}
	var out []peer.AddrInfo
	seen := map[peer.ID]struct{}{}
	for _, raw := range r.bootstrapPeerList() {
		addr, err := ma.NewMultiaddr(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil || info == nil || info.ID == "" {
			continue
		}
		if _, ok := connected[info.ID]; !ok {
			continue
		}
		if _, ok := seen[info.ID]; ok {
			continue
		}
		seen[info.ID] = struct{}{}
		addrs := r.host.Peerstore().Addrs(info.ID)
		if len(addrs) == 0 {
			addrs = info.Addrs
		}
		out = append(out, peer.AddrInfo{ID: info.ID, Addrs: addrs})
	}
	return out
}

// fetchConnectedPairing reads the namespace from bootstrap peers we can
// already dial. A private node does not need those peers to be DHT servers.
func (r *Libp2pDHTRouter) fetchConnectedPairing(ctx context.Context, namespace string) [][]byte {
	var out [][]byte
	for _, info := range r.connectedBootstrapInfos() {
		payload, err := r.FetchPairing(ctx, info, namespace)
		if err != nil || len(payload) == 0 {
			continue
		}
		out = append(out, payload)
	}
	return out
}

// finishBootstrap keeps dialing until a peer lands in the routing table.
// The first attempt often runs before the other engine is listening, and an
// empty table must not count as finished or pairing never retries the dial.
func (r *Libp2pDHTRouter) finishBootstrap(wait chan struct{}) {
	bootCtx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			// The peer we just dialed is still a DHT client until some other
			// node dials one of our announced addresses back. Ask it to, then
			// look at the routing table again. A peer that stays a client is
			// still a place to serve the pairing stream.
			if r.shouldProbeRemoteDialback() {
				r.tryRemoteDialback(bootCtx, nil)
			}
			if r.connectedBootstrapIsClient() {
				err = nil
				break
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-bootCtx.Done():
				timer.Stop()
				err = bootCtx.Err()
			case <-timer.C:
			}
		}
		if bootCtx.Err() != nil && err == nil {
			err = bootCtx.Err()
		}
		if bootCtx.Err() != nil {
			break
		}
		err = acceptedBootstrap(r.Bootstrap(bootCtx, r.bootstrapPeerList()), r.routingTableSize(), r.bootstrapPeerList())
		if err == nil {
			break
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.bootstrapped = true
		r.bootstrapErr = nil
	} else {
		r.bootstrapErr = err
	}
	r.bootstrapWait = nil
	close(wait)
}

func (r *Libp2pDHTRouter) handlePairingStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(8 * time.Second))
	reader := bufio.NewReader(io.LimitReader(stream, 512))
	namespace, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	namespace = strings.TrimSpace(namespace)
	r.mu.Lock()
	payload := append([]byte(nil), r.payloads[namespace]...)
	r.mu.Unlock()
	var lengthBuf [4]byte
	binary.BigEndian.PutUint32(lengthBuf[:], uint32(len(payload)))
	if _, err := stream.Write(lengthBuf[:]); err != nil {
		return
	}
	if len(payload) > 0 {
		_, _ = stream.Write(payload)
	}
}
