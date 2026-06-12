package transfercontrol

import "sync"

// Control tracks runtime-only pause and cancel scopes for transfer passes.
// It is safe for concurrent API handlers and daemon scheduling loops.
type Control struct {
	mu        sync.RWMutex
	paused    map[string]struct{}
	cancelled map[string]struct{}
}

func New() *Control {
	return &Control{paused: map[string]struct{}{}, cancelled: map[string]struct{}{}}
}

func (c *Control) Pause(folderID, peerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused[scopeKey(folderID, peerID)] = struct{}{}
}

func (c *Control) Resume(folderID, peerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.paused, scopeKey(folderID, peerID))
}

func (c *Control) Cancel(folderID, peerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelled[scopeKey(folderID, peerID)] = struct{}{}
}

func (c *Control) ClearCancel(folderID, peerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cancelled, scopeKey(folderID, peerID))
	delete(c.cancelled, scopeKey(folderID, ""))
	delete(c.cancelled, scopeKey("", peerID))
}

func (c *Control) IsPaused(folderID, peerID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return scopeMatched(c.paused, folderID, peerID)
}

func (c *Control) IsCancelled(folderID, peerID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return scopeMatched(c.cancelled, folderID, peerID)
}

func scopeKey(folderID, peerID string) string {
	return folderID + "\x00" + peerID
}

func scopeMatched(scopes map[string]struct{}, folderID, peerID string) bool {
	_, exact := scopes[scopeKey(folderID, peerID)]
	_, folder := scopes[scopeKey(folderID, "")]
	_, peer := scopes[scopeKey("", peerID)]
	return exact || folder || peer
}
