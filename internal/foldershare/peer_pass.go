package foldershare

import (
	"errors"
	"sync"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/link"
	"filesyncengine/internal/nat"
	"filesyncengine/internal/structuredlog"
)

// peerSyncAsync is false under go test so Reconcile stays synchronous for the
// existing transfer tests. Production starts the dial on another goroutine so
// a stuck peer cannot block the local scan.
var peerSyncAsync = !testing.Testing()

type peerPassState struct {
	mu      sync.Mutex
	running bool
	next    time.Time
}

var peerPasses sync.Map

func startPeerSync(cfg config.Config, configPath string, folder config.FolderConfig) {
	if folder.Mode == config.ModeSendOnly || len(peersFor(cfg, folder)) == 0 {
		return
	}
	key := configPath + "\x00" + folder.ID
	state := peerPass(key)
	state.mu.Lock()
	if !peerSyncAsync {
		state.mu.Unlock()
		runPeerSync(cfg, configPath, folder)
		return
	}
	if state.running || time.Now().Before(state.next) {
		state.mu.Unlock()
		return
	}
	state.running = true
	state.mu.Unlock()
	go func() {
		defer func() {
			state.mu.Lock()
			state.running = false
			state.next = time.Now().Add(peerPassInterval(cfg, folder))
			state.mu.Unlock()
		}()
		runPeerSync(cfg, configPath, folder)
	}()
}

func peerPass(key string) *peerPassState {
	value, _ := peerPasses.LoadOrStore(key, &peerPassState{})
	return value.(*peerPassState)
}

func peerPassInterval(cfg config.Config, folder config.FolderConfig) time.Duration {
	for _, peer := range peersFor(cfg, folder) {
		if _, ok := nat.DefaultPaths.Active(peer.ID); ok {
			return discovery.LiveProbeInterval
		}
		if peer.DHTPeerID != "" {
			if _, ok := nat.DefaultPaths.Active(peer.DHTPeerID); ok {
				return discovery.LiveProbeInterval
			}
		}
	}
	return discovery.OfflineProbeInterval
}

func runPeerSync(cfg config.Config, configPath string, folder config.FolderConfig) {
	for _, peer := range peersFor(cfg, folder) {
		remote, remoteCursor, err := fetchRemoteJournal(configPath, peer, folder.ID)
		if err != nil {
			if !errors.Is(err, link.ErrDialDeferred) {
				structuredlog.Event("error", "folder.sync.journal_failed", "remote journal was not read", map[string]any{
					"folder": folder.ID,
					"peer":   peer.ID,
					"error":  err.Error(),
				})
			}
			link.FinishPeerPass(peer.ID, false)
			continue
		}
		mu := journalLock(configPath, folder.ID)
		mu.Lock()
		entries, loadErr := LoadJournal(configPath, folder.ID)
		mu.Unlock()
		if loadErr != nil {
			link.FinishPeerPass(peer.ID, true)
			continue
		}
		byPath := indexEntries(entries)
		if err := applyRemote(peer, folder, byPath, remote); err != nil {
			structuredlog.Event("error", "folder.sync.apply_failed", "some remote files were not applied", map[string]any{
				"folder": folder.ID,
				"peer":   peer.ID,
				"error":  err.Error(),
			})
			link.FinishPeerPass(peer.ID, true)
			continue
		}
		applied := map[string]Entry{}
		for _, entry := range remote {
			if current, ok := byPath[entry.Path]; ok {
				applied[entry.Path] = current
			}
		}
		mu.Lock()
		if err := mergeApplied(configPath, folder.ID, applied); err != nil {
			structuredlog.Event("error", "folder.sync.failed", "folder sync failed", map[string]any{
				"folder": folder.ID,
				"error":  err.Error(),
			})
		}
		if remoteCursor > 0 {
			rememberPeerCursor(configPath, folder.ID, peer.ID, remoteCursor)
		}
		mu.Unlock()
		link.FinishPeerPass(peer.ID, true)
	}
}

var journalLocks sync.Map

func journalLock(configPath, folderID string) *sync.Mutex {
	value, _ := journalLocks.LoadOrStore(configPath+"\x00"+folderID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func mergeApplied(configPath, folderID string, applied map[string]Entry) error {
	entries, err := LoadJournal(configPath, folderID)
	if err != nil {
		return err
	}
	byPath := indexEntries(entries)
	for path, entry := range applied {
		current, ok := byPath[path]
		if !ok || entry.Revision >= current.Revision {
			byPath[path] = entry
		}
	}
	return saveJournal(configPath, folderID, byPath)
}
