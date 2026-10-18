package foldershare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"filesyncengine/internal/config"
)

// A local scan hashes every file. The reconcile loop runs every second, so
// the scan runs on a filesystem wake or every 30s, not on every tick.
const localScanInterval = 30 * time.Second

var (
	journalMu     sync.Mutex
	journalCache  = map[string]cachedJournal{}
	peerCursors   = map[string]int64{}
	lastLocalScan = map[string]time.Time{}
	forceScan     = map[string]struct{}{}
	changedPaths  = map[string]map[string]struct{}{}
	wakeCh        = make(chan struct{}, 1)
	passReporter  func(folderID string, peer config.PeerConfig, writes, fetched, reused int)
)

type cachedJournal struct {
	mod    time.Time
	size   int64
	raw    []byte
	stored journalFile
}

// Wake asks the daemon to reconcile promptly. A file-monitor event uses it
// so a change does not wait for the next quiet poll. Wake does not by itself
// rehash the folder; NoteChanged names the path, and RequestFullScan asks
// for a full pass.
func Wake() {
	select {
	case wakeCh <- struct{}{}:
	default:
	}
}

// NoteChanged records one filesystem path so the next reconcile hashes that
// path instead of the whole folder.
func NoteChanged(folderID, path string) {
	if folderID == "" || path == "" {
		return
	}
	journalMu.Lock()
	set := changedPaths[folderID]
	if set == nil {
		set = map[string]struct{}{}
		changedPaths[folderID] = set
	}
	set[path] = struct{}{}
	journalMu.Unlock()
	Wake()
}

// RequestFullScan makes the next reconcile hash the folder. The monitor's
// fallback scan uses it. A single file event uses NoteChanged instead.
func RequestFullScan(folderID string) {
	if folderID == "" {
		return
	}
	journalMu.Lock()
	forceScan[folderID] = struct{}{}
	journalMu.Unlock()
	Wake()
}

// WakeChannel is signaled by Wake. The daemon selects on it.
func WakeChannel() <-chan struct{} { return wakeCh }

// SetPassReporter receives real transfer counts after a peer pass that moved
// or reused blocks. An empty pass is not reported, so the API does not claim
// a direct transfer that did not happen.
func SetPassReporter(fn func(folderID string, peer config.PeerConfig, writes, fetched, reused int)) {
	journalMu.Lock()
	passReporter = fn
	journalMu.Unlock()
}

func reportPass(folderID string, peer config.PeerConfig, writes, fetched, reused int) {
	if writes+fetched+reused == 0 {
		return
	}
	journalMu.Lock()
	fn := passReporter
	journalMu.Unlock()
	if fn != nil {
		fn(folderID, peer, writes, fetched, reused)
	}
}

func localScanDue(configPath, folderID string) bool {
	key := configPath + "\x00" + folderID
	journalMu.Lock()
	defer journalMu.Unlock()
	if _, ok := forceScan[folderID]; ok {
		delete(forceScan, folderID)
		delete(changedPaths, folderID)
		lastLocalScan[key] = time.Now()
		return true
	}
	last := lastLocalScan[key]
	if last.IsZero() || time.Since(last) >= localScanInterval {
		delete(changedPaths, folderID)
		lastLocalScan[key] = time.Now()
		return true
	}
	return false
}

func takeChangedPaths(folderID string) []string {
	journalMu.Lock()
	defer journalMu.Unlock()
	set := changedPaths[folderID]
	delete(changedPaths, folderID)
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	return out
}

func loadJournalFile(configPath, folderID string) (journalFile, error) {
	path := JournalPath(configPath, folderID)
	info, err := os.Stat(path)
	if err != nil {
		return journalFile{}, err
	}
	journalMu.Lock()
	cached, ok := journalCache[path]
	journalMu.Unlock()
	if ok && cached.size == info.Size() && cached.mod.Equal(info.ModTime()) {
		return cached.stored, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return journalFile{}, err
	}
	var stored journalFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return journalFile{}, err
	}
	rememberJournalBytes(path, info, data, stored)
	return stored, nil
}

func storeJournalCache(path string, stored journalFile) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	rememberJournalBytes(path, info, data, stored)
}

func rememberJournalBytes(path string, info os.FileInfo, data []byte, stored journalFile) {
	journalMu.Lock()
	journalCache[path] = cachedJournal{mod: info.ModTime(), size: info.Size(), raw: append([]byte(nil), data...), stored: stored}
	journalMu.Unlock()
}

func cachedJournalBytes(path string) ([]byte, bool) {
	journalMu.Lock()
	defer journalMu.Unlock()
	cached, ok := journalCache[path]
	if !ok || cached.raw == nil {
		return nil, false
	}
	return cached.raw, true
}

// JournalDelta returns entries newer than after. A zero cursor, or a journal
// that has never assigned one, returns the full list so an old peer still syncs.
func JournalDelta(configPath, folderID string, after int64) ([]Entry, int64, bool, error) {
	stored, err := loadJournalFile(configPath, folderID)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, 0, false, nil
		}
		return nil, 0, false, err
	}
	if after <= 0 || stored.Cursor == 0 {
		entries := stored.Entries
		if entries == nil {
			entries = []Entry{}
		}
		return entries, stored.Cursor, false, nil
	}
	out := []Entry{}
	for _, entry := range stored.Entries {
		if entry.Cursor > after {
			out = append(out, entry)
		}
	}
	return out, stored.Cursor, true, nil
}

// CountLiveFiles is the journal's live path count. The folder index stays at
// zero when foldershare, not the metadata engine, owns the files.
func CountLiveFiles(configPath, folderID string) int {
	entries, err := LoadJournal(configPath, folderID)
	if err != nil {
		return 0
	}
	n := 0
	for _, entry := range entries {
		if entry.Path != "" && !entry.Deleted {
			n++
		}
	}
	return n
}

func peerCursor(configPath, folderID, peerID string) int64 {
	key := cursorKey(configPath, folderID, peerID)
	journalMu.Lock()
	if cursor, ok := peerCursors[key]; ok {
		journalMu.Unlock()
		return cursor
	}
	journalMu.Unlock()
	data, err := os.ReadFile(cursorPath(configPath, folderID))
	if err != nil {
		return 0
	}
	var stored map[string]int64
	if err := json.Unmarshal(data, &stored); err != nil {
		return 0
	}
	journalMu.Lock()
	for id, cursor := range stored {
		peerCursors[cursorKey(configPath, folderID, id)] = cursor
	}
	cursor := peerCursors[key]
	journalMu.Unlock()
	return cursor
}

func rememberPeerCursor(configPath, folderID, peerID string, cursor int64) {
	if cursor <= 0 || peerID == "" {
		return
	}
	key := cursorKey(configPath, folderID, peerID)
	journalMu.Lock()
	if peerCursors[key] >= cursor {
		journalMu.Unlock()
		return
	}
	peerCursors[key] = cursor
	journalMu.Unlock()
	path := cursorPath(configPath, folderID)
	data, err := os.ReadFile(path)
	stored := map[string]int64{}
	if err == nil {
		_ = json.Unmarshal(data, &stored)
	}
	if stored[peerID] >= cursor {
		return
	}
	stored[peerID] = cursor
	encoded, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func cursorKey(configPath, folderID, peerID string) string {
	return configPath + "\x00" + folderID + "\x00" + peerID
}

func cursorPath(configPath, folderID string) string {
	return JournalPath(configPath, folderID) + ".cursors"
}
