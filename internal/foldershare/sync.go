// Package foldershare keeps paired folders aligned in both directions.
// Each node records a per-path revision. A peer applies a remote entry only
// when that revision is newer, so a create on one side is not deleted by a
// mirror pull from the other side.
package foldershare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/link"
	"filesyncengine/internal/structuredlog"
	"filesyncengine/internal/transferprogress"
)

// Entry is one path in a folder's sync journal.
// Blocks is the file's block list (4MB hashes and offsets). Sub-block hashes
// are not part of this metadata.
type Entry struct {
	Path     string      `json:"path"`
	SHA256   string      `json:"sha256,omitempty"`
	Size     int64       `json:"size"`
	Deleted  bool        `json:"deleted"`
	Revision int64       `json:"revision"`
	Origin   string      `json:"origin,omitempty"`
	Blocks   []FileBlock `json:"blocks,omitempty"`
	// Cursor is the folder journal cursor at the moment this entry changed.
	// A peer that already has an older cursor asks only for newer entries.
	Cursor int64 `json:"cursor,omitempty"`
}

type journalFile struct {
	Cursor  int64   `json:"cursor,omitempty"`
	Entries []Entry `json:"entries"`
}

// Reconcile scans enabled folders and exchanges newer revisions with paired peers.
// paused, when set, skips folders whose sync is paused.
func Reconcile(cfg config.Config, configPath string, paused func(folderID string) bool) {
	for _, folder := range cfg.Folders {
		if !folder.Enabled || folder.Path == "" {
			continue
		}
		if folder.Mode != config.ModeSendReceive && folder.Mode != config.ModeSendOnly && folder.Mode != config.ModeReceiveOnly {
			continue
		}
		if paused != nil && paused(folder.ID) {
			continue
		}
		if err := reconcileFolder(cfg, configPath, folder); err != nil {
			structuredlog.Event("error", "folder.sync.failed", "folder sync failed", map[string]any{
				"folder": folder.ID,
				"error":  err.Error(),
			})
		}
	}
}

// LoadJournal returns the persisted journal for a folder. A missing journal is empty.
func LoadJournal(configPath, folderID string) ([]Entry, error) {
	stored, err := loadJournalFile(configPath, folderID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return stored.Entries, nil
}

// JournalIndex summarizes the journal for the folder API. The metadata store
// stays at the empty hash when foldershare owns the files, so the GUI would
// otherwise say the folder was never scanned.
func JournalIndex(configPath, folderID string) (live, verified, blocks int, bytes int64, hash string, ok bool) {
	entries, err := LoadJournal(configPath, folderID)
	if err != nil || len(entries) == 0 {
		return 0, 0, 0, 0, "", false
	}
	paths := make([]string, 0, len(entries))
	byPath := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		if entry.Path == "" || entry.Deleted {
			continue
		}
		paths = append(paths, entry.Path)
		byPath[entry.Path] = entry
	}
	if len(paths) == 0 {
		return 0, 0, 0, 0, "", false
	}
	sort.Strings(paths)
	sum := sha256.New()
	for _, path := range paths {
		entry := byPath[path]
		live++
		if entry.Size > 0 {
			bytes += entry.Size
		}
		blocks += len(entry.Blocks)
		if entry.SHA256 != "" {
			verified++
		}
		fmt.Fprintf(sum, "%s\x00%s\x00%d\x00", path, entry.SHA256, entry.Size)
	}
	return live, verified, blocks, bytes, hex.EncodeToString(sum.Sum(nil)), true
}

// JournalPath is the on-disk journal for folderID next to the daemon config.
func JournalPath(configPath, folderID string) string {
	return filepath.Join(filepath.Dir(configPath), "sync-journals", safeID(folderID)+".json")
}

func reconcileFolder(cfg config.Config, configPath string, folder config.FolderConfig) error {
	entries, err := LoadJournal(configPath, folder.ID)
	if err != nil {
		return err
	}
	byPath := indexEntries(entries)
	origin := cfg.Identity.PublicKey
	if origin == "" {
		origin = cfg.NodeName
	}
	if folder.Mode != config.ModeReceiveOnly {
		if localScanDue(configPath, folder.ID) {
			if err := absorbLocal(folder.Path, origin, byPath); err != nil {
				return err
			}
		} else if err := absorbPaths(folder.Path, origin, byPath, takeChangedPaths(folder.ID)); err != nil {
			return err
		}
		if err := saveJournal(configPath, folder.ID, byPath); err != nil {
			return err
		}
	}
	if folder.Mode == config.ModeSendOnly {
		return nil
	}
	peers := peersFor(cfg, folder)
	for _, peer := range peers {
		remote, remoteCursor, err := fetchRemoteJournal(configPath, peer, folder.ID)
		if err != nil {
			structuredlog.Event("error", "folder.sync.journal_failed", "remote journal was not read", map[string]any{
				"folder": folder.ID,
				"peer":   peer.ID,
				"error":  err.Error(),
			})
			continue
		}
		if err := applyRemote(peer, folder, byPath, remote); err != nil {
			structuredlog.Event("error", "folder.sync.apply_failed", "some remote files were not applied", map[string]any{
				"folder": folder.ID,
				"peer":   peer.ID,
				"error":  err.Error(),
			})
			continue
		}
		// Advance only after every entry in this delta applied. A failed file
		// stays above the saved cursor and is requested again.
		if remoteCursor > 0 {
			rememberPeerCursor(configPath, folder.ID, peer.ID, remoteCursor)
		}
	}
	return saveJournal(configPath, folder.ID, byPath)
}

func absorbLocal(root, origin string, byPath map[string]Entry) error {
	seen, err := scanFolder(root)
	if err != nil {
		return err
	}
	for path, file := range seen {
		noteLocalFile(byPath, path, origin, file.sha, file.size, file.blocks)
	}
	for path, current := range byPath {
		if current.Deleted {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		markDeleted(byPath, path, origin)
	}
	return nil
}

// absorbPaths hashes only the paths a filesystem event named. Other journal
// entries stay as they are, including files this pass did not look at.
func absorbPaths(root, origin string, byPath map[string]Entry, paths []string) error {
	for _, path := range paths {
		rel := filepath.ToSlash(path)
		if filepath.IsAbs(path) {
			next, err := filepath.Rel(root, path)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(next)
		}
		if !safeRel(rel) {
			continue
		}
		target, ok := safeJoin(root, rel)
		if !ok {
			continue
		}
		info, err := os.Stat(target)
		if err != nil {
			if os.IsNotExist(err) {
				markDeleted(byPath, rel, origin)
			}
			continue
		}
		if info.IsDir() {
			continue
		}
		hash, size, blocks, err := InspectFile(target)
		if err != nil {
			return err
		}
		noteLocalFile(byPath, rel, origin, hash, size, blocks)
	}
	return nil
}

func noteLocalFile(byPath map[string]Entry, rel, origin, hash string, size int64, blocks []FileBlock) {
	current, ok := byPath[rel]
	if ok && !current.Deleted && current.SHA256 == hash {
		if len(current.Blocks) == 0 && len(blocks) > 0 {
			current.Blocks = blocks
			byPath[rel] = current
		}
		return
	}
	revision := int64(1)
	if ok && current.Revision >= revision {
		revision = current.Revision + 1
	}
	byPath[rel] = Entry{Path: rel, SHA256: hash, Size: size, Blocks: blocks, Revision: revision, Origin: origin}
}

func markDeleted(byPath map[string]Entry, rel, origin string) {
	current, ok := byPath[rel]
	if !ok || current.Deleted {
		return
	}
	current.Deleted = true
	current.SHA256 = ""
	current.Size = 0
	current.Blocks = nil
	current.Revision++
	current.Origin = origin
	byPath[rel] = current
}

const (
	transferWorkers = 16
)

// fileRetryBackoff is the capped wait between attempts in one reconcile pass.
// A file that still has missing blocks stays pending: the journal revision is
// not advanced, and the next pass (or the next peer) fetches only those blocks.
var fileRetryBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

type transferJob struct {
	entry  Entry
	target string
}

func applyRemote(peer config.PeerConfig, folder config.FolderConfig, byPath map[string]Entry, remote []Entry) error {
	var jobs []transferJob
	var mu sync.Mutex
	var applyErr error
	var passDeletes, passMoves int
	remoteLive := map[string]Entry{}
	for _, entry := range remote {
		if entry.Deleted || entry.SHA256 == "" || !safeRel(entry.Path) {
			continue
		}
		remoteLive[entry.SHA256] = entry
	}
	for _, entry := range remote {
		if !entry.Deleted || !safeRel(entry.Path) {
			continue
		}
		current, ok := byPath[entry.Path]
		if ok && current.Revision >= entry.Revision {
			continue
		}
		if !ok || current.Deleted || current.SHA256 == "" {
			continue
		}
		dest, found := remoteLive[current.SHA256]
		if !found || dest.Path == entry.Path {
			continue
		}
		from, fromOK := safeJoin(folder.Path, entry.Path)
		to, toOK := safeJoin(folder.Path, dest.Path)
		if !fromOK || !toOK {
			continue
		}
		if _, statErr := os.Stat(from); statErr != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			continue
		}
		passMoves++
		byPath[entry.Path] = entry
		byPath[dest.Path] = dest
	}
	for _, entry := range remote {
		if !safeRel(entry.Path) {
			continue
		}
		current, ok := byPath[entry.Path]
		if ok && current.Revision >= entry.Revision {
			continue
		}
		target, ok := safeJoin(folder.Path, entry.Path)
		if !ok {
			continue
		}
		if entry.Deleted {
			err := os.Remove(target)
			if err != nil && !os.IsNotExist(err) {
				applyErr = errors.Join(applyErr, err)
				structuredlog.Event("error", "folder.sync.file_failed", "remote delete was not applied", map[string]any{
					"folder": folder.ID,
					"peer":   peer.ID,
					"path":   entry.Path,
					"error":  err.Error(),
				})
				continue
			}
			mu.Lock()
			byPath[entry.Path] = entry
			mu.Unlock()
			if err == nil {
				passDeletes++
			}
			continue
		}
		jobs = append(jobs, transferJob{entry: entry, target: target})
	}
	var remaining int64
	for _, job := range jobs {
		if job.entry.Size > 0 {
			remaining += job.entry.Size
		}
	}
	transferprogress.Note(peer.ID, 0, remaining)
	locals := indexLocalBlocks(folder.Path, byPath)
	var wg sync.WaitGroup
	var passWrites, passFetched, passReused int
	slots := make(chan struct{}, transferWorkers)
	var remainMu sync.Mutex
	note := func(delta, left int64) {
		transferprogress.Note(peer.ID, delta, left)
	}
	outcome, jobs, batchErr := applySmallBatches(peer, folder, jobs, byPath, &mu, &remainMu, &remaining, note)
	if batchErr != nil {
		applyErr = errors.Join(applyErr, batchErr)
	}
	passWrites += outcome.writes
	passFetched += outcome.fetched
	for _, job := range jobs {
		wg.Add(1)
		slots <- struct{}{}
		go func(job transferJob) {
			defer wg.Done()
			defer func() { <-slots }()
			var got int64
			fetched, reused, err := downloadFile(peer, folder, job.entry, job.target, locals, func(network, completed int64) {
				delta := completed - got
				if delta < 0 {
					delta = 0
				}
				got = completed
				remainMu.Lock()
				remaining -= delta
				if remaining < 0 {
					remaining = 0
				}
				left := remaining
				remainMu.Unlock()
				note(network, left)
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				applyErr = errors.Join(applyErr, err)
				structuredlog.Event("error", "folder.sync.file_failed", "remote file was not applied", map[string]any{
					"folder": folder.ID,
					"peer":   peer.ID,
					"path":   job.entry.Path,
					"error":  err.Error(),
				})
				leftover := job.entry.Size - got
				if leftover < 0 {
					leftover = 0
				}
				remainMu.Lock()
				remaining -= leftover
				if remaining < 0 {
					remaining = 0
				}
				left := remaining
				remainMu.Unlock()
				note(0, left)
				return
			}
			passFetched += fetched
			passReused += reused
			passWrites++
			byPath[job.entry.Path] = job.entry
		}(job)
	}
	wg.Wait()
	transferprogress.Note(peer.ID, 0, 0)
	reportPass(folder.ID, peer, PassStats{Writes: passWrites, Fetched: passFetched, Reused: passReused, Deletes: passDeletes, Moves: passMoves})
	return applyErr
}

type scanned struct {
	sha    string
	size   int64
	blocks []FileBlock
}

func scanFolder(root string) (map[string]scanned, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(root, 0o755); mkErr != nil {
				return nil, mkErr
			}
			return map[string]scanned{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("folder path %s is not a directory", root)
	}
	out := map[string]scanned{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(rel)
		if !safeRel(rel) || strings.HasPrefix(base, ".fse-") || strings.Contains(base, ".fse-sync-") {
			return nil
		}
		hash, size, blocks, err := InspectFile(path)
		if err != nil {
			return err
		}
		out[rel] = scanned{sha: hash, size: size, blocks: blocks}
		return nil
	})
	return out, err
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}

func finishDownload(tmp, target string, entry Entry) error {
	sum, size, err := hashFile(tmp)
	if err != nil {
		return err
	}
	if entry.Size > 0 && size != entry.Size {
		return fmt.Errorf("downloaded %s size %d, want %d", entry.Path, size, entry.Size)
	}
	if entry.SHA256 != "" && !strings.EqualFold(sum, entry.SHA256) {
		_ = os.Remove(tmp)
		return fmt.Errorf("downloaded %s checksum mismatch", entry.Path)
	}
	return os.Rename(tmp, target)
}

func fetchRemoteJournal(configPath string, peer config.PeerConfig, folderID string) ([]Entry, int64, error) {
	var entries []Entry
	after := peerCursor(configPath, folderID, peer.ID)
	var remoteCursor int64
	used, err := link.Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		endpoint := base + "/v1/sync-journal?folder=" + url.QueryEscape(folderID) + "&afterCursor=" + strconv.FormatInt(after, 10)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-FSE-API-Key", peer.APIKey)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("sync journal: %s", resp.Status)
		}
		var body struct {
			Entries []Entry `json:"entries"`
			Cursor  int64   `json:"cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return err
		}
		entries = body.Entries
		remoteCursor = body.Cursor
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	noteLinkDial(peer, used)
	return entries, remoteCursor, nil
}

var (
	linkRefreshMu sync.Mutex
	linkRefreshFn func(config.PeerConfig, bool)
	linkRefreshAt = map[string]time.Time{}
)

// SetLinkRefresher registers a callback that pulls a peer's current addresses
// after a successful sync dial. Failover refreshes immediately. Quiet links
// refresh at most every two minutes.
func SetLinkRefresher(fn func(config.PeerConfig, bool)) {
	linkRefreshMu.Lock()
	linkRefreshFn = fn
	linkRefreshMu.Unlock()
}

func noteLinkDial(peer config.PeerConfig, used string) {
	endpoints := link.OrderedEndpoints(peer)
	failedOver := len(endpoints) > 0 && strings.TrimRight(endpoints[0].Address, "/") != strings.TrimRight(used, "/")
	linkRefreshMu.Lock()
	fn := linkRefreshFn
	last := linkRefreshAt[peer.ID]
	if fn == nil || (!failedOver && time.Since(last) < 2*time.Minute) {
		linkRefreshMu.Unlock()
		return
	}
	linkRefreshAt[peer.ID] = time.Now()
	linkRefreshMu.Unlock()
	fn(peer, failedOver)
}

func peersFor(cfg config.Config, folder config.FolderConfig) []config.PeerConfig {
	allowed := map[string]struct{}{}
	for _, id := range folder.SharedWith {
		if id = strings.TrimSpace(id); id != "" {
			allowed[id] = struct{}{}
		}
	}
	out := []config.PeerConfig{}
	for _, peer := range cfg.Peers {
		if peer.APIKey == "" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[peer.ID]; !ok {
				continue
			}
		}
		if !link.HasAPIEndpoint(peer) {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func indexEntries(entries []Entry) map[string]Entry {
	out := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		if entry.Path == "" || !safeRel(entry.Path) {
			continue
		}
		out[entry.Path] = entry
	}
	return out
}

func saveJournal(configPath, folderID string, byPath map[string]Entry) error {
	previous, _ := loadJournalFile(configPath, folderID)
	prevByPath := indexEntries(previous.Entries)
	entries := make([]Entry, 0, len(byPath))
	cursor := previous.Cursor
	for _, entry := range byPath {
		old, ok := prevByPath[entry.Path]
		changed := !ok || old.Revision != entry.Revision || old.SHA256 != entry.SHA256 || old.Deleted != entry.Deleted || old.Size != entry.Size
		if changed || old.Cursor == 0 {
			cursor++
			entry.Cursor = cursor
		} else {
			entry.Cursor = old.Cursor
		}
		entries = append(entries, entry)
	}
	// Stable order keeps the file quiet when nothing changed.
	sortEntries(entries)
	stored := journalFile{Cursor: cursor, Entries: entries}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := JournalPath(configPath, folderID)
	if previous, ok := cachedJournalBytes(path); ok && bytesEqual(previous, data) {
		return nil
	}
	if previousBytes, readErr := os.ReadFile(path); readErr == nil && bytesEqual(previousBytes, data) {
		storeJournalCache(path, stored)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	storeJournalCache(path, stored)
	return nil
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sortEntries(entries []Entry) {
	for i := 1; i < len(entries); i++ {
		j := i
		for j > 0 && entries[j].Path < entries[j-1].Path {
			entries[j], entries[j-1] = entries[j-1], entries[j]
			j--
		}
	}
}

func safeRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	return clean != ".." && !strings.HasPrefix(clean, "../") && clean != "."
}

func safeJoin(root, rel string) (string, bool) {
	if !safeRel(rel) {
		return "", false
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	relBack, err := filepath.Rel(root, target)
	if err != nil || strings.HasPrefix(relBack, "..") {
		return "", false
	}
	return target, true
}

func safeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "folder"
	}
	return b.String()
}
