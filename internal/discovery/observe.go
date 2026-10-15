package discovery

import (
	"context"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// WatchObservedAddresses bootstraps a client DHT and reports the IP addresses
// remote peers observe for this node. The callback may run more than once.
// Bridge and other unusable addresses are filtered by the advertise package.
func WatchObservedAddresses(ctx context.Context, peers []string, note func(string)) {
	if note == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err := watchObservedOnce(ctx, peers, note)
		if ctx.Err() != nil || err == nil {
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func watchObservedOnce(ctx context.Context, peers []string, note func(string)) error {
	h, err := libp2p.New()
	if err != nil {
		return err
	}
	defer h.Close()
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		return err
	}
	defer sub.Close()
	kad, err := dht.New(ctx, h, dht.Mode(dht.ModeClient))
	if err != nil {
		return err
	}
	defer kad.Close()
	bootstrapPeers := append([]string(nil), peers...)
	if len(bootstrapPeers) == 0 {
		bootstrapPeers = append([]string(nil), DefaultPublicDHTBootstrapPeers...)
	}
	infos, err := parsePeerAddrInfos(ctx, bootstrapPeers)
	if err != nil {
		return err
	}
	dialObservedBootstrap(ctx, h, preferDialableTransports(infos))
	_ = kad.Bootstrap(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-sub.Out():
			if !ok {
				return nil
			}
			completed, ok := ev.(event.EvtPeerIdentificationCompleted)
			if !ok {
				continue
			}
			if ip := observedIP(completed.ObservedAddr); ip != "" {
				note(ip)
			}
		}
	}
}

func dialObservedBootstrap(ctx context.Context, h host.Host, infos []peer.AddrInfo) {
	limit := len(infos)
	if limit > maxBootstrapDials {
		limit = maxBootstrapDials
	}
	for _, info := range infos[:limit] {
		connectCtx, cancel := context.WithTimeout(ctx, bootstrapDialTimeout)
		_ = h.Connect(connectCtx, info)
		cancel()
	}
}

func observedIP(multiaddr ma.Multiaddr) string {
	if multiaddr == nil {
		return ""
	}
	ip := ""
	ma.ForEach(multiaddr, func(component ma.Component) bool {
		switch component.Protocol().Code {
		case ma.P_IP4, ma.P_IP6:
			ip = component.Value()
			return false
		default:
			return true
		}
	})
	return ip
}
