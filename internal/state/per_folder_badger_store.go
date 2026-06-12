package state

import (
	"fmt"

	"filesyncengine/internal/block"
)

func (b perFolderBadgerBackend) Load() (snapshot, error) {
	merged := snapshot{Folders: map[string]map[string]block.Manifest{}}
	for _, store := range b.stores {
		child, err := store.load()
		if err != nil {
			return snapshot{}, err
		}
		mergeSnapshot(&merged, normalizeLoadedSnapshot(child))
	}
	return normalizeLoadedSnapshot(merged), nil
}

func (b perFolderBadgerBackend) Save(snap snapshot) error {
	normalized := normalizeLoadedSnapshot(snap)
	for folderID, store := range b.stores {
		if err := store.save(snapshotForFolder(normalized, folderID)); err != nil {
			return fmt.Errorf("save per-folder metadata %s: %w", folderID, err)
		}
	}
	return nil
}

func (b perFolderBadgerBackend) Close() error {
	var firstErr error
	for _, store := range b.stores {
		if err := store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (b perFolderBadgerBackend) FindBlocks(size int, hash []byte) ([]BlockRef, error) {
	refs := make([]BlockRef, 0)
	for folderID, store := range b.stores {
		matches, err := store.FindBlocks(size, hash)
		if err != nil {
			return nil, fmt.Errorf("find per-folder metadata blocks %s: %w", folderID, err)
		}
		refs = append(refs, matches...)
	}
	return sortBlockRefs(refs), nil
}

func (b perFolderBadgerBackend) ListBlockRefs(folderID string) ([]BlockRef, error) {
	if folderID != "" {
		store, ok := b.stores[folderID]
		if !ok {
			return nil, nil
		}
		refs, err := store.ListBlockRefs(folderID)
		if err != nil {
			return nil, fmt.Errorf("list per-folder metadata block refs %s: %w", folderID, err)
		}
		return sortBlockRefs(refs), nil
	}
	refs := make([]BlockRef, 0)
	for currentFolderID, store := range b.stores {
		folderRefs, err := store.ListBlockRefs(currentFolderID)
		if err != nil {
			return nil, fmt.Errorf("list per-folder metadata block refs %s: %w", currentFolderID, err)
		}
		refs = append(refs, folderRefs...)
	}
	return sortBlockRefs(refs), nil
}

func mergeSnapshot(dst *snapshot, src snapshot) {
	ensureSnapshotMaps(dst)
	for folderID, manifests := range src.Folders {
		if dst.Folders[folderID] == nil {
			dst.Folders[folderID] = map[string]block.Manifest{}
		}
		for path, manifest := range manifests {
			dst.Folders[folderID][path] = manifest
		}
	}
	mergeManifestHistory(dst, src)
	mergeFolderUintMaps(dst.Revisions, src.Revisions)
	mergeFolderUintMaps(dst.Tombstones, src.Tombstones)
	mergeRenameHints(dst, src)
	for folderID, cursor := range src.Cursors {
		dst.Cursors[folderID] = cursor
	}
	mergePeerStates(dst, src)
	mergePeerFolders(dst, src)
	mergePeerUintMaps(dst.PeerRevisions, src.PeerRevisions)
	mergePeerUintMaps(dst.PeerTombstones, src.PeerTombstones)
	mergePeerRenameHints(dst, src)
	for peerID, folders := range src.PeerCursors {
		if dst.PeerCursors[peerID] == nil {
			dst.PeerCursors[peerID] = map[string]uint64{}
		}
		for folderID, cursor := range folders {
			dst.PeerCursors[peerID][folderID] = cursor
		}
	}
	for peerID, folders := range src.PeerApplyCheckpoints {
		if dst.PeerApplyCheckpoints[peerID] == nil {
			dst.PeerApplyCheckpoints[peerID] = map[string]PeerApplyCheckpoint{}
		}
		for folderID, checkpoint := range folders {
			dst.PeerApplyCheckpoints[peerID][folderID] = checkpoint
		}
	}
	for folderID, snapshots := range src.CompactionSnapshots {
		dst.CompactionSnapshots[folderID] = append([]MetadataCompactionSnapshot(nil), snapshots...)
	}
	for id, marker := range src.SnapshotMarkers {
		dst.SnapshotMarkers[id] = marker
	}
	for id, jobs := range src.ArchiveIntakeJobs {
		dst.ArchiveIntakeJobs[id] = append([]ArchiveIntakeJob(nil), jobs...)
	}
	for folderID, writes := range src.PendingWrites {
		if dst.PendingWrites[folderID] == nil {
			dst.PendingWrites[folderID] = map[string]PendingWrite{}
		}
		for path, write := range writes {
			dst.PendingWrites[folderID][path] = write
		}
	}
	for folderID, deletes := range src.SkippedDeletes {
		if dst.SkippedDeletes[folderID] == nil {
			dst.SkippedDeletes[folderID] = map[string]SkippedDelete{}
		}
		for path, delete := range deletes {
			dst.SkippedDeletes[folderID][path] = delete
		}
	}
}

func snapshotForFolder(src snapshot, folderID string) snapshot {
	out := snapshot{Folders: map[string]map[string]block.Manifest{}}
	ensureSnapshotMaps(&out)
	if manifests := src.Folders[folderID]; len(manifests) > 0 {
		out.Folders[folderID] = cloneManifestMap(manifests)
	}
	if history := src.ManifestHistory[folderID]; len(history) > 0 {
		out.ManifestHistory[folderID] = cloneManifestHistoryMap(history)
	}
	if revisions := src.Revisions[folderID]; len(revisions) > 0 {
		out.Revisions[folderID] = cloneUintMap(revisions)
	}
	if tombstones := src.Tombstones[folderID]; len(tombstones) > 0 {
		out.Tombstones[folderID] = cloneUintMap(tombstones)
	}
	if hints := src.RenameHints[folderID]; len(hints) > 0 {
		out.RenameHints[folderID] = cloneRenameHintMap(hints)
	}
	if cursor, ok := src.Cursors[folderID]; ok {
		out.Cursors[folderID] = cursor
	}
	for peerID, folders := range src.PeerStates {
		if summary, ok := folders[folderID]; ok {
			if out.PeerStates[peerID] == nil {
				out.PeerStates[peerID] = map[string]FolderSummary{}
			}
			out.PeerStates[peerID][folderID] = summary
		}
	}
	for peerID, folders := range src.PeerFolders {
		if manifests := folders[folderID]; len(manifests) > 0 {
			if out.PeerFolders[peerID] == nil {
				out.PeerFolders[peerID] = map[string]map[string]block.Manifest{}
			}
			out.PeerFolders[peerID][folderID] = cloneManifestMap(manifests)
		}
	}
	copyPeerFolderUint(src.PeerRevisions, out.PeerRevisions, folderID)
	copyPeerFolderUint(src.PeerTombstones, out.PeerTombstones, folderID)
	for peerID, folders := range src.PeerRenameHints {
		if hints := folders[folderID]; len(hints) > 0 {
			if out.PeerRenameHints[peerID] == nil {
				out.PeerRenameHints[peerID] = map[string]map[string]RenameHint{}
			}
			out.PeerRenameHints[peerID][folderID] = cloneRenameHintMap(hints)
		}
	}
	for peerID, folders := range src.PeerCursors {
		if cursor, ok := folders[folderID]; ok {
			if out.PeerCursors[peerID] == nil {
				out.PeerCursors[peerID] = map[string]uint64{}
			}
			out.PeerCursors[peerID][folderID] = cursor
		}
	}
	for peerID, folders := range src.PeerApplyCheckpoints {
		if checkpoint, ok := folders[folderID]; ok {
			if out.PeerApplyCheckpoints[peerID] == nil {
				out.PeerApplyCheckpoints[peerID] = map[string]PeerApplyCheckpoint{}
			}
			out.PeerApplyCheckpoints[peerID][folderID] = checkpoint
		}
	}
	if snapshots := src.CompactionSnapshots[folderID]; len(snapshots) > 0 {
		out.CompactionSnapshots[folderID] = append([]MetadataCompactionSnapshot(nil), snapshots...)
	}
	for id, marker := range src.SnapshotMarkers {
		if marker.FolderID == folderID {
			out.SnapshotMarkers[id] = marker
		}
	}
	for id, jobs := range src.ArchiveIntakeJobs {
		filtered := make([]ArchiveIntakeJob, 0, len(jobs))
		for _, job := range jobs {
			if job.FolderID == folderID {
				filtered = append(filtered, job)
			}
		}
		if len(filtered) > 0 {
			out.ArchiveIntakeJobs[id] = filtered
		}
	}
	if writes := src.PendingWrites[folderID]; len(writes) > 0 {
		out.PendingWrites[folderID] = clonePendingWriteMap(writes)
	}
	if deletes := src.SkippedDeletes[folderID]; len(deletes) > 0 {
		out.SkippedDeletes[folderID] = cloneSkippedDeleteMap(deletes)
	}
	return out
}

func ensureSnapshotMaps(snap *snapshot) {
	if snap.Folders == nil {
		snap.Folders = map[string]map[string]block.Manifest{}
	}
	if snap.ManifestHistory == nil {
		snap.ManifestHistory = map[string]map[string]map[uint64]block.Manifest{}
	}
	if snap.Revisions == nil {
		snap.Revisions = map[string]map[string]uint64{}
	}
	if snap.Tombstones == nil {
		snap.Tombstones = map[string]map[string]uint64{}
	}
	if snap.RenameHints == nil {
		snap.RenameHints = map[string]map[string]RenameHint{}
	}
	if snap.Cursors == nil {
		snap.Cursors = map[string]uint64{}
	}
	if snap.PeerStates == nil {
		snap.PeerStates = map[string]map[string]FolderSummary{}
	}
	if snap.PeerFolders == nil {
		snap.PeerFolders = map[string]map[string]map[string]block.Manifest{}
	}
	if snap.PeerRevisions == nil {
		snap.PeerRevisions = map[string]map[string]map[string]uint64{}
	}
	if snap.PeerTombstones == nil {
		snap.PeerTombstones = map[string]map[string]map[string]uint64{}
	}
	if snap.PeerRenameHints == nil {
		snap.PeerRenameHints = map[string]map[string]map[string]RenameHint{}
	}
	if snap.PeerCursors == nil {
		snap.PeerCursors = map[string]map[string]uint64{}
	}
	if snap.PeerApplyCheckpoints == nil {
		snap.PeerApplyCheckpoints = map[string]map[string]PeerApplyCheckpoint{}
	}
	if snap.CompactionSnapshots == nil {
		snap.CompactionSnapshots = map[string][]MetadataCompactionSnapshot{}
	}
	if snap.SnapshotMarkers == nil {
		snap.SnapshotMarkers = map[string]SnapshotMarker{}
	}
	if snap.ArchiveIntakeJobs == nil {
		snap.ArchiveIntakeJobs = map[string][]ArchiveIntakeJob{}
	}
	if snap.PendingWrites == nil {
		snap.PendingWrites = map[string]map[string]PendingWrite{}
	}
	if snap.SkippedDeletes == nil {
		snap.SkippedDeletes = map[string]map[string]SkippedDelete{}
	}
}

func cloneManifestMap(in map[string]block.Manifest) map[string]block.Manifest {
	out := make(map[string]block.Manifest, len(in))
	for path, manifest := range in {
		out[path] = manifest
	}
	return out
}

func cloneManifestHistoryMap(in map[string]map[uint64]block.Manifest) map[string]map[uint64]block.Manifest {
	out := make(map[string]map[uint64]block.Manifest, len(in))
	for path, revisions := range in {
		out[path] = map[uint64]block.Manifest{}
		for rev, manifest := range revisions {
			out[path][rev] = manifest
		}
	}
	return out
}

func cloneUintMap(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneRenameHintMap(in map[string]RenameHint) map[string]RenameHint {
	out := make(map[string]RenameHint, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func clonePendingWriteMap(in map[string]PendingWrite) map[string]PendingWrite {
	out := make(map[string]PendingWrite, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneSkippedDeleteMap(in map[string]SkippedDelete) map[string]SkippedDelete {
	out := make(map[string]SkippedDelete, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeManifestHistory(dst *snapshot, src snapshot) {
	for folderID, paths := range src.ManifestHistory {
		if dst.ManifestHistory[folderID] == nil {
			dst.ManifestHistory[folderID] = map[string]map[uint64]block.Manifest{}
		}
		for path, revisions := range paths {
			dst.ManifestHistory[folderID][path] = map[uint64]block.Manifest{}
			for rev, manifest := range revisions {
				dst.ManifestHistory[folderID][path][rev] = manifest
			}
		}
	}
}

func mergeFolderUintMaps(dst, src map[string]map[string]uint64) {
	for folderID, values := range src {
		if dst[folderID] == nil {
			dst[folderID] = map[string]uint64{}
		}
		for path, value := range values {
			dst[folderID][path] = value
		}
	}
}

func mergeRenameHints(dst *snapshot, src snapshot) {
	for folderID, hints := range src.RenameHints {
		if dst.RenameHints[folderID] == nil {
			dst.RenameHints[folderID] = map[string]RenameHint{}
		}
		for path, hint := range hints {
			dst.RenameHints[folderID][path] = hint
		}
	}
}

func mergePeerStates(dst *snapshot, src snapshot) {
	for peerID, folders := range src.PeerStates {
		if dst.PeerStates[peerID] == nil {
			dst.PeerStates[peerID] = map[string]FolderSummary{}
		}
		for folderID, summary := range folders {
			dst.PeerStates[peerID][folderID] = summary
		}
	}
}

func mergePeerFolders(dst *snapshot, src snapshot) {
	for peerID, folders := range src.PeerFolders {
		if dst.PeerFolders[peerID] == nil {
			dst.PeerFolders[peerID] = map[string]map[string]block.Manifest{}
		}
		for folderID, manifests := range folders {
			dst.PeerFolders[peerID][folderID] = cloneManifestMap(manifests)
		}
	}
}

func mergePeerUintMaps(dst, src map[string]map[string]map[string]uint64) {
	for peerID, folders := range src {
		if dst[peerID] == nil {
			dst[peerID] = map[string]map[string]uint64{}
		}
		for folderID, values := range folders {
			dst[peerID][folderID] = cloneUintMap(values)
		}
	}
}

func mergePeerRenameHints(dst *snapshot, src snapshot) {
	for peerID, folders := range src.PeerRenameHints {
		if dst.PeerRenameHints[peerID] == nil {
			dst.PeerRenameHints[peerID] = map[string]map[string]RenameHint{}
		}
		for folderID, hints := range folders {
			dst.PeerRenameHints[peerID][folderID] = cloneRenameHintMap(hints)
		}
	}
}

func copyPeerFolderUint(src, dst map[string]map[string]map[string]uint64, folderID string) {
	for peerID, folders := range src {
		if values := folders[folderID]; len(values) > 0 {
			if dst[peerID] == nil {
				dst[peerID] = map[string]map[string]uint64{}
			}
			dst[peerID][folderID] = cloneUintMap(values)
		}
	}
}
