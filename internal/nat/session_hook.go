package nat

import "sync"

// DisconnectHook is told when the last connection to a peer is gone.
// InboundHook is told when an authenticated stream is accepted or dialed.
var (
	hookMu         sync.Mutex
	disconnectHook func(peerID string)
	inboundHook    func(peerID, remote string, localInitiated bool)
	newAddrHook    func()
)

// SetSessionHooks installs the process callbacks. Nil leaves the previous one.
func SetSessionHooks(onDisconnect func(string), onInbound func(string, string, bool)) {
	hookMu.Lock()
	if onDisconnect != nil {
		disconnectHook = onDisconnect
	}
	if onInbound != nil {
		inboundHook = onInbound
	}
	hookMu.Unlock()
}

// SetNewAddrHook runs when a DHT lookup learns dial addresses the pass did not have.
func SetNewAddrHook(fn func()) {
	hookMu.Lock()
	newAddrHook = fn
	hookMu.Unlock()
}

// NotifyNewDialAddrs asks for another dial pass now that candidates exist.
func NotifyNewDialAddrs() {
	hookMu.Lock()
	fn := newAddrHook
	hookMu.Unlock()
	if fn != nil {
		fn()
	}
}

// NotifyDisconnected runs the hook when a peer has no connection left.
func NotifyDisconnected(peerID string) {
	hookMu.Lock()
	fn := disconnectHook
	hookMu.Unlock()
	if fn != nil && peerID != "" {
		fn(peerID)
	}
}

// NotifyInbound runs the hook for an authenticated stream.
func NotifyInbound(peerID, remote string, localInitiated bool) {
	hookMu.Lock()
	fn := inboundHook
	hookMu.Unlock()
	if fn != nil && peerID != "" {
		fn(peerID, remote, localInitiated)
	}
}
