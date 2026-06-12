package peersync

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/ratelimit"
	"filesyncengine/internal/recovery"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/scanner"
)

type Result struct {
	Writes                int
	Deletes               int
	FilesMoved            int
	BlocksFetched         int
	BlocksReused          int
	MissingIgnoreIncludes []string
}

type PullOptions struct {
	ReceiveBytesPerSecond int64
	BlockSources          []BlockSource
}

type BlockSource struct {
	PeerID         string
	BaseURL        string
	APIKey         string
	Path           routing.PathKind
	Network        routing.NetworkKind
	RelayViaPeerID string
	Reachable      bool
}

type localBlock struct {
	Path  string
	Block scannerBlock
}

type scannerBlock struct {
	Index  int
	Offset int64
	Size   int
	Hash   []byte
}

func PullFolder(baseURL, apiKey, folderID, localRoot string) (Result, error) {
	return PullFolderWithOptions(baseURL, apiKey, folderID, localRoot, PullOptions{})
}

func PullFolderWithOptions(baseURL, apiKey, folderID, localRoot string, opts PullOptions) (Result, error) {
	remote, err := fetchIndex(baseURL, apiKey, folderID)
	if err != nil {
		return Result{}, err
	}
	if _, err := recovery.CleanFolder(localRoot); err != nil {
		return Result{}, err
	}
	missingIncludes, err := fetchMissingInShareIgnoreIncludes(baseURL, apiKey, folderID, localRoot)
	if err != nil {
		return Result{}, err
	}
	ignoreMatcher, err := scanner.LoadSyncIgnoreMatcher(localRoot)
	if err != nil {
		return Result{}, err
	}
	local, err := scanner.ScanFolder(localRoot, scanner.Options{BlockSize: config.DefaultBlockSize})
	if os.IsNotExist(err) {
		if err := os.MkdirAll(localRoot, 0o755); err != nil {
			return Result{}, err
		}
		local = scanner.Result{Root: localRoot}
	} else if err != nil {
		return Result{}, err
	}
	remoteByPath := byPath(remote.Files)
	result := Result{MissingIgnoreIncludes: missingIncludes}
	receiveLimiter := ratelimit.NewLimiter(opts.ReceiveBytesPerSecond)
	moveRemoteFiles := make([]scanner.File, 0, len(remote.Files))
	for _, file := range remote.Files {
		if !ignoredForPeerPull(ignoreMatcher, file.RelativePath) {
			moveRemoteFiles = append(moveRemoteFiles, file)
		}
	}
	moved, err := moveMatchingStaleLocalFiles(localRoot, moveRemoteFiles, config.DefaultBlockSize)
	if err != nil {
		return result, err
	}
	result.FilesMoved += moved
	if moved > 0 {
		local, err = scanner.ScanFolder(localRoot, scanner.Options{BlockSize: config.DefaultBlockSize})
		if err != nil {
			return result, err
		}
	}
	localByPath := byPath(local.Files)
	localBlocks := blockIndex(localRoot, local.Files)
	for _, rel := range sortedKeys(remoteByPath) {
		if ignoredForPeerPull(ignoreMatcher, rel) {
			continue
		}
		remoteFile := remoteByPath[rel]
		if localFile, ok := localByPath[rel]; ok && manifestEqual(remoteFile, localFile) {
			continue
		}
		targetPath, ok := safeLocalPath(localRoot, rel)
		if !ok {
			return result, fmt.Errorf("peer index contains unsafe path %q", rel)
		}
		blocksFetched, blocksReused, err := downloadFileByBlocks(baseURL, apiKey, folderID, rel, remoteFile, localRoot, targetPath, localBlocks, receiveLimiter, opts.BlockSources)
		if err != nil {
			return result, err
		}
		result.Writes++
		result.BlocksFetched += blocksFetched
		result.BlocksReused += blocksReused
	}
	for _, rel := range sortedKeys(localByPath) {
		if ignoredForPeerPull(ignoreMatcher, rel) {
			continue
		}
		if _, ok := remoteByPath[rel]; ok {
			continue
		}
		targetPath, ok := safeLocalPath(localRoot, rel)
		if !ok {
			return result, fmt.Errorf("local index contains unsafe path %q", rel)
		}
		if _, _, err := backup.RetainExistingBackupIntakeFile(localRoot, rel, time.Time{}); err != nil {
			return result, err
		}
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return result, err
		}
		result.Deletes++
	}
	return result, nil
}

func fetchMissingInShareIgnoreIncludes(baseURL, apiKey, folderID, localRoot string) ([]string, error) {
	for {
		missing, err := scanner.MissingInShareSyncIgnoreIncludes(localRoot)
		if err != nil {
			return nil, err
		}
		if len(missing) == 0 {
			return nil, nil
		}
		fetchedAny := false
		for _, rel := range missing {
			fetched, err := downloadIncludeFile(baseURL, apiKey, folderID, rel, localRoot)
			if err != nil {
				return nil, err
			}
			fetchedAny = fetchedAny || fetched
		}
		if !fetchedAny {
			return missing, nil
		}
	}
}

func downloadIncludeFile(baseURL, apiKey, folderID, rel, localRoot string) (bool, error) {
	targetPath, ok := safeLocalPath(localRoot, rel)
	if !ok {
		return false, fmt.Errorf("ignore include path is unsafe %q", rel)
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/folder-file?folder=" + url.QueryEscape(folderID) + "&path=" + url.QueryEscape(rel)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-FSE-API-Key", apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetch ignore include %s: %s", rel, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return false, err
	}
	tmp := targetPath + ".fse-peer-tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return false, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return false, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, os.Rename(tmp, targetPath)
}

func ignoredForPeerPull(matcher scanner.SyncIgnoreMatcher, rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == ".sync" || strings.HasPrefix(rel, ".sync/") || matcher.IsIgnored(rel)
}

func safeLocalPath(root, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	cleanPath, err := filepath.Abs(filepath.Join(cleanRoot, cleanRel))
	if err != nil {
		return "", false
	}
	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
		return "", false
	}
	return cleanPath, true
}

func fetchIndex(baseURL, apiKey, folderID string) (scanner.Result, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/folder-index?folder=" + url.QueryEscape(folderID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return scanner.Result{}, err
	}
	req.Header.Set("X-FSE-API-Key", apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return scanner.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return scanner.Result{}, fmt.Errorf("fetch index: %s", resp.Status)
	}
	var result scanner.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return scanner.Result{}, err
	}
	return result, nil
}

func downloadFileByBlocks(baseURL, apiKey, folderID, rel string, remote scanner.File, localRoot string, targetPath string, localBlocks map[string]localBlock, receiveLimiter *ratelimit.Limiter, blockSources []BlockSource) (int, int, error) {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return 0, 0, err
	}
	tmp := targetPath + ".fse-peer-tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, 0, err
	}
	fetched := 0
	reused := 0
	for _, block := range remote.Manifest.Blocks {
		if source, ok := localBlocks[blockKey(block.Size, block.Hash)]; ok {
			if err := copyLocalBlock(file, source, block.Offset); err != nil {
				_ = file.Close()
				_ = os.Remove(tmp)
				return fetched, reused, err
			}
			reused++
			continue
		}
		data, err := downloadBlockFromBestSource(baseURL, apiKey, folderID, rel, block.Index, remote.Manifest.BlockSize, blockSources)
		if err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fetched, reused, err
		}
		if len(data) != block.Size {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fetched, reused, fmt.Errorf("downloaded block %d size=%d want=%d", block.Index, len(data), block.Size)
		}
		h := sha256.Sum256(data)
		if !bytes.Equal(h[:], block.Hash) {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fetched, reused, fmt.Errorf("downloaded block %d failed hash verification", block.Index)
		}
		if err := receiveLimiter.Wait(nil, len(data)); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fetched, reused, err
		}
		if _, err := file.Seek(block.Offset, io.SeekStart); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fetched, reused, err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fetched, reused, err
		}
		fetched++
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fetched, reused, err
	}
	if err := file.Close(); err != nil {
		return fetched, reused, err
	}
	if err := verifyDownloadedManifest(tmp, remote); err != nil {
		_ = os.Remove(tmp)
		return fetched, reused, err
	}
	if _, _, err := backup.RetainExistingBackupIntakeFile(localRoot, rel, time.Time{}); err != nil {
		_ = os.Remove(tmp)
		return fetched, reused, err
	}
	return fetched, reused, os.Rename(tmp, targetPath)
}

func copyLocalBlock(dst *os.File, source localBlock, targetOffset int64) error {
	in, err := os.Open(source.Path)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := in.Seek(source.Block.Offset, io.SeekStart); err != nil {
		return err
	}
	if _, err := dst.Seek(targetOffset, io.SeekStart); err != nil {
		return err
	}
	_, err = io.CopyN(dst, in, int64(source.Block.Size))
	return err
}

func downloadBlockFromBestSource(baseURL, apiKey, folderID, rel string, index, blockSize int, blockSources []BlockSource) ([]byte, error) {
	source := chooseBlockSource(baseURL, apiKey, rel, index, blockSources)
	return downloadBlock(source.BaseURL, source.APIKey, folderID, rel, index, blockSize)
}

func chooseBlockSource(baseURL, apiKey, rel string, index int, blockSources []BlockSource) BlockSource {
	if len(blockSources) == 0 {
		return BlockSource{BaseURL: baseURL, APIKey: apiKey, Path: routing.RelayPath, Network: routing.WANNetwork, Reachable: true}
	}
	contentID := fmt.Sprintf("%s:%d", rel, index)
	candidates := make([]routing.CandidateSource, 0, len(blockSources))
	byKey := make(map[string]BlockSource, len(blockSources))
	for i, source := range blockSources {
		if source.BaseURL == "" {
			continue
		}
		reachable := source.Reachable
		if !source.Reachable && source.Path == "" {
			reachable = true
		}
		candidate := routing.CandidateSource{PeerID: source.PeerID, ContentID: contentID, Path: source.Path, Network: source.Network, RelayViaPeerID: source.RelayViaPeerID, Reachable: reachable}
		if candidate.PeerID == "" {
			candidate.PeerID = fmt.Sprintf("source-%06d", i)
		}
		if candidate.Path == "" {
			candidate.Path = routing.RelayPath
		}
		candidates = append(candidates, candidate)
		byKey[candidate.PeerID] = source
	}
	choice, ok := routing.ChooseTransferSource(routing.SourceSelectionRequest{ContentID: contentID, Candidates: candidates})
	if !ok {
		return BlockSource{BaseURL: baseURL, APIKey: apiKey, Path: routing.RelayPath, Network: routing.WANNetwork, Reachable: true}
	}
	selected := byKey[choice.PeerID]
	if selected.APIKey == "" {
		selected.APIKey = apiKey
	}
	return selected
}

func downloadBlock(baseURL, apiKey, folderID, rel string, index, blockSize int) ([]byte, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/folder-block?folder=" + url.QueryEscape(folderID) + "&path=" + url.QueryEscape(rel) + "&index=" + url.QueryEscape(fmt.Sprint(index)) + "&blockSize=" + url.QueryEscape(fmt.Sprint(blockSize))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-FSE-API-Key", apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download block %s[%d]: %s", rel, index, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func verifyDownloadedManifest(path string, remote scanner.File) error {
	got, err := scanner.ScanFolder(filepath.Dir(path), scanner.Options{BlockSize: remote.Manifest.BlockSize})
	if err != nil {
		return err
	}
	base := filepath.Base(path)
	for _, file := range got.Files {
		if file.RelativePath == base && manifestEqual(file, remote) {
			return nil
		}
	}
	return fmt.Errorf("downloaded file failed manifest verification")
}

func byPath(files []scanner.File) map[string]scanner.File {
	out := make(map[string]scanner.File, len(files))
	for _, file := range files {
		out[file.RelativePath] = file
	}
	return out
}

func blockIndex(root string, files []scanner.File) map[string]localBlock {
	index := make(map[string]localBlock)
	for _, file := range files {
		path, ok := safeLocalPath(root, file.RelativePath)
		if !ok {
			continue
		}
		for _, block := range file.Manifest.Blocks {
			key := blockKey(block.Size, block.Hash)
			if _, exists := index[key]; exists {
				continue
			}
			index[key] = localBlock{Path: path, Block: scannerBlock{Index: block.Index, Offset: block.Offset, Size: block.Size, Hash: block.Hash}}
		}
	}
	return index
}

func blockKey(size int, hash []byte) string {
	return fmt.Sprintf("%d:%x", size, hash)
}

func moveMatchingStaleLocalFiles(root string, remoteFiles []scanner.File, blockSize int) (int, error) {
	if blockSize <= 0 || len(remoteFiles) == 0 {
		return 0, nil
	}
	local, err := scanner.ScanFolder(root, scanner.Options{BlockSize: blockSize})
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	remoteByPath := make(map[string]scanner.File, len(remoteFiles))
	remotePaths := make([]string, 0, len(remoteFiles))
	for _, file := range remoteFiles {
		remoteByPath[file.RelativePath] = file
		remotePaths = append(remotePaths, file.RelativePath)
	}
	sort.Strings(remotePaths)
	localByPath := byPath(local.Files)
	candidates := make(map[string][]string)
	for _, file := range local.Files {
		if _, stillPresent := remoteByPath[file.RelativePath]; stillPresent {
			continue
		}
		key := manifestKey(file.Manifest)
		if key == "" {
			continue
		}
		candidates[key] = append(candidates[key], file.RelativePath)
	}
	for key := range candidates {
		sort.Strings(candidates[key])
	}
	moved := 0
	for _, rel := range remotePaths {
		remote := remoteByPath[rel]
		finalPath, ok := safeLocalPath(root, rel)
		if !ok {
			return moved, fmt.Errorf("peer index contains unsafe path %q", rel)
		}
		if localFile, ok := localByPath[rel]; ok && manifestEqual(remote, localFile) {
			continue
		}
		if _, err := os.Stat(finalPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return moved, err
		}
		moveRel, ok := takeMoveCandidate(candidates, remote.Manifest)
		if !ok {
			continue
		}
		sourcePath, ok := safeLocalPath(root, moveRel)
		if !ok {
			return moved, fmt.Errorf("local index contains unsafe path %q", moveRel)
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
			return moved, err
		}
		if err := os.Rename(sourcePath, finalPath); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

func takeMoveCandidate(candidates map[string][]string, manifest block.Manifest) (string, bool) {
	key := manifestKey(manifest)
	paths := candidates[key]
	if len(paths) == 0 {
		return "", false
	}
	path := paths[0]
	if len(paths) == 1 {
		delete(candidates, key)
	} else {
		candidates[key] = paths[1:]
	}
	return path, true
}

func manifestKey(manifest block.Manifest) string {
	if manifest.Size == 0 && len(manifest.Blocks) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%d", manifest.Size))
	for _, b := range manifest.Blocks {
		builder.WriteString(fmt.Sprintf("|%d:%x", b.Size, b.Hash))
	}
	return builder.String()
}

func sortedKeys(files map[string]scanner.File) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func manifestEqual(a, b scanner.File) bool {
	if a.Manifest.Size != b.Manifest.Size || len(a.Manifest.Blocks) != len(b.Manifest.Blocks) {
		return false
	}
	for i := range a.Manifest.Blocks {
		ab := a.Manifest.Blocks[i]
		bb := b.Manifest.Blocks[i]
		if ab.Size != bb.Size || string(ab.Hash) != string(bb.Hash) {
			return false
		}
	}
	return true
}
