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

	corediscovery "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"filesyncengine/internal/structuredlog"
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
	if err := r.ensureBootstrapped(ctx); err != nil {
		structuredlog.Event("error", "pairing.publish.failed", "pairing DHT bootstrap failed", map[string]any{"namespace": namespace, "error": err.Error()})
		return err
	}
	if err := r.advertisePairing(ctx, namespace); err != nil {
		structuredlog.Event("error", "pairing.publish.failed", "pairing code was not advertised on the DHT", map[string]any{"namespace": namespace, "error": err.Error()})
		return err
	}
	r.mu.Lock()
	r.advertise[namespace] = struct{}{}
	r.mu.Unlock()
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
	if err := r.ensureBootstrapped(lookupCtx); err != nil {
		structuredlog.Event("error", "pairing.lookup.failed", "pairing DHT bootstrap failed", map[string]any{"namespace": namespace, "error": err.Error()})
		if lookupCtx.Err() != nil && ctx.Err() == nil {
			return nil, nil
		}
		return nil, err
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
	for info := range peerCh {
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
	if err := r.host.Connect(dialCtx, dialInfo); err != nil {
		return nil, err
	}
	stream, err := r.host.NewStream(dialCtx, info.ID, protocol.ID(PairingProtocol))
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

// finishBootstrap connects once. Lookup deadlines wait on the channel and do
// not cancel this attempt.
func (r *Libp2pDHTRouter) finishBootstrap(wait chan struct{}) {
	bootCtx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	err := r.Bootstrap(bootCtx, r.bootstrapPeerList())
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
