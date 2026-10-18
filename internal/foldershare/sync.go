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
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/link"
	"filesyncengine/internal/structuredlog"
	"filesyncengine/internal/transferprogress"
)

// Entry is one path in a folder's sync journal.
type Entry struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256,omitempty"`
	Size     int64  `json:"size"`
	Deleted  bool   `json:"deleted"`
	Revision int64  `json:"revision"`
	Origin   string `json:"origin,omitempty"`
}

type journalFile struct {
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
	data, err := os.ReadFile(JournalPath(configPath, folderID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var stored journalFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	return stored.Entries, nil
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
		if err := absorbLocal(folder.Path, origin, byPath); err != nil {
			return err
		}
		if err := saveJournal(configPath, folder.ID, byPath); err != nil {
			return err
		}
	}
	if folder.Mode == config.ModeSendOnly {
		return nil
	}
	for _, peer := range peersFor(cfg, folder) {
		remote, err := fetchRemoteJournal(peer, folder.ID)
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
	}
	return saveJournal(configPath, folder.ID, byPath)
}

func absorbLocal(root, origin string, byPath map[string]Entry) error {
	seen, err := scanFolder(root)
	if err != nil {
		return err
	}
	for path, file := range seen {
		current, ok := byPath[path]
		if ok && !current.Deleted && current.SHA256 == file.sha {
			continue
		}
		revision := int64(1)
		if ok && current.Revision >= revision {
			revision = current.Revision + 1
		}
		byPath[path] = Entry{Path: path, SHA256: file.sha, Size: file.size, Revision: revision, Origin: origin}
	}
	for path, current := range byPath {
		if current.Deleted {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		current.Deleted = true
		current.SHA256 = ""
		current.Size = 0
		current.Revision++
		current.Origin = origin
		byPath[path] = current
	}
	return nil
}

const (
	transferWorkers   = 4
	transferChunkSize = 4 << 20
)

// fileRetryBackoff is the wait before each retry of one file.
// Other files keep transferring while a file waits.
var fileRetryBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

func applyRemote(peer config.PeerConfig, folder config.FolderConfig, byPath map[string]Entry, remote []Entry) error {
	type transferJob struct {
		entry  Entry
		target string
	}
	var jobs []transferJob
	var mu sync.Mutex
	var applyErr error
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
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
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
	var wg sync.WaitGroup
	slots := make(chan struct{}, transferWorkers)
	var remainMu sync.Mutex
	note := func(delta, left int64) {
		transferprogress.Note(peer.ID, delta, left)
	}
	for _, job := range jobs {
		wg.Add(1)
		slots <- struct{}{}
		go func(job transferJob) {
			defer wg.Done()
			defer func() { <-slots }()
			var got int64
			err := downloadFile(peer, folder.ID, job.entry, job.target, func(n int64) {
				got += n
				remainMu.Lock()
				remaining -= n
				if remaining < 0 {
					remaining = 0
				}
				left := remaining
				remainMu.Unlock()
				note(n, left)
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
			byPath[job.entry.Path] = job.entry
		}(job)
	}
	wg.Wait()
	transferprogress.Note(peer.ID, 0, 0)
	return applyErr
}

type scanned struct {
	sha  string
	size int64
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
		if !safeRel(rel) || strings.HasPrefix(filepath.Base(rel), ".fse-") {
			return nil
		}
		hash, size, err := hashFile(path)
		if err != nil {
			return err
		}
		out[rel] = scanned{sha: hash, size: size}
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

func downloadFile(peer config.PeerConfig, folderID string, entry Entry, target string, progress func(int64)) error {
	var last error
	attempts := 1 + len(fileRetryBackoff)
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := fileRetryBackoff[attempt-1]
			structuredlog.Event("warn", "folder.sync.file_retry", "retrying file download", map[string]any{
				"folder":  folderID,
				"peer":    peer.ID,
				"path":    entry.Path,
				"attempt": attempt + 1,
				"error":   last.Error(),
			})
			time.Sleep(delay)
		}
		last = downloadFileOnce(peer, folderID, entry, target, progress)
		if last == nil {
			return nil
		}
	}
	return last
}

func downloadFileOnce(peer config.PeerConfig, folderID string, entry Entry, target string, progress func(int64)) error {
	if entry.Size < 0 {
		return fmt.Errorf("download %s: negative size", entry.Path)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".fse-sync-tmp"
	if entry.Size == 0 {
		if err := os.WriteFile(tmp, nil, 0o644); err != nil {
			return err
		}
		return finishDownload(tmp, target, entry)
	}
	offset, err := resumeOffset(tmp, entry.Size)
	if err != nil {
		return err
	}
	for offset < entry.Size {
		end := offset + transferChunkSize - 1
		if end >= entry.Size {
			end = entry.Size - 1
		}
		written, err := fetchRange(peer, folderID, entry.Path, tmp, offset, end, entry.Size)
		if err != nil {
			return err
		}
		if written <= 0 {
			return fmt.Errorf("download %s: empty response at byte %d", entry.Path, offset)
		}
		offset += written
		if progress != nil {
			progress(written)
		}
	}
	return finishDownload(tmp, target, entry)
}

func resumeOffset(tmp string, size int64) (int64, error) {
	info, err := os.Stat(tmp)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if info.Size() > size {
		if err := os.Remove(tmp); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return info.Size(), nil
}

func fetchRange(peer config.PeerConfig, folderID, rel, tmp string, offset, end, total int64) (int64, error) {
	var written int64
	_, err := link.Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		endpoint := base + "/v1/folder-file?folder=" + url.QueryEscape(folderID) + "&path=" + url.QueryEscape(rel)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-FSE-API-Key", peer.APIKey)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end))
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		full := resp.StatusCode == http.StatusOK && offset == 0
		if resp.StatusCode != http.StatusPartialContent && !full {
			return fmt.Errorf("download %s: %s", rel, resp.Status)
		}
		file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer file.Close()
		if full {
			if err := file.Truncate(0); err != nil {
				return err
			}
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		reader := io.Reader(resp.Body)
		if !full {
			reader = io.LimitReader(resp.Body, end-offset+1)
		}
		n, err := io.Copy(file, reader)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		written = n
		if full && total > 0 && n > total {
			return fmt.Errorf("download %s: received %d bytes, want %d", rel, n, total)
		}
		return nil
	})
	return written, err
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

func fetchRemoteJournal(peer config.PeerConfig, folderID string) ([]Entry, error) {
	var entries []Entry
	used, err := link.Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		endpoint := base + "/v1/sync-journal?folder=" + url.QueryEscape(folderID)
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
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return err
		}
		entries = body.Entries
		return nil
	})
	if err != nil {
		return nil, err
	}
	noteLinkDial(peer, used)
	return entries, nil
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
	entries := make([]Entry, 0, len(byPath))
	for _, entry := range byPath {
		entries = append(entries, entry)
	}
	// Stable order keeps the file quiet when nothing changed.
	sortEntries(entries)
	data, err := json.MarshalIndent(journalFile{Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	path := JournalPath(configPath, folderID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
