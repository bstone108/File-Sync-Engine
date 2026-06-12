package maintenance

import (
	"context"
	"sort"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

// MetadataConsistencyStore exposes metadata records needed by the report-only
// maintenance consistency crawler. The crawler deliberately reports suspicious
// records without pruning so delete/tombstone history is never resurrected by a
// maintenance pass.
type MetadataConsistencyStore interface {
	ListManifests(folderID string) (map[string]block.Manifest, error)
	ListManifestRevisions(folderID string) (map[string]uint64, error)
	ListTombstones(folderID string) (map[string]uint64, error)
	FolderSummary(folderID string) (state.FolderSummary, error)
}

type MetadataConsistencyCrawler struct {
	Store     MetadataConsistencyStore
	FolderIDs []string
}

type metadataConsistencyRecord struct {
	folderID string
	path     string
	revision uint64
	kind     metadataConsistencyKind
}

type metadataConsistencyKind string

const (
	metadataMissingRevision    metadataConsistencyKind = "missing-revision"
	metadataOrphanRevision     metadataConsistencyKind = "orphan-revision"
	metadataLiveAndTombstoned  metadataConsistencyKind = "live-and-tombstoned"
	metadataRevisionPastCursor metadataConsistencyKind = "revision-past-cursor"
)

func (c MetadataConsistencyCrawler) Step(ctx context.Context, cursor Cursor) (StepResult, error) {
	if err := ctx.Err(); err != nil {
		return StepResult{}, err
	}
	records, err := c.records()
	if err != nil {
		return StepResult{}, err
	}
	idx := int(cursor.Position)
	if idx >= len(records) {
		return StepResult{Cursor: Cursor{}, Complete: true}, nil
	}
	record := records[idx]
	return StepResult{
		Cursor:       Cursor{Position: uint64(idx + 1), FolderID: record.folderID, Path: record.path, Revision: record.revision},
		FilesScanned: 1,
		Reported:     1,
		Complete:     idx+1 >= len(records),
	}, nil
}

func (c MetadataConsistencyCrawler) records() ([]metadataConsistencyRecord, error) {
	folderIDs := append([]string(nil), c.FolderIDs...)
	sort.Strings(folderIDs)
	records := make([]metadataConsistencyRecord, 0)
	for _, folderID := range folderIDs {
		manifests, err := c.Store.ListManifests(folderID)
		if err != nil {
			return nil, err
		}
		revisions, err := c.Store.ListManifestRevisions(folderID)
		if err != nil {
			return nil, err
		}
		tombstones, err := c.Store.ListTombstones(folderID)
		if err != nil {
			return nil, err
		}
		summary, err := c.Store.FolderSummary(folderID)
		if err != nil {
			return nil, err
		}
		for path := range manifests {
			revision, hasRevision := revisions[path]
			if !hasRevision {
				records = append(records, metadataConsistencyRecord{folderID: folderID, path: path, kind: metadataMissingRevision})
			}
			if tombRevision, hasTombstone := tombstones[path]; hasTombstone {
				records = append(records, metadataConsistencyRecord{folderID: folderID, path: path, revision: tombRevision, kind: metadataLiveAndTombstoned})
			}
			if hasRevision && revision > summary.Cursor {
				records = append(records, metadataConsistencyRecord{folderID: folderID, path: path, revision: revision, kind: metadataRevisionPastCursor})
			}
		}
		for path, revision := range revisions {
			if _, hasManifest := manifests[path]; !hasManifest {
				if _, hasTombstone := tombstones[path]; !hasTombstone {
					records = append(records, metadataConsistencyRecord{folderID: folderID, path: path, revision: revision, kind: metadataOrphanRevision})
				}
			}
			if revision > summary.Cursor {
				records = append(records, metadataConsistencyRecord{folderID: folderID, path: path, revision: revision, kind: metadataRevisionPastCursor})
			}
		}
		for path, revision := range tombstones {
			if revision > summary.Cursor {
				records = append(records, metadataConsistencyRecord{folderID: folderID, path: path, revision: revision, kind: metadataRevisionPastCursor})
			}
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].folderID != records[j].folderID {
			return records[i].folderID < records[j].folderID
		}
		if records[i].path != records[j].path {
			return records[i].path < records[j].path
		}
		if records[i].revision != records[j].revision {
			return records[i].revision < records[j].revision
		}
		return records[i].kind < records[j].kind
	})
	return records, nil
}
