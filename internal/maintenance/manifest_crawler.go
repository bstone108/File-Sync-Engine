package maintenance

import (
	"context"
	"sort"

	"filesyncengine/internal/block"
)

type ManifestStore interface {
	ListManifests(folderID string) (map[string]block.Manifest, error)
}

type ManifestRevisionStore interface {
	ListManifestRevisions(folderID string) (map[string]uint64, error)
}

type ManifestCrawler struct {
	Store     ManifestStore
	FolderIDs []string
}

type manifestRecord struct {
	folderID string
	path     string
	revision uint64
	manifest block.Manifest
}

func (c ManifestCrawler) Step(_ context.Context, cursor Cursor) (StepResult, error) {
	records, err := c.records()
	if err != nil {
		return StepResult{}, err
	}
	idx := c.resumeIndex(records, cursor)
	if idx >= len(records) {
		return StepResult{Cursor: Cursor{}, Complete: true}, nil
	}
	record := records[idx]
	next := Cursor{Position: uint64(idx + 1), FolderID: record.folderID, Path: record.path, Revision: record.revision}
	return StepResult{
		Cursor:       next,
		FilesScanned: 1,
		BytesScanned: record.manifest.Size,
		Complete:     idx+1 >= len(records),
	}, nil
}

func (c ManifestCrawler) records() ([]manifestRecord, error) {
	folderIDs := append([]string(nil), c.FolderIDs...)
	sort.Strings(folderIDs)
	records := make([]manifestRecord, 0)
	for _, folderID := range folderIDs {
		manifests, err := c.Store.ListManifests(folderID)
		if err != nil {
			return nil, err
		}
		revisions, err := c.revisions(folderID)
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(manifests))
		for path := range manifests {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			records = append(records, manifestRecord{folderID: folderID, path: path, revision: revisions[path], manifest: manifests[path]})
		}
	}
	return records, nil
}

func (c ManifestCrawler) revisions(folderID string) (map[string]uint64, error) {
	store, ok := c.Store.(ManifestRevisionStore)
	if !ok {
		return map[string]uint64{}, nil
	}
	return store.ListManifestRevisions(folderID)
}

func (c ManifestCrawler) resumeIndex(records []manifestRecord, cursor Cursor) int {
	if cursor.FolderID == "" || cursor.Path == "" {
		return int(cursor.Position)
	}
	for i, record := range records {
		if record.folderID == cursor.FolderID && record.path == cursor.Path && record.revision == cursor.Revision {
			return i + 1
		}
	}
	return int(cursor.Position)
}
