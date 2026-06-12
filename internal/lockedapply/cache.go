package lockedapply

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

type Store interface {
	SavePendingWrite(write state.PendingWrite) error
}

type RetryStore interface {
	PendingWrite(folderID string, path string) (state.PendingWrite, bool, error)
	MarkPendingWriteCommitted(folderID string, path string) error
}

type BlockData struct {
	Index int
	Data  []byte
}

type PersistOptions struct {
	Root                 string
	Store                Store
	FolderID             string
	Path                 string
	Manifest             block.Manifest
	ExpectedBaseManifest *block.Manifest
	Blocks               []BlockData
	Reason               string
}

type CachedApply struct {
	Path   string
	Blocks int
}

func Persist(opts PersistOptions) (CachedApply, error) {
	if opts.Store == nil {
		return CachedApply{}, fmt.Errorf("store required")
	}
	finalPath, err := safeRootPath(opts.Root, opts.Path)
	if err != nil {
		return CachedApply{}, err
	}
	if opts.FolderID == "" {
		return CachedApply{}, fmt.Errorf("folder id required")
	}
	verified, byIndex, err := verifyBlocks(opts.Manifest, opts.Blocks)
	if err != nil {
		return CachedApply{}, err
	}
	cachePath := cacheDir(opts.Root, opts.Path)
	if err := os.MkdirAll(cachePath, 0o700); err != nil {
		return CachedApply{}, err
	}
	written := 0
	for _, blockRef := range verified {
		data := byIndex[blockRef.Index]
		path := filepath.Join(cachePath, fmt.Sprintf("block-%06d", blockRef.Index))
		if err := writeBlockFile(path, data); err != nil {
			return CachedApply{}, err
		}
		written++
	}
	write := state.PendingWrite{
		FolderID:             opts.FolderID,
		Path:                 opts.Path,
		Manifest:             opts.Manifest,
		ExpectedBaseManifest: opts.ExpectedBaseManifest,
		VerifiedBlocks:       verified,
		Reason:               opts.Reason,
	}
	if write.Manifest.Path == "" {
		write.Manifest.Path = opts.Path
	}
	if err := opts.Store.SavePendingWrite(write); err != nil {
		return CachedApply{}, err
	}
	_ = finalPath
	return CachedApply{Path: cachePath, Blocks: written}, nil
}

type RetryOptions struct {
	Root     string
	Store    RetryStore
	FolderID string
	Path     string
}

type RetryResult struct {
	Applied         bool
	BaseMismatch    bool
	ConflictCreated bool
	Missing         bool
}

func RetryPending(opts RetryOptions) (RetryResult, error) {
	if opts.Store == nil {
		return RetryResult{}, fmt.Errorf("store required")
	}
	finalPath, err := safeRootPath(opts.Root, opts.Path)
	if err != nil {
		return RetryResult{}, err
	}
	write, ok, err := opts.Store.PendingWrite(opts.FolderID, opts.Path)
	if err != nil {
		return RetryResult{}, err
	}
	if !ok || write.Committed {
		return RetryResult{Missing: !ok}, nil
	}
	if write.ExpectedBaseManifest == nil {
		return RetryResult{BaseMismatch: true}, nil
	}
	current, err := block.BuildManifest(finalPath, write.ExpectedBaseManifest.BlockSize)
	if err != nil {
		if os.IsNotExist(err) {
			return RetryResult{BaseMismatch: true}, nil
		}
		return RetryResult{}, err
	}
	current.Path = opts.Path
	conflictCreated := false
	if !manifestsEqualBlocks(current, *write.ExpectedBaseManifest) {
		conflictPath := nextConflictPath(finalPath, ".sync-conflict-locked-apply")
		if err := copyFileAtomic(finalPath, conflictPath); err != nil {
			return RetryResult{}, err
		}
		conflictCreated = true
	}
	if err := assembleCachedBlocks(opts.Root, write); err != nil {
		return RetryResult{}, err
	}
	if err := opts.Store.MarkPendingWriteCommitted(opts.FolderID, opts.Path); err != nil {
		return RetryResult{}, err
	}
	return RetryResult{Applied: true, ConflictCreated: conflictCreated}, nil
}

func nextConflictPath(finalPath string, suffix string) string {
	dir := filepath.Dir(finalPath)
	ext := filepath.Ext(finalPath)
	base := strings.TrimSuffix(filepath.Base(finalPath), ext)
	for i := 0; ; i++ {
		candidateSuffix := suffix
		if i > 0 {
			candidateSuffix = fmt.Sprintf("%s-%d", suffix, i)
		}
		candidate := filepath.Join(dir, base+candidateSuffix+ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func copyFileAtomic(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + fmt.Sprintf(".%d.tmp", os.Getpid())
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func assembleCachedBlocks(root string, write state.PendingWrite) error {
	finalPath, err := safeRootPath(root, write.Path)
	if err != nil {
		return err
	}
	cachePath := cacheDir(root, write.Path)
	tmp := filepath.Join(filepath.Dir(finalPath), fmt.Sprintf(".%s.%d.staging", filepath.Base(finalPath), os.Getpid()))
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	verifiedByIndex := make(map[int]state.VerifiedStagedBlock, len(write.VerifiedBlocks))
	for _, verified := range write.VerifiedBlocks {
		verifiedByIndex[verified.Index] = verified
	}
	for _, want := range write.Manifest.Blocks {
		verified, ok := verifiedByIndex[want.Index]
		if !ok {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("pending write %s missing verified block %d", write.Path, want.Index)
		}
		if verified.Offset != want.Offset || verified.Size != want.Size || !bytes.Equal(verified.Hash, want.Hash) {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("pending write %s verified block %d no longer matches manifest", write.Path, want.Index)
		}
		blockPath := filepath.Join(cachePath, fmt.Sprintf("block-%06d", want.Index))
		data, err := os.ReadFile(blockPath)
		if err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return err
		}
		hash := sha256.Sum256(data)
		if len(data) != want.Size || !bytes.Equal(hash[:], want.Hash) {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("cached block verification failed for %s block %d", write.Path, want.Index)
		}
		if _, err := file.Seek(want.Offset, io.SeekStart); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	assembled, err := block.BuildManifest(tmp, write.Manifest.BlockSize)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	assembled.Path = write.Manifest.Path
	if !manifestsEqualBlocks(assembled, write.Manifest) {
		_ = os.Remove(tmp)
		return fmt.Errorf("assembled pending write %s does not match desired manifest", write.Path)
	}
	return os.Rename(tmp, finalPath)
}

func manifestsEqualBlocks(a block.Manifest, b block.Manifest) bool {
	if a.Size != b.Size || len(a.Blocks) != len(b.Blocks) {
		return false
	}
	for i := range a.Blocks {
		ab, bb := a.Blocks[i], b.Blocks[i]
		if ab.Index != bb.Index || ab.Offset != bb.Offset || ab.Size != bb.Size || !bytes.Equal(ab.Hash, bb.Hash) {
			return false
		}
	}
	return true
}

func verifyBlocks(manifest block.Manifest, blocks []BlockData) ([]state.VerifiedStagedBlock, map[int][]byte, error) {
	wanted := make(map[int]block.Block, len(manifest.Blocks))
	for _, b := range manifest.Blocks {
		wanted[b.Index] = b
	}
	byIndex := make(map[int][]byte, len(blocks))
	verified := make([]state.VerifiedStagedBlock, 0, len(blocks))
	for _, b := range blocks {
		want, ok := wanted[b.Index]
		if !ok {
			return nil, nil, fmt.Errorf("block %d is not in manifest", b.Index)
		}
		hash := sha256.Sum256(b.Data)
		if len(b.Data) != want.Size || !bytes.Equal(hash[:], want.Hash) {
			return nil, nil, fmt.Errorf("block verification failed for %s block %d", manifest.Path, b.Index)
		}
		byIndex[b.Index] = append([]byte(nil), b.Data...)
		verified = append(verified, state.VerifiedStagedBlock{Index: want.Index, Offset: want.Offset, Size: want.Size, Hash: append([]byte(nil), want.Hash...)})
	}
	sort.Slice(verified, func(i, j int) bool { return verified[i].Index < verified[j].Index })
	return verified, byIndex, nil
}

func cacheDir(root string, rel string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
	base := strings.TrimSuffix(filepath.Base(filepath.Clean(filepath.FromSlash(rel))), string(os.PathSeparator))
	if base == "." || base == string(os.PathSeparator) || base == "" {
		base = "pending"
	}
	return filepath.Join(root, ".sync", "locked-apply", base+"-"+hex.EncodeToString(sum[:8]))
}

func safeRootPath(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("root required")
	}
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined, err := filepath.Abs(filepath.Join(cleanRoot, cleanRel))
	if err != nil {
		return "", err
	}
	if joined != cleanRoot && !strings.HasPrefix(joined, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	return joined, nil
}

func writeBlockFile(path string, data []byte) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
