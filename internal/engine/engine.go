package engine

import (
	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/scanner"
)

type ManifestStore interface {
	SaveManifest(folderID string, relativePath string, manifest block.Manifest) error
	LoadManifest(folderID string, relativePath string) (block.Manifest, bool, error)
	ListManifests(folderID string) (map[string]block.Manifest, error)
	DeleteManifest(folderID string, relativePath string) error
}

type Engine struct {
	store ManifestStore
}

type ScanResult struct {
	FolderID     string
	Changed      []ChangedFile
	Deleted      []string
	Inaccessible []scanner.InaccessibleFile
}

type ChangedFile struct {
	Path         string
	NeededBlocks []block.Block
}

func New(store ManifestStore) Engine {
	return Engine{store: store}
}

func (e Engine) Scan(folder config.FolderConfig) (ScanResult, error) {
	return e.scan(folder, false)
}

func (e Engine) QuickIndex(folder config.FolderConfig) (ScanResult, error) {
	return e.scan(folder, true)
}

func (e Engine) scan(folder config.FolderConfig, metadataOnly bool) (ScanResult, error) {
	result := ScanResult{FolderID: folder.ID}
	var scan scanner.Result
	var err error
	if metadataOnly {
		scan, err = scanner.ScanFolderMetadataOnly(folder.Path, scanner.Options{BlockSize: folder.BlockSize, IgnoreSuffixes: folder.Ignore})
	} else {
		scan, err = scanner.ScanFolder(folder.Path, scanner.Options{BlockSize: folder.BlockSize, IgnoreSuffixes: folder.Ignore})
	}
	if err != nil {
		return ScanResult{}, err
	}
	seen := make(map[string]struct{}, len(scan.Files)+len(scan.Inaccessible))
	result.Inaccessible = append(result.Inaccessible, scan.Inaccessible...)
	for _, inaccessible := range scan.Inaccessible {
		seen[inaccessible.RelativePath] = struct{}{}
	}
	for _, file := range scan.Files {
		seen[file.RelativePath] = struct{}{}
		previous, ok, err := e.store.LoadManifest(folder.ID, file.RelativePath)
		if err != nil {
			return ScanResult{}, err
		}
		var needed []block.Block
		if metadataOnly {
			if !ok || metadataChanged(previous, file.Manifest) {
				result.Changed = append(result.Changed, ChangedFile{Path: file.RelativePath})
				if err := e.store.SaveManifest(folder.ID, file.RelativePath, file.Manifest); err != nil {
					return ScanResult{}, err
				}
			}
			continue
		} else {
			if !ok {
				needed = append([]block.Block(nil), file.Manifest.Blocks...)
			} else {
				needed = block.PlanDelta(previous, file.Manifest).Needed
			}
			if len(needed) > 0 {
				result.Changed = append(result.Changed, ChangedFile{Path: file.RelativePath, NeededBlocks: needed})
			}
		}
		if err := e.store.SaveManifest(folder.ID, file.RelativePath, file.Manifest); err != nil {
			return ScanResult{}, err
		}
	}
	previous, err := e.store.ListManifests(folder.ID)
	if err != nil {
		return ScanResult{}, err
	}
	for rel := range previous {
		if _, ok := seen[rel]; ok {
			continue
		}
		if err := e.store.DeleteManifest(folder.ID, rel); err != nil {
			return ScanResult{}, err
		}
		result.Deleted = append(result.Deleted, rel)
	}
	return result, nil
}

func metadataChanged(previous block.Manifest, current block.Manifest) bool {
	return previous.Size != current.Size ||
		previous.ModTimeUnixNano != current.ModTimeUnixNano ||
		previous.ChangeTimeUnixNano != current.ChangeTimeUnixNano
}
