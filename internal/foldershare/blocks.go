package foldershare

import (
	"bytes"
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
	"sort"
	"strconv"
	"strings"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/link"
	"filesyncengine/internal/structuredlog"
)

const (
	// TransferBlockSize is the block stored in file metadata and requested
	// from peers. Sub-block hashes are never written to the journal.
	TransferBlockSize = 4 << 20
	// SubBlockSize is the in-flight slice used when a 4MB block differs and
	// the receiver still has the previous bytes for that block.
	SubBlockSize = 128 << 10
)

// FileBlock is one block in a file's metadata. Hash is lowercase hex SHA-256.
type FileBlock struct {
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Size   int    `json:"size"`
	Hash   string `json:"hash"`
}

type partialState struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	BlockSize int    `json:"blockSize"`
	Have      []int  `json:"have"`
}

type storedBlock struct {
	path   string
	offset int64
	size   int
}

type subHashList struct {
	Hashes []string `json:"hashes"`
}

type manifestBody struct {
	SHA256    string      `json:"sha256"`
	Size      int64       `json:"size"`
	BlockSize int         `json:"blockSize"`
	Blocks    []FileBlock `json:"blocks"`
}

// DescribeReader hashes r and returns its transfer block list.
func DescribeReader(r io.Reader, size int64) (string, int64, []FileBlock, error) {
	return blocksOf(r, size)
}

// InspectFile hashes path and returns its transfer block list.
func InspectFile(path string) (string, int64, []FileBlock, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, nil, err
	}
	return blocksOf(file, info.Size())
}

func blocksOf(r io.Reader, size int64) (string, int64, []FileBlock, error) {
	if size < 0 {
		return "", 0, nil, fmt.Errorf("negative size")
	}
	fileHash := sha256.New()
	if size == 0 {
		return hex.EncodeToString(fileHash.Sum(nil)), 0, nil, nil
	}
	buf := make([]byte, TransferBlockSize)
	var blocks []FileBlock
	var read int64
	for index := 0; read < size; index++ {
		want := TransferBlockSize
		if remain := size - read; remain < int64(want) {
			want = int(remain)
		}
		n, err := io.ReadFull(r, buf[:want])
		if err != nil {
			return "", 0, nil, err
		}
		_, _ = fileHash.Write(buf[:n])
		sum := sha256.Sum256(buf[:n])
		blocks = append(blocks, FileBlock{
			Index:  index,
			Offset: read,
			Size:   n,
			Hash:   hex.EncodeToString(sum[:]),
		})
		read += int64(n)
	}
	return hex.EncodeToString(fileHash.Sum(nil)), read, blocks, nil
}

func blocksFromBytes(payload []byte) []FileBlock {
	_, _, blocks, err := blocksOf(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil
	}
	return blocks
}

// SubHashes splits data into subSize pieces and returns each SHA-256 in hex.
// Callers compute this on the fly. The results are not stored.
func SubHashes(data []byte, subSize int) []string {
	hashes, err := HashSubs(bytes.NewReader(data), int64(len(data)), subSize)
	if err != nil {
		return nil
	}
	return hashes
}

// HashSubs hashes n bytes from r in subSize pieces using a fixed read buffer.
// The hashes are for the in-flight response only.
func HashSubs(r io.Reader, n int64, subSize int) ([]string, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative length")
	}
	if n == 0 {
		return nil, nil
	}
	if subSize <= 0 {
		subSize = SubBlockSize
	}
	buf := make([]byte, 32<<10)
	var out []string
	remain := n
	for remain > 0 {
		take := int64(subSize)
		if take > remain {
			take = remain
		}
		hash := sha256.New()
		left := take
		for left > 0 {
			chunk := buf
			if int64(len(chunk)) > left {
				chunk = buf[:left]
			}
			if _, err := io.ReadFull(r, chunk); err != nil {
				return nil, err
			}
			_, _ = hash.Write(chunk)
			left -= int64(len(chunk))
		}
		out = append(out, hex.EncodeToString(hash.Sum(nil)))
		remain -= take
	}
	return out, nil
}

// preferSubRanges reports whether fetching the differing sub-ranges moves
// little enough data to beat one whole-block transfer. The cutoff is a
// quarter of the block: a few changed slices are a clear win, and a dense
// edit stays a single read so the only extra cost is the in-flight hash list.
func preferSubRanges(diffBytes, blockSize, ranges int) bool {
	if ranges <= 0 || diffBytes <= 0 || blockSize <= 0 || diffBytes >= blockSize {
		return false
	}
	return diffBytes*4 <= blockSize
}

func indexLocalBlocks(root string, byPath map[string]Entry) map[string]storedBlock {
	out := make(map[string]storedBlock)
	for rel, entry := range byPath {
		if entry.Deleted || len(entry.Blocks) == 0 {
			continue
		}
		target, ok := safeJoin(root, rel)
		if !ok {
			continue
		}
		for _, block := range entry.Blocks {
			key := block.Hash + ":" + strconv.Itoa(block.Size)
			if _, exists := out[key]; exists {
				continue
			}
			out[key] = storedBlock{path: target, offset: block.Offset, size: block.Size}
		}
	}
	return out
}

func downloadFile(peer config.PeerConfig, folder config.FolderConfig, entry Entry, target string, locals map[string]storedBlock, progress func(network, completed int64)) (int, int, error) {
	var last error
	attempts := 1 + len(fileRetryBackoff)
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := fileRetryBackoff[attempt-1]
			structuredlog.Event("warn", "folder.sync.file_retry", "retrying file download", map[string]any{
				"folder":  folder.ID,
				"peer":    peer.ID,
				"path":    entry.Path,
				"attempt": attempt + 1,
				"error":   last.Error(),
			})
			time.Sleep(delay)
		}
		fetched, reused, err := downloadFileOnce(peer, folder.ID, entry, target, locals, progress)
		if err == nil {
			return fetched, reused, nil
		}
		last = err
	}
	return 0, 0, last
}

func downloadFileOnce(peer config.PeerConfig, folderID string, entry Entry, target string, locals map[string]storedBlock, progress func(network, completed int64)) (int, int, error) {
	fail := func(err error) (int, int, error) { return 0, 0, err }
	if entry.Size < 0 {
		return fail(fmt.Errorf("download %s: negative size", entry.Path))
	}
	ready, err := ensureBlocks(peer, folderID, entry)
	if err != nil {
		return fail(err)
	}
	entry = ready
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fail(err)
	}
	tmp := target + ".fse-sync-tmp"
	statePath := target + ".fse-sync-state"
	if entry.Size == 0 {
		if err := os.WriteFile(tmp, nil, 0o644); err != nil {
			return fail(err)
		}
		if err := finishDownload(tmp, target, entry); err != nil {
			return fail(err)
		}
		_ = os.Remove(statePath)
		return 0, 0, nil
	}
	if err := validateBlocks(entry); err != nil {
		return fail(err)
	}
	have := loadPartial(statePath, tmp, entry)
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fail(err)
	}
	defer file.Close()
	if len(have) == 0 {
		if err := file.Truncate(0); err != nil {
			return fail(err)
		}
	}
	oldPath := ""
	if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() {
		oldPath = target
	}
	var failed error
	var fetched, reused int
	completed := int64(0)
	emit := func(network, done int64) {
		if progress != nil {
			progress(network, done)
		}
	}
	for _, block := range entry.Blocks {
		if _, ok := have[block.Index]; ok {
			reused++
			completed += int64(block.Size)
			continue
		}
		data, network, fetchErr := fetchBlock(peer, folderID, entry.Path, block, oldPath, locals)
		if fetchErr != nil {
			failed = fetchErr
			continue
		}
		if err := writeVerified(file, block, data); err != nil {
			failed = err
			continue
		}
		have[block.Index] = struct{}{}
		if err := savePartial(statePath, entry, have); err != nil {
			return fail(err)
		}
		if network == 0 {
			reused++
		} else {
			fetched++
		}
		completed += int64(block.Size)
		emit(network, completed)
	}
	emit(0, completed)
	if failed != nil {
		return fail(failed)
	}
	if err := file.Close(); err != nil {
		return fail(err)
	}
	if err := finishDownload(tmp, target, entry); err != nil {
		_ = os.Remove(statePath)
		return fail(err)
	}
	_ = os.Remove(statePath)
	return fetched, reused, nil
}

func ensureBlocks(peer config.PeerConfig, folderID string, entry Entry) (Entry, error) {
	if entry.Size == 0 || len(entry.Blocks) > 0 {
		return entry, nil
	}
	body, err := peerBytes(peer, "/v1/folder-manifest?folder="+url.QueryEscape(folderID)+"&path="+url.QueryEscape(entry.Path), 64<<20)
	if err != nil {
		return entry, err
	}
	var manifest manifestBody
	if err := json.Unmarshal(body, &manifest); err != nil {
		return entry, err
	}
	if manifest.BlockSize != TransferBlockSize || manifest.Size != entry.Size {
		return entry, fmt.Errorf("download %s: manifest does not match journal", entry.Path)
	}
	if entry.SHA256 != "" && !strings.EqualFold(manifest.SHA256, entry.SHA256) {
		return entry, fmt.Errorf("download %s: manifest checksum mismatch", entry.Path)
	}
	entry.Blocks = manifest.Blocks
	return entry, nil
}

func validateBlocks(entry Entry) error {
	if len(entry.Blocks) == 0 {
		return fmt.Errorf("download %s: missing block list", entry.Path)
	}
	var covered int64
	for i, block := range entry.Blocks {
		if block.Index != i || block.Offset != covered || block.Size <= 0 || block.Size > TransferBlockSize {
			return fmt.Errorf("download %s: block %d is not a %d-byte transfer block", entry.Path, block.Index, TransferBlockSize)
		}
		if i < len(entry.Blocks)-1 && block.Size != TransferBlockSize {
			return fmt.Errorf("download %s: short block %d is not last", entry.Path, block.Index)
		}
		if len(block.Hash) != 64 {
			return fmt.Errorf("download %s: block %d hash is not sha256", entry.Path, block.Index)
		}
		covered += int64(block.Size)
	}
	if covered != entry.Size {
		return fmt.Errorf("download %s: blocks cover %d bytes, want %d", entry.Path, covered, entry.Size)
	}
	return nil
}

func fetchBlock(peer config.PeerConfig, folderID, rel string, block FileBlock, oldPath string, locals map[string]storedBlock) ([]byte, int64, error) {
	if src, ok := locals[block.Hash+":"+strconv.Itoa(block.Size)]; ok {
		data, err := readFileRange(src.path, src.offset, src.size)
		if err == nil && hashEquals(data, block.Hash) {
			return data, 0, nil
		}
	}
	if data, network, ok := trySubBlock(peer, folderID, rel, block, oldPath); ok {
		return data, network, nil
	}
	data, err := fetchWholeBlock(peer, folderID, rel, block)
	if err != nil {
		return nil, 0, err
	}
	return data, int64(len(data)), nil
}

func trySubBlock(peer config.PeerConfig, folderID, rel string, block FileBlock, oldPath string) ([]byte, int64, bool) {
	if oldPath == "" || block.Size <= SubBlockSize {
		return nil, 0, false
	}
	old, err := readFileRange(oldPath, block.Offset, block.Size)
	if err != nil || len(old) != block.Size || hashEquals(old, block.Hash) {
		if err == nil && hashEquals(old, block.Hash) {
			return old, 0, true
		}
		return nil, 0, false
	}
	localSubs := SubHashes(old, SubBlockSize)
	remoteSubs, err := fetchSubHashes(peer, folderID, rel, block)
	if err != nil || len(remoteSubs) != len(localSubs) {
		return nil, 0, false
	}
	diff := make([]int, 0)
	diffBytes := 0
	for i := range remoteSubs {
		if strings.EqualFold(localSubs[i], remoteSubs[i]) {
			continue
		}
		diff = append(diff, i)
		diffBytes += subLength(block.Size, i)
	}
	if !preferSubRanges(diffBytes, block.Size, len(diff)) {
		return nil, 0, false
	}
	buf := append([]byte(nil), old...)
	moved := 0
	for _, i := range diff {
		piece, err := fetchSubRange(peer, folderID, rel, block, i)
		if err != nil || len(piece) != subLength(block.Size, i) {
			return nil, 0, false
		}
		sum := sha256.Sum256(piece)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), remoteSubs[i]) {
			return nil, 0, false
		}
		copy(buf[i*SubBlockSize:], piece)
		moved += len(piece)
	}
	if !hashEquals(buf, block.Hash) {
		return nil, 0, false
	}
	return buf, int64(moved), true
}

func subLength(blockSize, index int) int {
	offset := index * SubBlockSize
	remain := blockSize - offset
	if remain < SubBlockSize {
		return remain
	}
	return SubBlockSize
}

func fetchWholeBlock(peer config.PeerConfig, folderID, rel string, block FileBlock) ([]byte, error) {
	body, err := peerBytes(peer, blockURL(folderID, rel, block, -1), int64(block.Size))
	if err != nil {
		return nil, err
	}
	if !hashEquals(body, block.Hash) {
		return nil, fmt.Errorf("download %s: block %d checksum mismatch", rel, block.Index)
	}
	return body, nil
}

func fetchSubHashes(peer config.PeerConfig, folderID, rel string, block FileBlock) ([]string, error) {
	endpoint := "/v1/folder-block-subhashes?folder=" + url.QueryEscape(folderID) +
		"&path=" + url.QueryEscape(rel) +
		"&index=" + strconv.Itoa(block.Index) +
		"&blockSize=" + strconv.Itoa(TransferBlockSize) +
		"&subSize=" + strconv.Itoa(SubBlockSize)
	body, err := peerBytes(peer, endpoint, 1<<20)
	if err != nil {
		return nil, err
	}
	var list subHashList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	return list.Hashes, nil
}

func fetchSubRange(peer config.PeerConfig, folderID, rel string, block FileBlock, sub int) ([]byte, error) {
	return peerBytes(peer, blockURL(folderID, rel, block, sub), int64(subLength(block.Size, sub)))
}

func blockURL(folderID, rel string, block FileBlock, sub int) string {
	endpoint := "/v1/folder-block?folder=" + url.QueryEscape(folderID) +
		"&path=" + url.QueryEscape(rel) +
		"&index=" + strconv.Itoa(block.Index) +
		"&blockSize=" + strconv.Itoa(TransferBlockSize)
	if sub >= 0 {
		endpoint += "&sub=" + strconv.Itoa(sub) + "&subSize=" + strconv.Itoa(SubBlockSize)
	}
	return endpoint
}

func peerBytes(peer config.PeerConfig, endpoint string, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("negative read limit")
	}
	var body []byte
	_, err := link.Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+endpoint, nil)
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
			return fmt.Errorf("GET %s: %s", endpoint, resp.Status)
		}
		read, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return err
		}
		if int64(len(read)) > limit {
			return fmt.Errorf("GET %s: response exceeds %d bytes", endpoint, limit)
		}
		body = read
		return nil
	})
	return body, err
}

func writeVerified(dst *os.File, block FileBlock, data []byte) error {
	if !hashEquals(data, block.Hash) {
		return fmt.Errorf("block %d checksum mismatch", block.Index)
	}
	if _, err := dst.WriteAt(data, block.Offset); err != nil {
		return err
	}
	return dst.Sync()
}

func hashEquals(data []byte, want string) bool {
	if len(want) != 64 {
		return false
	}
	sum := sha256.Sum256(data)
	return strings.EqualFold(hex.EncodeToString(sum[:]), want)
}

func readFileRange(path string, offset int64, size int) ([]byte, error) {
	if size < 0 {
		return nil, fmt.Errorf("negative range")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buf := make([]byte, size)
	if _, err := io.ReadFull(io.NewSectionReader(file, offset, int64(size)), buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func loadPartial(statePath, tmp string, entry Entry) map[int]struct{} {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return map[int]struct{}{}
	}
	var stored partialState
	if err := json.Unmarshal(data, &stored); err != nil || stored.BlockSize != TransferBlockSize || stored.Size != entry.Size || !strings.EqualFold(stored.SHA256, entry.SHA256) {
		_ = os.Remove(statePath)
		return map[int]struct{}{}
	}
	claimed := make(map[int]struct{}, len(stored.Have))
	for _, index := range stored.Have {
		claimed[index] = struct{}{}
	}
	return confirmPartial(tmp, entry.Blocks, claimed)
}

func confirmPartial(tmp string, blocks []FileBlock, claimed map[int]struct{}) map[int]struct{} {
	if len(claimed) == 0 {
		return map[int]struct{}{}
	}
	file, err := os.Open(tmp)
	if err != nil {
		return map[int]struct{}{}
	}
	defer file.Close()
	out := make(map[int]struct{})
	for _, block := range blocks {
		if _, ok := claimed[block.Index]; !ok {
			continue
		}
		buf := make([]byte, block.Size)
		n, err := file.ReadAt(buf, block.Offset)
		if err != nil || n != block.Size || !hashEquals(buf, block.Hash) {
			continue
		}
		out[block.Index] = struct{}{}
	}
	return out
}

func savePartial(path string, entry Entry, have map[int]struct{}) error {
	indexes := make([]int, 0, len(have))
	for index := range have {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	stored := partialState{SHA256: entry.SHA256, Size: entry.Size, BlockSize: TransferBlockSize, Have: indexes}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
