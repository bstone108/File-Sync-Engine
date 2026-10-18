package discovery

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	ma "github.com/multiformats/go-multiaddr"
)

// WatchObservedAddresses reports the IP addresses remote peers observe for
// this node, using the process libp2p host. It does not open a second DHT.
// Bridge and other unusable addresses are filtered by the advertise package.
func WatchObservedAddresses(ctx context.Context, _ []string, note func(string)) {
	if note == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Use the process libp2p host. A second DHT client here dials the public
	// bootstrap continuously even when this engine is not looking a peer up.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var sub event.Subscription
	defer func() {
		if sub != nil {
			_ = sub.Close()
		}
	}()
	for ctx.Err() == nil {
		if sub == nil {
			if h := sharedHost(); h != nil {
				opened, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
				if err == nil {
					sub = opened
				}
			}
		}
		if sub != nil {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub.Out():
				if !ok {
					return
				}
				completed, ok := ev.(event.EvtPeerIdentificationCompleted)
				if !ok {
					continue
				}
				if ip := observedIP(completed.ObservedAddr); ip != "" {
					note(ip)
				}
			case <-ticker.C:
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
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
