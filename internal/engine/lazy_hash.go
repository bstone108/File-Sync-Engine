package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/scanner"
)

var ErrLazyHashBaselineChanged = errors.New("lazy hash baseline changed")

type LazyHashResult struct {
	Hashed   bool
	Path     string
	Manifest block.Manifest
}

func (e Engine) HashFileOnDemand(folder config.FolderConfig, relativePath string) (block.Manifest, error) {
	stored, ok, err := e.store.LoadManifest(folder.ID, relativePath)
	if err != nil {
		return block.Manifest{}, err
	}
	if !ok {
		return block.Manifest{}, fmt.Errorf("manifest %s/%s not indexed", folder.ID, relativePath)
	}
	if stored.HashState == "complete" {
		return stored, nil
	}
	fullPath, err := safeFolderPath(folder.Path, relativePath)
	if err != nil {
		return block.Manifest{}, err
	}
	currentMeta, err := scanner.ScanFileMetadataOnly(fullPath, folder.BlockSize)
	if err != nil {
		return block.Manifest{}, err
	}
	if stored.HashState == HashStateAssumedValidUnverified {
		if seedBaselineChanged(stored, currentMeta) {
			if err := e.store.SaveManifest(folder.ID, relativePath, currentMeta); err != nil {
				return block.Manifest{}, err
			}
			return block.Manifest{}, ErrLazyHashBaselineChanged
		}
		manifest, err := scanner.ScanFile(fullPath, folder.BlockSize)
		if err != nil {
			return block.Manifest{}, err
		}
		if !sameBlocks(manifest.Blocks, stored.Blocks) {
			return block.Manifest{}, fmt.Errorf("manual seed verification failed for %s/%s", folder.ID, relativePath)
		}
		if stored.ModTimeUnixNano != 0 {
			mtime := time.Unix(0, stored.ModTimeUnixNano)
			if err := os.Chtimes(fullPath, mtime, mtime); err != nil {
				return block.Manifest{}, err
			}
			refreshedMeta, err := scanner.ScanFileMetadataOnly(fullPath, folder.BlockSize)
			if err != nil {
				return block.Manifest{}, err
			}
			manifest.ModTimeUnixNano = refreshedMeta.ModTimeUnixNano
			manifest.ChangeTimeUnixNano = refreshedMeta.ChangeTimeUnixNano
		}
		if err := e.store.SaveManifest(folder.ID, relativePath, manifest); err != nil {
			return block.Manifest{}, err
		}
		return manifest, nil
	}
	if metadataChanged(stored, currentMeta) {
		if err := e.store.SaveManifest(folder.ID, relativePath, currentMeta); err != nil {
			return block.Manifest{}, err
		}
		return block.Manifest{}, ErrLazyHashBaselineChanged
	}
	manifest, err := scanner.ScanFile(fullPath, folder.BlockSize)
	if err != nil {
		return block.Manifest{}, err
	}
	if err := e.store.SaveManifest(folder.ID, relativePath, manifest); err != nil {
		return block.Manifest{}, err
	}
	return manifest, nil
}

func (e Engine) HashNextUnknown(folder config.FolderConfig) (LazyHashResult, error) {
	manifests, err := e.store.ListManifests(folder.ID)
	if err != nil {
		return LazyHashResult{}, err
	}
	paths := make([]string, 0, len(manifests))
	for rel, manifest := range manifests {
		if manifest.HashState != "complete" {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	for _, rel := range paths {
		manifest, err := e.HashFileOnDemand(folder, rel)
		if errors.Is(err, ErrLazyHashBaselineChanged) {
			return LazyHashResult{Path: rel}, err
		}
		if err != nil {
			return LazyHashResult{}, err
		}
		return LazyHashResult{Hashed: true, Path: rel, Manifest: manifest}, nil
	}
	return LazyHashResult{}, nil
}

func safeFolderPath(root string, relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) || strings.Contains(relativePath, "\\") {
		return "", fmt.Errorf("invalid relative path %q", relativePath)
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("invalid relative path %q", relativePath)
	}
	return filepath.Join(root, clean), nil
}

func seedBaselineChanged(seed block.Manifest, current block.Manifest) bool {
	return seed.Size != current.Size ||
		seed.SeedBaselineModTimeUnixNano != current.ModTimeUnixNano ||
		seed.SeedBaselineChangeTimeUnixNano != current.ChangeTimeUnixNano
}

func sameBlocks(a []block.Block, b []block.Block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Index != b[i].Index || a[i].Offset != b[i].Offset || a[i].Size != b[i].Size || !bytes.Equal(a[i].Hash, b[i].Hash) {
			return false
		}
	}
	return true
}
