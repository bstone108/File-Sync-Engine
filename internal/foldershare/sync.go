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
			fmt.Fprintf(os.Stderr, "folder sync %s: %v\n", folder.ID, err)
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
			fmt.Fprintf(os.Stderr, "folder sync %s peer %s: %v\n", folder.ID, peer.ID, err)
			continue
		}
		if err := applyRemote(peer, folder, byPath, remote); err != nil {
			fmt.Fprintf(os.Stderr, "folder sync %s apply %s: %v\n", folder.ID, peer.ID, err)
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

func applyRemote(peer config.PeerConfig, folder config.FolderConfig, byPath map[string]Entry, remote []Entry) error {
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
				return err
			}
			byPath[entry.Path] = entry
			continue
		}
		if err := downloadFile(peer, folder.ID, entry, target); err != nil {
			return err
		}
		byPath[entry.Path] = entry
	}
	return nil
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

func downloadFile(peer config.PeerConfig, folderID string, entry Entry, target string) error {
	_, err := link.Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		endpoint := base + "/v1/folder-file?folder=" + url.QueryEscape(folderID) + "&path=" + url.QueryEscape(entry.Path)
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
			return fmt.Errorf("download %s: %s", entry.Path, resp.Status)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		tmp := target + ".fse-sync-tmp"
		file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		hash := sha256.New()
		if _, err := io.Copy(io.MultiWriter(file, hash), resp.Body); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return err
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		sum := hex.EncodeToString(hash.Sum(nil))
		if entry.SHA256 != "" && !strings.EqualFold(sum, entry.SHA256) {
			_ = os.Remove(tmp)
			return fmt.Errorf("downloaded %s checksum mismatch", entry.Path)
		}
		return os.Rename(tmp, target)
	})
	return err
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
