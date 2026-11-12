package foldershare

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/link"
	"filesyncengine/internal/nat"
	"filesyncengine/internal/structuredlog"
)

// discovery intervals are the production defaults. Tests replace passOffline.

// peerSyncAsync is false under go test so Reconcile stays synchronous for the
// existing transfer tests. Production starts the dial on another goroutine so
// a stuck peer cannot block the local scan.
var peerSyncAsync = !testing.Testing()

func startPeerSync(cfg config.Config, configPath string, folder config.FolderConfig) {
	if schedulerOn() || folder.Mode == config.ModeSendOnly || len(peersFor(cfg, folder)) == 0 {
		return
	}
	if !peerSyncAsync {
		ctx, cancel := context.WithTimeout(context.Background(), passDeadline)
		defer cancel()
		runPeerSync(ctx, cfg, configPath, folder)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), passDeadline)
		defer cancel()
		passFn(ctx, cfg, configPath, folder)
	}()
}

var (
	scheduleOnce sync.Once
	scheduleOn   atomic.Bool
	passDeadline = 12 * time.Second
	// passOffline and passLive are the production intervals. Tests shorten them.
	passOffline = discovery.OfflineProbeInterval
	passLive    = discovery.LiveProbeInterval
	passFn      = runPeerSync
)

func schedulerOn() bool { return scheduleOn.Load() }

// StartSchedulers scans on its own clock and dials on a clock that does not
// slip when a dial is slow. The first dial pass starts immediately.
func StartSchedulers(ctx context.Context, configPath string, load func() config.Config) {
	if ctx == nil {
		ctx = context.Background()
	}
	if load == nil {
		load = func() config.Config { return config.Config{} }
	}
	scheduleOnce.Do(func() {
		scheduleOn.Store(true)
		go scanLoop(ctx, configPath, load)
		go peerLoop(ctx, configPath, load)
	})
}

func scanLoop(ctx context.Context, configPath string, load func() config.Config) {
	ticker := time.NewTicker(localScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-WakeChannel():
		case <-ticker.C:
		}
		cfg := load()
		for _, folder := range cfg.Folders {
			if !folder.Enabled || folder.Path == "" {
				continue
			}
			_ = reconcileFolder(cfg, configPath, folder)
		}
	}
}

func peerLoop(ctx context.Context, configPath string, load func() config.Config) {
	interval := passOffline
	var next time.Time
	for {
		if wait := time.Until(next); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		cfg := load()
		passCtx, cancel := context.WithTimeout(ctx, passDeadline)
		live := false
		for _, folder := range cfg.Folders {
			if !folder.Enabled || folder.Path == "" || folder.Mode == config.ModeSendOnly {
				continue
			}
			if passFn(passCtx, cfg, configPath, folder) {
				live = true
			}
		}
		cancel()
		if live {
			interval = passLive
		} else {
			interval = passOffline
		}
		next = started.Add(interval)
	}
}

func runPeerSync(ctx context.Context, cfg config.Config, configPath string, folder config.FolderConfig) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	live := false
	for _, peer := range peersFor(cfg, folder) {
		if ctx.Err() != nil {
			return live
		}
		remote, remoteCursor, err := fetchRemoteJournal(ctx, configPath, peer, folder.ID)
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
		if _, ok := nat.DefaultPaths.Active(peer.ID); ok {
			live = true
		}
	}
	return live
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
