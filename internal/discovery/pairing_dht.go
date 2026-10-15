package discovery

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"

	corediscovery "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

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
	if err := r.storePairing(namespace, payload); err != nil {
		return err
	}
	if err := r.ensureBootstrapped(ctx); err != nil {
		return err
	}
	if _, err := r.discovery.Advertise(ctx, namespace); err != nil {
		return err
	}
	r.mu.Lock()
	r.advertise[namespace] = struct{}{}
	r.mu.Unlock()
	return nil
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
	if err := r.ensureBootstrapped(ctx); err != nil {
		return nil, err
	}
	peerCh, err := r.discovery.FindPeers(ctx, namespace, corediscovery.Limit(16))
	if err != nil {
		return nil, err
	}
	payloads := [][]byte{}
	for info := range peerCh {
		if info.ID == "" || info.ID == r.host.ID() {
			continue
		}
		payload, fetchErr := r.FetchPairing(ctx, info, namespace)
		if fetchErr != nil || len(payload) == 0 {
			continue
		}
		payloads = append(payloads, payload)
		if len(payloads) >= 8 {
			break
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
func (r *Libp2pDHTRouter) FetchPairing(ctx context.Context, info peer.AddrInfo, namespace string) ([]byte, error) {
	if r == nil || r.host == nil {
		return nil, fmt.Errorf("libp2p DHT router is not initialized")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := r.host.Connect(dialCtx, info); err != nil {
		return nil, err
	}
	stream, err := r.host.NewStream(dialCtx, info.ID, protocol.ID(PairingProtocol))
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(8 * time.Second))
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

func (r *Libp2pDHTRouter) storePairing(namespace string, payload []byte) error {
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
	if r.payloads == nil {
		r.payloads = map[string][]byte{}
	}
	r.payloads[namespace] = append([]byte(nil), payload...)
	return nil
}

func (r *Libp2pDHTRouter) ensureBootstrapped(ctx context.Context) error {
	r.mu.Lock()
	ready := r.bootstrapped
	r.mu.Unlock()
	if ready {
		return nil
	}
	if err := r.Bootstrap(ctx, DefaultPublicDHTBootstrapPeers); err != nil {
		return err
	}
	r.mu.Lock()
	r.bootstrapped = true
	r.mu.Unlock()
	return nil
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
