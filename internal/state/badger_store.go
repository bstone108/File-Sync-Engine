package state

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dgraph-io/badger/v4"

	"filesyncengine/internal/block"
)

const badgerKeyPrefix = "fse/v1/"

func badgerEncodePart(value string) string {
	if value == "" {
		return "-"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func badgerDecodePart(value string) (string, error) {
	if value == "-" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func badgerManifestKey(folderID string, relativePath string) []byte {
	return []byte(badgerKeyPrefix + "manifest/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath))
}

func badgerManifestHistoryKey(folderID string, relativePath string, revision uint64) []byte {
	return []byte(badgerKeyPrefix + "manifest-history/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath) + "/" + strconv.FormatUint(revision, 10))
}

func badgerRevisionKey(folderID string, relativePath string) []byte {
	return []byte(badgerKeyPrefix + "revision/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath))
}

func badgerTombstoneKey(folderID string, relativePath string) []byte {
	return []byte(badgerKeyPrefix + "tombstone/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath))
}

func badgerRenameHintKey(folderID string, fromPath string) []byte {
	return []byte(badgerKeyPrefix + "rename/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(fromPath))
}

func badgerCursorKey(folderID string) []byte {
	return []byte(badgerKeyPrefix + "cursor/" + badgerEncodePart(folderID))
}

func badgerPeerStateKey(peerID string, folderID string) []byte {
	return []byte(badgerKeyPrefix + "peer-state/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID))
}

func badgerPeerManifestKey(peerID string, folderID string, relativePath string) []byte {
	return []byte(badgerKeyPrefix + "peer-manifest/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath))
}

func badgerPeerRevisionKey(peerID string, folderID string, relativePath string) []byte {
	return []byte(badgerKeyPrefix + "peer-revision/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath))
}

func badgerPeerTombstoneKey(peerID string, folderID string, relativePath string) []byte {
	return []byte(badgerKeyPrefix + "peer-tombstone/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath))
}

func badgerPeerRenameHintKey(peerID string, folderID string, fromPath string) []byte {
	return []byte(badgerKeyPrefix + "peer-rename/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(fromPath))
}

func badgerPeerCursorKey(peerID string, folderID string) []byte {
	return []byte(badgerKeyPrefix + "peer-cursor/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID))
}

func badgerPeerApplyCheckpointKey(peerID string, folderID string) []byte {
	return []byte(badgerKeyPrefix + "peer-checkpoint/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID))
}

func badgerCompactionSnapshotKey(folderID string, index int) []byte {
	return []byte(badgerKeyPrefix + "compaction-snapshot/" + badgerEncodePart(folderID) + "/" + strconv.Itoa(index))
}

func badgerSnapshotMarkerKey(id string) []byte {
	return []byte(badgerKeyPrefix + "snapshot-marker/" + badgerEncodePart(id))
}

func badgerArchiveIntakeJobKey(snapshotID string, jobID string) []byte {
	return []byte(badgerKeyPrefix + "archive-intake/" + badgerEncodePart(snapshotID) + "/" + badgerEncodePart(jobID))
}

func badgerBackupRestoreJobKey(jobID string) []byte {
	return []byte(badgerKeyPrefix + "backup-restore-job/" + badgerEncodePart(jobID))
}

func badgerBackupRetentionJobKey(jobID string) []byte {
	return []byte(badgerKeyPrefix + "backup-retention-job/" + badgerEncodePart(jobID))
}

func badgerBackupRepairJobKey(jobID string) []byte {
	return []byte(badgerKeyPrefix + "backup-repair-job/" + badgerEncodePart(jobID))
}

func badgerPendingWriteKey(folderID string, path string) []byte {
	return []byte(badgerKeyPrefix + "pending-write/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(path))
}

func badgerSkippedDeleteKey(folderID string, path string) []byte {
	return []byte(badgerKeyPrefix + "skipped-delete/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(path))
}

func badgerNodeSettingsKey(nodeID string) []byte {
	return []byte(badgerKeyPrefix + "node-settings/" + badgerEncodePart(nodeID))
}

func badgerPendingSettingsKey(targetNodeID string, changeID string) []byte {
	return []byte(badgerKeyPrefix + "pending-settings/" + badgerEncodePart(targetNodeID) + "/" + badgerEncodePart(changeID))
}

func badgerBlockIndexKey(size int, hash []byte, folderID string, relativePath string, blockIndex int) []byte {
	return []byte(badgerKeyPrefix + "block/" + strconv.Itoa(size) + "/" + hex.EncodeToString(hash) + "/" + badgerEncodePart(folderID) + "/" + badgerEncodePart(relativePath) + "/" + strconv.Itoa(blockIndex))
}

func (b badgerSnapshotBackend) migrateLegacySnapshot() error {
	var legacy *snapshot
	foundKeyLevel := false
	if err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerKeyPrefix)
		it := txn.NewIterator(opts)
		defer it.Close()
		it.Rewind()
		foundKeyLevel = it.Valid()
		if foundKeyLevel {
			return nil
		}
		item, err := txn.Get(badgerSnapshotKey)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var snap snapshot
		if err := item.Value(func(data []byte) error { return json.Unmarshal(data, &snap) }); err != nil {
			return err
		}
		normalized := normalizeLoadedSnapshot(snap)
		legacy = &normalized
		return nil
	}); err != nil {
		return err
	}
	if foundKeyLevel {
		return b.db.Update(func(txn *badger.Txn) error {
			if err := txn.Delete(badgerSnapshotKey); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
			return nil
		})
	}
	if legacy == nil {
		return nil
	}
	return b.db.Update(func(txn *badger.Txn) error {
		if err := badgerClearKeyspace(txn); err != nil {
			return err
		}
		return badgerWriteSnapshot(txn, *legacy)
	})
}

func (b badgerSnapshotBackend) Load() (snapshot, error) {
	var snap snapshot
	foundKeyLevel := false
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerKeyPrefix)
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			foundKeyLevel = true
			item := it.Item()
			key := string(item.Key())
			if err := item.Value(func(data []byte) error {
				return badgerLoadKeyValue(&snap, key, data)
			}); err != nil {
				return err
			}
		}
		if foundKeyLevel {
			return nil
		}
		item, err := txn.Get(badgerSnapshotKey)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(data []byte) error {
			return json.Unmarshal(data, &snap)
		})
	})
	if err != nil {
		return snapshot{}, err
	}
	if snap.Folders == nil {
		snap.Folders = map[string]map[string]block.Manifest{}
	}
	return normalizeLoadedSnapshot(snap), nil
}

func (b badgerSnapshotBackend) Save(snap snapshot) error {
	return b.db.Update(func(txn *badger.Txn) error {
		if err := badgerClearKeyspace(txn); err != nil {
			return err
		}
		return badgerWriteSnapshot(txn, snap)
	})
}

func (b badgerSnapshotBackend) Close() error {
	return b.db.Close()
}

func (b badgerSnapshotBackend) SaveSnapshotMarker(marker SnapshotMarker) error {
	if marker.ID == "" {
		return fmt.Errorf("snapshot marker id is required")
	}
	if marker.FolderID == "" {
		return fmt.Errorf("snapshot marker folder id is required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerSnapshotMarkerKey(marker.ID), marker)
	})
}

func (b badgerSnapshotBackend) LoadSnapshotMarker(id string) (SnapshotMarker, bool, error) {
	var marker SnapshotMarker
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerSnapshotMarkerKey(id))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &marker) })
	})
	if err != nil {
		return SnapshotMarker{}, false, err
	}
	return marker, found, nil
}

func (b badgerSnapshotBackend) ListSnapshotMarkers(folderID string) ([]SnapshotMarker, error) {
	markers := make([]SnapshotMarker, 0)
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerKeyPrefix + "snapshot-marker/")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var marker SnapshotMarker
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &marker) }); err != nil {
				return err
			}
			if folderID == "" || marker.FolderID == folderID {
				markers = append(markers, marker)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].CreatedAt != markers[j].CreatedAt {
			return markers[i].CreatedAt < markers[j].CreatedAt
		}
		return markers[i].ID < markers[j].ID
	})
	return markers, nil
}

func (b badgerSnapshotBackend) DeleteSnapshotMarker(id string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(badgerSnapshotMarkerKey(id)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		return nil
	})
}

func (b badgerSnapshotBackend) PreserveSnapshotManifest(folderID string, relativePath string, revision uint64, manifest block.Manifest) error {
	if folderID == "" {
		return fmt.Errorf("folder id required")
	}
	if relativePath == "" {
		return fmt.Errorf("relative path required")
	}
	if revision == 0 {
		return fmt.Errorf("revision required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerManifestHistoryKey(folderID, relativePath, revision), manifest)
	})
}

func (b badgerSnapshotBackend) SaveArchiveIntakeJobs(snapshotID string, jobs []ArchiveIntakeJob) error {
	if snapshotID == "" {
		return fmt.Errorf("snapshot id is required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		prefix := []byte(badgerKeyPrefix + "archive-intake/" + badgerEncodePart(snapshotID) + "/")
		if err := badgerDeletePrefix(txn, prefix); err != nil {
			return err
		}
		for _, job := range jobs {
			if job.ID == "" {
				return fmt.Errorf("archive intake job id is required")
			}
			if err := badgerSetJSON(txn, badgerArchiveIntakeJobKey(snapshotID, job.ID), job); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b badgerSnapshotBackend) ListArchiveIntakeJobs(snapshotID string) ([]ArchiveIntakeJob, error) {
	jobs := make([]ArchiveIntakeJob, 0)
	err := b.db.View(func(txn *badger.Txn) error {
		prefix := []byte(badgerKeyPrefix + "archive-intake/")
		if snapshotID != "" {
			prefix = []byte(badgerKeyPrefix + "archive-intake/" + badgerEncodePart(snapshotID) + "/")
		}
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var job ArchiveIntakeJob
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &job) }); err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].SnapshotID != jobs[j].SnapshotID {
			return jobs[i].SnapshotID < jobs[j].SnapshotID
		}
		return jobs[i].ID < jobs[j].ID
	})
	return cloneArchiveIntakeJobs(jobs), nil
}

func (b badgerSnapshotBackend) SaveBackupRestoreJob(job BackupRestoreJob) error {
	if job.ID == "" {
		return fmt.Errorf("backup restore job id is required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerBackupRestoreJobKey(job.ID), job)
	})
}

func (b badgerSnapshotBackend) LoadBackupRestoreJob(id string) (BackupRestoreJob, bool, error) {
	var job BackupRestoreJob
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerBackupRestoreJobKey(id))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &job) })
	})
	return cloneBackupRestoreJob(job), found, err
}

func (b badgerSnapshotBackend) ListBackupRestoreJobs(snapshotID string) ([]BackupRestoreJob, error) {
	jobs := make([]BackupRestoreJob, 0)
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerKeyPrefix + "backup-restore-job/")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var job BackupRestoreJob
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &job) }); err != nil {
				return err
			}
			if snapshotID == "" || job.SnapshotID == snapshotID {
				jobs = append(jobs, cloneBackupRestoreJob(job))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	return jobs, nil
}

func (b badgerSnapshotBackend) SaveBackupRetentionJob(job BackupRetentionJob) error {
	if job.ID == "" {
		return fmt.Errorf("backup retention job id is required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerBackupRetentionJobKey(job.ID), job)
	})
}

func (b badgerSnapshotBackend) LoadBackupRetentionJob(id string) (BackupRetentionJob, bool, error) {
	var job BackupRetentionJob
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerBackupRetentionJobKey(id))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &job) })
	})
	return job, found, err
}

func (b badgerSnapshotBackend) ListBackupRetentionJobs() ([]BackupRetentionJob, error) {
	jobs := make([]BackupRetentionJob, 0)
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerKeyPrefix + "backup-retention-job/")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var job BackupRetentionJob
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &job) }); err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	return jobs, nil
}

func (b badgerSnapshotBackend) SaveBackupRepairJob(job BackupRepairJob) error {
	if job.ID == "" {
		return fmt.Errorf("backup repair job id is required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerBackupRepairJobKey(job.ID), cloneBackupRepairJob(job))
	})
}

func (b badgerSnapshotBackend) LoadBackupRepairJob(id string) (BackupRepairJob, bool, error) {
	var job BackupRepairJob
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerBackupRepairJobKey(id))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &job) })
	})
	return cloneBackupRepairJob(job), found, err
}

func (b badgerSnapshotBackend) ListBackupRepairJobs() ([]BackupRepairJob, error) {
	jobs := make([]BackupRepairJob, 0)
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerKeyPrefix + "backup-repair-job/")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var job BackupRepairJob
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &job) }); err != nil {
				return err
			}
			jobs = append(jobs, cloneBackupRepairJob(job))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	return jobs, nil
}

func (b badgerSnapshotBackend) SaveNodeSettingsDocument(nodeID string, doc NodeSettingsDocument) error {
	if doc.NodeID == "" {
		return fmt.Errorf("node settings document node id is required")
	}
	if nodeID == "" {
		nodeID = doc.NodeID
	}
	if doc.NodeID != nodeID {
		return fmt.Errorf("node settings document owner mismatch: key %q document %q", nodeID, doc.NodeID)
	}
	if doc.Settings == nil {
		doc.Settings = map[string]any{}
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerNodeSettingsKey(nodeID), doc)
	})
}

func (b badgerSnapshotBackend) LoadNodeSettingsDocument(nodeID string) (NodeSettingsDocument, bool, error) {
	var doc NodeSettingsDocument
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerNodeSettingsKey(nodeID))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &doc) })
	})
	return doc, found, err
}

func (b badgerSnapshotBackend) ListNodeSettingsDocuments() ([]NodeSettingsDocument, error) {
	docs := make([]NodeSettingsDocument, 0)
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(badgerKeyPrefix + "node-settings/")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var doc NodeSettingsDocument
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &doc) }); err != nil {
				return err
			}
			docs = append(docs, doc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].NodeID < docs[j].NodeID })
	return docs, nil
}

func (b badgerSnapshotBackend) SavePendingSettingsChange(targetNodeID string, change PendingSettingsChange) error {
	if err := validatePendingSettingsChangeTarget(targetNodeID, change); err != nil {
		return err
	}
	if change.SettingsPatch == nil {
		change.SettingsPatch = map[string]any{}
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerPendingSettingsKey(targetNodeID, change.ID), change)
	})
}

func (b badgerSnapshotBackend) LoadPendingSettingsChange(targetNodeID string, changeID string) (PendingSettingsChange, bool, error) {
	var change PendingSettingsChange
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerPendingSettingsKey(targetNodeID, changeID))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &change) })
	})
	return change, found, err
}

func (b badgerSnapshotBackend) ListPendingSettingsChanges(targetNodeID string) ([]PendingSettingsChange, error) {
	changes := make([]PendingSettingsChange, 0)
	prefix := badgerKeyPrefix + "pending-settings/"
	if targetNodeID != "" {
		prefix += badgerEncodePart(targetNodeID) + "/"
	}
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(prefix)
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var change PendingSettingsChange
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &change) }); err != nil {
				return err
			}
			changes = append(changes, change)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].TargetNodeID == changes[j].TargetNodeID {
			return changes[i].ID < changes[j].ID
		}
		return changes[i].TargetNodeID < changes[j].TargetNodeID
	})
	return changes, nil
}

func (b badgerSnapshotBackend) SaveManifest(folderID string, relativePath string, manifest block.Manifest) error {
	return b.db.Update(func(txn *badger.Txn) error {
		rev, err := badgerNextFolderRevision(txn, folderID)
		if err != nil {
			return err
		}
		if err := badgerDeleteManifestRecords(txn, folderID, relativePath); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerManifestKey(folderID, relativePath), manifest); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerRevisionKey(folderID, relativePath), rev); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerManifestHistoryKey(folderID, relativePath, rev), manifest); err != nil {
			return err
		}
		if err := txn.Delete(badgerTombstoneKey(folderID, relativePath)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		if !manifest.Damaged {
			for _, block := range manifest.Blocks {
				ref := BlockRef{FolderID: folderID, RelativePath: relativePath, Block: block}
				if err := badgerSetJSON(txn, badgerBlockIndexKey(block.Size, block.Hash, folderID, relativePath, block.Index), ref); err != nil {
					return err
				}
			}
		}
		return badgerSetJSON(txn, badgerCursorKey(folderID), rev)
	})
}

func (b badgerSnapshotBackend) LoadManifest(folderID string, relativePath string) (block.Manifest, bool, error) {
	var manifest block.Manifest
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerManifestKey(folderID, relativePath))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &manifest) })
	})
	if err != nil {
		return block.Manifest{}, false, err
	}
	if !found {
		return block.Manifest{}, false, nil
	}
	return manifest, true, nil
}

func (b badgerSnapshotBackend) ListManifests(folderID string) (map[string]block.Manifest, error) {
	out := map[string]block.Manifest{}
	prefix := []byte(badgerKeyPrefix + "manifest/" + badgerEncodePart(folderID) + "/")
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := string(it.Item().Key())
			parts := strings.Split(strings.TrimPrefix(key, badgerKeyPrefix), "/")
			if len(parts) != 3 {
				return fmt.Errorf("invalid badger manifest key %q", key)
			}
			rel, err := badgerDecodePart(parts[2])
			if err != nil {
				return err
			}
			var manifest block.Manifest
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &manifest) }); err != nil {
				return err
			}
			out[rel] = manifest
		}
		return nil
	})
	return out, err
}

func (b badgerSnapshotBackend) DeleteManifest(folderID string, relativePath string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		rev, err := badgerNextFolderRevision(txn, folderID)
		if err != nil {
			return err
		}
		if err := badgerDeleteManifestRecords(txn, folderID, relativePath); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerTombstoneKey(folderID, relativePath), rev); err != nil {
			return err
		}
		return badgerSetJSON(txn, badgerCursorKey(folderID), rev)
	})
}

func (b badgerSnapshotBackend) MoveManifest(folderID string, fromPath string, toPath string, manifest block.Manifest) error {
	if folderID == "" {
		return fmt.Errorf("folder id required")
	}
	if fromPath == "" || toPath == "" {
		return fmt.Errorf("move paths required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		rev, err := badgerNextFolderRevision(txn, folderID)
		if err != nil {
			return err
		}
		if err := badgerDeleteManifestRecords(txn, folderID, fromPath); err != nil {
			return err
		}
		if err := badgerDeleteManifestRecords(txn, folderID, toPath); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerTombstoneKey(folderID, fromPath), rev); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerManifestKey(folderID, toPath), manifest); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerRevisionKey(folderID, toPath), rev); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerManifestHistoryKey(folderID, toPath, rev), manifest); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerRenameHintKey(folderID, fromPath), RenameHint{FromPath: fromPath, ToPath: toPath, Revision: rev}); err != nil {
			return err
		}
		for _, block := range manifest.Blocks {
			ref := BlockRef{FolderID: folderID, RelativePath: toPath, Block: block}
			if err := badgerSetJSON(txn, badgerBlockIndexKey(block.Size, block.Hash, folderID, toPath, block.Index), ref); err != nil {
				return err
			}
		}
		return badgerSetJSON(txn, badgerCursorKey(folderID), rev)
	})
}

func (b badgerSnapshotBackend) ListManifestRevisions(folderID string) (map[string]uint64, error) {
	return b.listFolderUint64Records(folderID, "revision")
}

func (b badgerSnapshotBackend) ListTombstones(folderID string) (map[string]uint64, error) {
	return b.listFolderUint64Records(folderID, "tombstone")
}

func (b badgerSnapshotBackend) listFolderUint64Records(folderID string, recordKind string) (map[string]uint64, error) {
	out := map[string]uint64{}
	prefix := []byte(badgerKeyPrefix + recordKind + "/" + badgerEncodePart(folderID) + "/")
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := string(it.Item().Key())
			parts := strings.Split(strings.TrimPrefix(key, badgerKeyPrefix), "/")
			if len(parts) != 3 {
				return fmt.Errorf("invalid badger %s key %q", recordKind, key)
			}
			rel, err := badgerDecodePart(parts[2])
			if err != nil {
				return err
			}
			var rev uint64
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &rev) }); err != nil {
				return err
			}
			out[rel] = rev
		}
		return nil
	})
	return out, err
}

func (b badgerSnapshotBackend) FindBlocks(size int, hash []byte) ([]BlockRef, error) {
	refs := make([]BlockRef, 0)
	prefix := []byte(badgerKeyPrefix + "block/" + strconv.Itoa(size) + "/" + hex.EncodeToString(hash) + "/")
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var ref BlockRef
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &ref) }); err != nil {
				return err
			}
			refs = append(refs, ref)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sortBlockRefs(refs), nil
}

func (b badgerSnapshotBackend) ListBlockRefs(folderID string) ([]BlockRef, error) {
	refs := make([]BlockRef, 0)
	prefix := []byte(badgerKeyPrefix + "block/")
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var ref BlockRef
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &ref) }); err != nil {
				return err
			}
			if folderID != "" && ref.FolderID != folderID {
				continue
			}
			refs = append(refs, ref)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sortBlockRefs(refs), nil
}

func (b badgerSnapshotBackend) SavePeerFolderState(peerID string, summary FolderSummary) error {
	if peerID == "" {
		return fmt.Errorf("peer id required")
	}
	if summary.FolderID == "" {
		return fmt.Errorf("folder id required")
	}
	return b.db.Update(func(txn *badger.Txn) error {
		if err := badgerSetJSON(txn, badgerPeerStateKey(peerID, summary.FolderID), summary); err != nil {
			return err
		}
		return badgerSetJSON(txn, badgerPeerCursorKey(peerID, summary.FolderID), summary.Cursor)
	})
}

func (b badgerSnapshotBackend) LoadPeerManifest(peerID string, folderID string, relativePath string) (block.Manifest, bool, error) {
	var manifest block.Manifest
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerPeerManifestKey(peerID, folderID, relativePath))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &manifest) })
	})
	if err != nil {
		return block.Manifest{}, false, err
	}
	return manifest, found, nil
}

func (b badgerSnapshotBackend) PeerApplyCheckpoint(peerID string, folderID string) (PeerApplyCheckpoint, bool, error) {
	var checkpoint PeerApplyCheckpoint
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerPeerApplyCheckpointKey(peerID, folderID))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &checkpoint) })
	})
	if err != nil {
		return PeerApplyCheckpoint{}, false, err
	}
	return checkpoint, found, nil
}

func (b badgerSnapshotBackend) ApplyPeerFolderChanges(peerID string, changes FolderChanges) error {
	if peerID == "" {
		return fmt.Errorf("peer id required")
	}
	if changes.FolderID == "" {
		return fmt.Errorf("folder id required")
	}
	snap, err := b.Load()
	if err != nil {
		return err
	}
	ensurePeerFolderState(&snap, peerID, changes.FolderID)
	current := snap.PeerCursors[peerID][changes.FolderID]
	if current != changes.FromCursor {
		if current == changes.ToCursor {
			summary := snap.PeerStates[peerID][changes.FolderID]
			if summary.Cursor == changes.ToCursor && summary.StateHash == changes.StateHash {
				return nil
			}
		}
		return fmt.Errorf("peer metadata cursor mismatch for %s/%s: have %d, changes start at %d", peerID, changes.FolderID, current, changes.FromCursor)
	}
	candidate := cloneSnapshot(snap)
	ensurePeerFolderState(&candidate, peerID, changes.FolderID)
	for _, change := range changes.Changes {
		if change.Revision <= changes.FromCursor || change.Revision > changes.ToCursor {
			return fmt.Errorf("peer metadata revision %d outside cursor range %d..%d", change.Revision, changes.FromCursor, changes.ToCursor)
		}
		switch change.Kind {
		case ChangeUpsert:
			if change.Manifest == nil {
				return fmt.Errorf("peer metadata upsert %s missing manifest", change.Path)
			}
			candidate.PeerFolders[peerID][changes.FolderID][change.Path] = *change.Manifest
			candidate.PeerRevisions[peerID][changes.FolderID][change.Path] = change.Revision
			delete(candidate.PeerTombstones[peerID][changes.FolderID], change.Path)
		case ChangeDelete:
			delete(candidate.PeerFolders[peerID][changes.FolderID], change.Path)
			delete(candidate.PeerRevisions[peerID][changes.FolderID], change.Path)
			candidate.PeerTombstones[peerID][changes.FolderID][change.Path] = change.Revision
		case ChangeMove:
			if change.FromPath == "" {
				return fmt.Errorf("peer metadata move %s missing fromPath", change.Path)
			}
			if change.Manifest == nil {
				return fmt.Errorf("peer metadata move %s missing manifest", change.Path)
			}
			delete(candidate.PeerFolders[peerID][changes.FolderID], change.FromPath)
			delete(candidate.PeerRevisions[peerID][changes.FolderID], change.FromPath)
			candidate.PeerTombstones[peerID][changes.FolderID][change.FromPath] = change.Revision
			candidate.PeerFolders[peerID][changes.FolderID][change.Path] = *change.Manifest
			candidate.PeerRevisions[peerID][changes.FolderID][change.Path] = change.Revision
			candidate.PeerRenameHints[peerID][changes.FolderID][change.FromPath] = RenameHint{FromPath: change.FromPath, ToPath: change.Path, Revision: change.Revision}
		default:
			return fmt.Errorf("unknown peer metadata change kind %q", change.Kind)
		}
	}
	candidate.PeerCursors[peerID][changes.FolderID] = changes.ToCursor
	summary, err := peerFolderSummary(candidate, peerID, changes.FolderID)
	if err != nil {
		return err
	}
	if summary.StateHash != changes.StateHash {
		return fmt.Errorf("peer metadata state hash mismatch for %s/%s", peerID, changes.FolderID)
	}
	checkpoint := PeerApplyCheckpoint{FolderID: changes.FolderID, FromCursor: changes.FromCursor, ToCursor: changes.ToCursor, ChangeCount: len(changes.Changes), LastVerifiedCursor: summary.Cursor, LastVerifiedStateHash: summary.StateHash}
	return b.db.Update(func(txn *badger.Txn) error {
		for _, change := range changes.Changes {
			switch change.Kind {
			case ChangeUpsert:
				if err := badgerDeletePeerPathRecords(txn, peerID, changes.FolderID, change.Path); err != nil {
					return err
				}
				if err := badgerSetJSON(txn, badgerPeerManifestKey(peerID, changes.FolderID, change.Path), *change.Manifest); err != nil {
					return err
				}
				if err := badgerSetJSON(txn, badgerPeerRevisionKey(peerID, changes.FolderID, change.Path), change.Revision); err != nil {
					return err
				}
			case ChangeDelete:
				if err := badgerDeletePeerPathRecords(txn, peerID, changes.FolderID, change.Path); err != nil {
					return err
				}
				if err := badgerSetJSON(txn, badgerPeerTombstoneKey(peerID, changes.FolderID, change.Path), change.Revision); err != nil {
					return err
				}
			case ChangeMove:
				if err := badgerDeletePeerPathRecords(txn, peerID, changes.FolderID, change.FromPath); err != nil {
					return err
				}
				if err := badgerDeletePeerPathRecords(txn, peerID, changes.FolderID, change.Path); err != nil {
					return err
				}
				if err := badgerSetJSON(txn, badgerPeerTombstoneKey(peerID, changes.FolderID, change.FromPath), change.Revision); err != nil {
					return err
				}
				if err := badgerSetJSON(txn, badgerPeerManifestKey(peerID, changes.FolderID, change.Path), *change.Manifest); err != nil {
					return err
				}
				if err := badgerSetJSON(txn, badgerPeerRevisionKey(peerID, changes.FolderID, change.Path), change.Revision); err != nil {
					return err
				}
				if err := badgerSetJSON(txn, badgerPeerRenameHintKey(peerID, changes.FolderID, change.FromPath), RenameHint{FromPath: change.FromPath, ToPath: change.Path, Revision: change.Revision}); err != nil {
					return err
				}
			}
		}
		if err := badgerSetJSON(txn, badgerPeerCursorKey(peerID, changes.FolderID), changes.ToCursor); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerPeerStateKey(peerID, changes.FolderID), summary); err != nil {
			return err
		}
		return badgerSetJSON(txn, badgerPeerApplyCheckpointKey(peerID, changes.FolderID), checkpoint)
	})
}

func (b badgerSnapshotBackend) ReplacePeerFolderFromFullRefresh(peerID string, folderID string, summary FolderSummary, manifests map[string]block.Manifest, revisions map[string]uint64) error {
	if peerID == "" {
		return fmt.Errorf("peer id required")
	}
	if folderID == "" {
		return fmt.Errorf("folder id required")
	}
	if summary.FolderID == "" {
		summary.FolderID = folderID
	}
	if summary.FolderID != folderID {
		return fmt.Errorf("summary folder %s does not match %s", summary.FolderID, folderID)
	}
	checkpoint := PeerApplyCheckpoint{FolderID: folderID, FromCursor: 0, ToCursor: summary.Cursor, ChangeCount: len(manifests), LastVerifiedCursor: summary.Cursor, LastVerifiedStateHash: summary.StateHash}
	return b.db.Update(func(txn *badger.Txn) error {
		if err := badgerDeletePeerFolderRecords(txn, peerID, folderID); err != nil {
			return err
		}
		for rel, manifest := range manifests {
			rev := revisions[rel]
			if rev == 0 {
				rev = summary.Cursor
			}
			if err := badgerSetJSON(txn, badgerPeerManifestKey(peerID, folderID, rel), manifest); err != nil {
				return err
			}
			if err := badgerSetJSON(txn, badgerPeerRevisionKey(peerID, folderID, rel), rev); err != nil {
				return err
			}
		}
		if err := badgerSetJSON(txn, badgerPeerCursorKey(peerID, folderID), summary.Cursor); err != nil {
			return err
		}
		if err := badgerSetJSON(txn, badgerPeerStateKey(peerID, folderID), summary); err != nil {
			return err
		}
		return badgerSetJSON(txn, badgerPeerApplyCheckpointKey(peerID, folderID), checkpoint)
	})
}

func (b badgerSnapshotBackend) CompactFolderMetadata(folderID string, policy MetadataCompactionPolicy) (MetadataCompactionResult, error) {
	snap, err := b.Load()
	if err != nil {
		return MetadataCompactionResult{}, err
	}
	plan := metadataCompactionPlan(snap, folderID, policy)
	before, err := folderSummary(snap, folderID)
	if err != nil {
		return MetadataCompactionResult{}, err
	}
	compactedPaths := make([]string, 0)
	for rel, rev := range snap.Tombstones[folderID] {
		if rev <= plan.SafeCursor {
			compactedPaths = append(compactedPaths, rel)
		}
	}
	sort.Strings(compactedPaths)
	compactionSnapshot := MetadataCompactionSnapshot{FolderID: folderID, Cursor: before.Cursor, StateHash: before.StateHash, Files: before.Files, Tombstones: before.Tombstones, SafeCursor: plan.SafeCursor, CompactedTombstones: len(compactedPaths)}
	if len(compactedPaths) == 0 {
		return MetadataCompactionResult{Plan: plan, Snapshot: compactionSnapshot}, nil
	}
	if err := b.db.Update(func(txn *badger.Txn) error {
		for _, rel := range compactedPaths {
			if err := txn.Delete(badgerTombstoneKey(folderID, rel)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
		}
		index, err := badgerNextCompactionSnapshotIndex(txn, folderID)
		if err != nil {
			return err
		}
		return badgerSetJSON(txn, badgerCompactionSnapshotKey(folderID, index), compactionSnapshot)
	}); err != nil {
		return MetadataCompactionResult{}, err
	}
	return MetadataCompactionResult{Plan: plan, Snapshot: compactionSnapshot, CompactedTombstones: len(compactedPaths)}, nil
}

func (b badgerSnapshotBackend) MetadataCompactionSnapshots(folderID string) ([]MetadataCompactionSnapshot, error) {
	snapshots := make([]MetadataCompactionSnapshot, 0)
	prefix := []byte(badgerKeyPrefix + "compaction-snapshot/" + badgerEncodePart(folderID) + "/")
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var snapshot MetadataCompactionSnapshot
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &snapshot) }); err != nil {
				return err
			}
			snapshots = append(snapshots, snapshot)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Cursor < snapshots[j].Cursor })
	return snapshots, nil
}

func (b badgerSnapshotBackend) SavePendingWrite(write PendingWrite) error {
	if write.FolderID == "" {
		return fmt.Errorf("folder id required")
	}
	if write.Path == "" {
		return fmt.Errorf("pending write path required")
	}
	write = clonePendingWrite(write)
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerPendingWriteKey(write.FolderID, write.Path), write)
	})
}

func (b badgerSnapshotBackend) PendingWrite(folderID string, path string) (PendingWrite, bool, error) {
	var write PendingWrite
	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerPendingWriteKey(folderID, path))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return item.Value(func(data []byte) error { return json.Unmarshal(data, &write) })
	})
	if err != nil {
		return PendingWrite{}, false, err
	}
	if !found {
		return PendingWrite{}, false, nil
	}
	return clonePendingWrite(write), true, nil
}

func (b badgerSnapshotBackend) PendingWrites(folderID string) ([]PendingWrite, error) {
	writes := make([]PendingWrite, 0)
	prefix := []byte(badgerKeyPrefix + "pending-write/" + badgerEncodePart(folderID) + "/")
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var write PendingWrite
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &write) }); err != nil {
				return err
			}
			writes = append(writes, clonePendingWrite(write))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(writes, func(i, j int) bool { return writes[i].Path < writes[j].Path })
	return writes, nil
}

func (b badgerSnapshotBackend) AddVerifiedStagedBlock(folderID string, path string, verified VerifiedStagedBlock) error {
	return b.db.Update(func(txn *badger.Txn) error {
		key := badgerPendingWriteKey(folderID, path)
		write, err := badgerPendingWriteByKey(txn, key)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("pending write %s/%s not found", folderID, path)
		}
		if err != nil {
			return err
		}
		write.VerifiedBlocks = append(write.VerifiedBlocks, verified)
		return badgerSetJSON(txn, key, write)
	})
}

func (b badgerSnapshotBackend) MarkPendingWriteCommitted(folderID string, path string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		key := badgerPendingWriteKey(folderID, path)
		write, err := badgerPendingWriteByKey(txn, key)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("pending write %s/%s not found", folderID, path)
		}
		if err != nil {
			return err
		}
		write.Committed = true
		return badgerSetJSON(txn, key, write)
	})
}

func (b badgerSnapshotBackend) RemovePendingWrite(folderID string, path string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete(badgerPendingWriteKey(folderID, path))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		return err
	})
}

func (b badgerSnapshotBackend) SaveSkippedDelete(delete SkippedDelete) error {
	if delete.FolderID == "" {
		return fmt.Errorf("folder id required")
	}
	if delete.Path == "" {
		return fmt.Errorf("skipped delete path required")
	}
	delete.RequiredWrites = append([]string(nil), delete.RequiredWrites...)
	sort.Strings(delete.RequiredWrites)
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerSetJSON(txn, badgerSkippedDeleteKey(delete.FolderID, delete.Path), delete)
	})
}

func (b badgerSnapshotBackend) ReadySkippedDeletes(folderID string, current FolderSummary) ([]SkippedDelete, error) {
	deletes, err := b.SkippedDeletes(folderID)
	if err != nil {
		return nil, err
	}
	writes, err := b.pendingWriteMap(folderID)
	if err != nil {
		return nil, err
	}
	ready := make([]SkippedDelete, 0)
	for _, delete := range deletes {
		if !metadataPrerequisitesMet(delete, current) {
			continue
		}
		if !requiredWritesCommitted(writes, delete.RequiredWrites) {
			continue
		}
		delete.RequiredWrites = append([]string(nil), delete.RequiredWrites...)
		ready = append(ready, delete)
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].Path < ready[j].Path })
	return ready, nil
}

func (b badgerSnapshotBackend) SkippedDeletes(folderID string) ([]SkippedDelete, error) {
	deletes := make([]SkippedDelete, 0)
	prefix := []byte(badgerKeyPrefix + "skipped-delete/" + badgerEncodePart(folderID) + "/")
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			var delete SkippedDelete
			if err := it.Item().Value(func(data []byte) error { return json.Unmarshal(data, &delete) }); err != nil {
				return err
			}
			delete.RequiredWrites = append([]string(nil), delete.RequiredWrites...)
			deletes = append(deletes, delete)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(deletes, func(i, j int) bool { return deletes[i].Path < deletes[j].Path })
	return deletes, nil
}

func (b badgerSnapshotBackend) RemoveSkippedDelete(folderID string, path string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete(badgerSkippedDeleteKey(folderID, path))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		return err
	})
}

func (b badgerSnapshotBackend) pendingWriteMap(folderID string) (map[string]PendingWrite, error) {
	writes, err := b.PendingWrites(folderID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PendingWrite, len(writes))
	for _, write := range writes {
		out[write.Path] = write
	}
	return out, nil
}

func badgerPendingWriteByKey(txn *badger.Txn, key []byte) (PendingWrite, error) {
	item, err := txn.Get(key)
	if err != nil {
		return PendingWrite{}, err
	}
	var write PendingWrite
	if err := item.Value(func(data []byte) error { return json.Unmarshal(data, &write) }); err != nil {
		return PendingWrite{}, err
	}
	return clonePendingWrite(write), nil
}

func badgerNextCompactionSnapshotIndex(txn *badger.Txn, folderID string) (int, error) {
	prefix := []byte(badgerKeyPrefix + "compaction-snapshot/" + badgerEncodePart(folderID) + "/")
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	it := txn.NewIterator(opts)
	defer it.Close()
	maxIndex := -1
	for it.Rewind(); it.Valid(); it.Next() {
		parts := strings.Split(string(it.Item().Key()), "/")
		if len(parts) == 4 {
			index, err := strconv.Atoi(parts[3])
			if err != nil {
				return 0, err
			}
			if index > maxIndex {
				maxIndex = index
			}
		}
	}
	return maxIndex + 1, nil
}

func clonePendingWrite(write PendingWrite) PendingWrite {
	if write.ExpectedBaseManifest != nil {
		base := *write.ExpectedBaseManifest
		write.ExpectedBaseManifest = &base
	}
	write.VerifiedBlocks = append([]VerifiedStagedBlock(nil), write.VerifiedBlocks...)
	return write
}

func badgerNextFolderRevision(txn *badger.Txn, folderID string) (uint64, error) {
	cursor, err := badgerReadUint64(txn, badgerCursorKey(folderID))
	if err != nil {
		return 0, err
	}
	return cursor + 1, nil
}

func badgerReadUint64(txn *badger.Txn, key []byte) (uint64, error) {
	item, err := txn.Get(key)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var value uint64
	if err := item.Value(func(data []byte) error { return json.Unmarshal(data, &value) }); err != nil {
		return 0, err
	}
	return value, nil
}

func badgerDeleteManifestRecords(txn *badger.Txn, folderID string, relativePath string) error {
	var existing block.Manifest
	item, err := txn.Get(badgerManifestKey(folderID, relativePath))
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	if err == nil {
		if err := item.Value(func(data []byte) error { return json.Unmarshal(data, &existing) }); err != nil {
			return err
		}
		for _, block := range existing.Blocks {
			if err := txn.Delete(badgerBlockIndexKey(block.Size, block.Hash, folderID, relativePath, block.Index)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
		}
	}
	for _, key := range [][]byte{badgerManifestKey(folderID, relativePath), badgerRevisionKey(folderID, relativePath)} {
		if err := txn.Delete(key); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
	}
	return nil
}

func badgerDeletePeerPathRecords(txn *badger.Txn, peerID string, folderID string, relativePath string) error {
	for _, key := range [][]byte{
		badgerPeerManifestKey(peerID, folderID, relativePath),
		badgerPeerRevisionKey(peerID, folderID, relativePath),
		badgerPeerTombstoneKey(peerID, folderID, relativePath),
		badgerPeerRenameHintKey(peerID, folderID, relativePath),
	} {
		if err := txn.Delete(key); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
	}
	return nil
}

func badgerDeletePeerFolderRecords(txn *badger.Txn, peerID string, folderID string) error {
	prefixes := [][]byte{
		[]byte(badgerKeyPrefix + "peer-manifest/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/"),
		[]byte(badgerKeyPrefix + "peer-revision/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/"),
		[]byte(badgerKeyPrefix + "peer-tombstone/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/"),
		[]byte(badgerKeyPrefix + "peer-rename/" + badgerEncodePart(peerID) + "/" + badgerEncodePart(folderID) + "/"),
	}
	for _, prefix := range prefixes {
		if err := badgerDeletePrefix(txn, prefix); err != nil {
			return err
		}
	}
	for _, key := range [][]byte{
		badgerPeerStateKey(peerID, folderID),
		badgerPeerCursorKey(peerID, folderID),
		badgerPeerApplyCheckpointKey(peerID, folderID),
	} {
		if err := txn.Delete(key); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
	}
	return nil
}

func badgerDeletePrefix(txn *badger.Txn, prefix []byte) error {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	it := txn.NewIterator(opts)
	defer it.Close()
	keys := make([][]byte, 0)
	for it.Rewind(); it.Valid(); it.Next() {
		keys = append(keys, append([]byte(nil), it.Item().Key()...))
	}
	for _, key := range keys {
		if err := txn.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

func badgerClearKeyspace(txn *badger.Txn) error {
	if err := txn.Delete(badgerSnapshotKey); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	opts := badger.DefaultIteratorOptions
	opts.Prefix = []byte(badgerKeyPrefix)
	it := txn.NewIterator(opts)
	defer it.Close()
	keys := make([][]byte, 0)
	for it.Rewind(); it.Valid(); it.Next() {
		key := append([]byte(nil), it.Item().Key()...)
		keys = append(keys, key)
	}
	for _, key := range keys {
		if err := txn.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

func badgerSetJSON(txn *badger.Txn, key []byte, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return txn.Set(key, data)
}

func badgerWriteSnapshot(txn *badger.Txn, snap snapshot) error {
	for folderID, folder := range snap.Folders {
		for rel, manifest := range folder {
			if err := badgerSetJSON(txn, badgerManifestKey(folderID, rel), manifest); err != nil {
				return err
			}
			for _, b := range manifest.Blocks {
				ref := BlockRef{FolderID: folderID, RelativePath: rel, Block: b}
				if err := badgerSetJSON(txn, badgerBlockIndexKey(b.Size, b.Hash, folderID, rel, b.Index), ref); err != nil {
					return err
				}
			}
		}
	}
	for folderID, paths := range snap.ManifestHistory {
		for rel, history := range paths {
			for rev, manifest := range history {
				if err := badgerSetJSON(txn, badgerManifestHistoryKey(folderID, rel, rev), manifest); err != nil {
					return err
				}
			}
		}
	}
	for folderID, revisions := range snap.Revisions {
		for rel, rev := range revisions {
			if err := badgerSetJSON(txn, badgerRevisionKey(folderID, rel), rev); err != nil {
				return err
			}
		}
	}
	for folderID, tombstones := range snap.Tombstones {
		for rel, rev := range tombstones {
			if err := badgerSetJSON(txn, badgerTombstoneKey(folderID, rel), rev); err != nil {
				return err
			}
		}
	}
	for folderID, hints := range snap.RenameHints {
		for from, hint := range hints {
			if err := badgerSetJSON(txn, badgerRenameHintKey(folderID, from), hint); err != nil {
				return err
			}
		}
	}
	for folderID, cursor := range snap.Cursors {
		if err := badgerSetJSON(txn, badgerCursorKey(folderID), cursor); err != nil {
			return err
		}
	}
	for peerID, folders := range snap.PeerStates {
		for folderID, summary := range folders {
			if err := badgerSetJSON(txn, badgerPeerStateKey(peerID, folderID), summary); err != nil {
				return err
			}
		}
	}
	for peerID, folders := range snap.PeerFolders {
		for folderID, manifests := range folders {
			for rel, manifest := range manifests {
				if err := badgerSetJSON(txn, badgerPeerManifestKey(peerID, folderID, rel), manifest); err != nil {
					return err
				}
			}
		}
	}
	for peerID, folders := range snap.PeerRevisions {
		for folderID, revisions := range folders {
			for rel, rev := range revisions {
				if err := badgerSetJSON(txn, badgerPeerRevisionKey(peerID, folderID, rel), rev); err != nil {
					return err
				}
			}
		}
	}
	for peerID, folders := range snap.PeerTombstones {
		for folderID, tombstones := range folders {
			for rel, rev := range tombstones {
				if err := badgerSetJSON(txn, badgerPeerTombstoneKey(peerID, folderID, rel), rev); err != nil {
					return err
				}
			}
		}
	}
	for peerID, folders := range snap.PeerRenameHints {
		for folderID, hints := range folders {
			for from, hint := range hints {
				if err := badgerSetJSON(txn, badgerPeerRenameHintKey(peerID, folderID, from), hint); err != nil {
					return err
				}
			}
		}
	}
	for peerID, folders := range snap.PeerCursors {
		for folderID, cursor := range folders {
			if err := badgerSetJSON(txn, badgerPeerCursorKey(peerID, folderID), cursor); err != nil {
				return err
			}
		}
	}
	for peerID, folders := range snap.PeerApplyCheckpoints {
		for folderID, checkpoint := range folders {
			if err := badgerSetJSON(txn, badgerPeerApplyCheckpointKey(peerID, folderID), checkpoint); err != nil {
				return err
			}
		}
	}
	for folderID, snapshots := range snap.CompactionSnapshots {
		for i, compactionSnapshot := range snapshots {
			if err := badgerSetJSON(txn, badgerCompactionSnapshotKey(folderID, i), compactionSnapshot); err != nil {
				return err
			}
		}
	}
	for id, marker := range snap.SnapshotMarkers {
		if err := badgerSetJSON(txn, badgerSnapshotMarkerKey(id), marker); err != nil {
			return err
		}
	}
	for snapshotID, jobs := range snap.ArchiveIntakeJobs {
		for _, job := range jobs {
			if err := badgerSetJSON(txn, badgerArchiveIntakeJobKey(snapshotID, job.ID), job); err != nil {
				return err
			}
		}
	}
	for id, job := range snap.BackupRestoreJobs {
		if err := badgerSetJSON(txn, badgerBackupRestoreJobKey(id), job); err != nil {
			return err
		}
	}
	for id, job := range snap.BackupRetentionJobs {
		if err := badgerSetJSON(txn, badgerBackupRetentionJobKey(id), job); err != nil {
			return err
		}
	}
	for id, job := range snap.BackupRepairJobs {
		if err := badgerSetJSON(txn, badgerBackupRepairJobKey(id), cloneBackupRepairJob(job)); err != nil {
			return err
		}
	}
	for folderID, writes := range snap.PendingWrites {
		for path, write := range writes {
			if err := badgerSetJSON(txn, badgerPendingWriteKey(folderID, path), write); err != nil {
				return err
			}
		}
	}
	for folderID, deletes := range snap.SkippedDeletes {
		for path, delete := range deletes {
			if err := badgerSetJSON(txn, badgerSkippedDeleteKey(folderID, path), delete); err != nil {
				return err
			}
		}
	}
	for nodeID, doc := range snap.NodeSettings {
		if err := badgerSetJSON(txn, badgerNodeSettingsKey(nodeID), doc); err != nil {
			return err
		}
	}
	for targetNodeID, changes := range snap.PendingSettings {
		for changeID, change := range changes {
			if err := badgerSetJSON(txn, badgerPendingSettingsKey(targetNodeID, changeID), change); err != nil {
				return err
			}
		}
	}
	return nil
}

func badgerLoadKeyValue(snap *snapshot, key string, data []byte) error {
	relativeKey := strings.TrimPrefix(key, badgerKeyPrefix)
	parts := strings.Split(relativeKey, "/")
	if len(parts) == 0 {
		return nil
	}
	switch parts[0] {
	case "manifest-history":
		if len(parts) != 4 {
			return fmt.Errorf("invalid badger manifest history key %q", key)
		}
		folderID, rel, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		rev, err := strconv.ParseUint(parts[3], 10, 64)
		if err != nil {
			return err
		}
		ensureFolderState(snap, folderID)
		if snap.ManifestHistory[folderID][rel] == nil {
			snap.ManifestHistory[folderID][rel] = map[uint64]block.Manifest{}
		}
		var manifest block.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
		snap.ManifestHistory[folderID][rel][rev] = manifest
	case "manifest":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger manifest key %q", key)
		}
		folderID, rel, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		ensureFolderState(snap, folderID)
		var manifest block.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
		snap.Folders[folderID][rel] = manifest
	case "revision":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger revision key %q", key)
		}
		folderID, rel, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		ensureFolderState(snap, folderID)
		var rev uint64
		if err := json.Unmarshal(data, &rev); err != nil {
			return err
		}
		snap.Revisions[folderID][rel] = rev
	case "tombstone":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger tombstone key %q", key)
		}
		folderID, rel, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		ensureFolderState(snap, folderID)
		var rev uint64
		if err := json.Unmarshal(data, &rev); err != nil {
			return err
		}
		snap.Tombstones[folderID][rel] = rev
	case "rename":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger rename key %q", key)
		}
		folderID, from, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		ensureFolderState(snap, folderID)
		var hint RenameHint
		if err := json.Unmarshal(data, &hint); err != nil {
			return err
		}
		snap.RenameHints[folderID][from] = hint
	case "cursor":
		if len(parts) != 2 {
			return fmt.Errorf("invalid badger cursor key %q", key)
		}
		folderID, err := badgerDecodePart(parts[1])
		if err != nil {
			return err
		}
		ensureFolderState(snap, folderID)
		var cursor uint64
		if err := json.Unmarshal(data, &cursor); err != nil {
			return err
		}
		snap.Cursors[folderID] = cursor
	case "peer-state":
		peerID, folderID, err := badgerDecodePeerFolderKey(parts, key)
		if err != nil {
			return err
		}
		ensurePeerFolderState(snap, peerID, folderID)
		var summary FolderSummary
		if err := json.Unmarshal(data, &summary); err != nil {
			return err
		}
		snap.PeerStates[peerID][folderID] = summary
	case "peer-manifest":
		peerID, folderID, rel, err := badgerDecodePeerFolderPathKey(parts, key)
		if err != nil {
			return err
		}
		ensurePeerFolderState(snap, peerID, folderID)
		var manifest block.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
		snap.PeerFolders[peerID][folderID][rel] = manifest
	case "peer-revision":
		peerID, folderID, rel, err := badgerDecodePeerFolderPathKey(parts, key)
		if err != nil {
			return err
		}
		ensurePeerFolderState(snap, peerID, folderID)
		var rev uint64
		if err := json.Unmarshal(data, &rev); err != nil {
			return err
		}
		snap.PeerRevisions[peerID][folderID][rel] = rev
	case "peer-tombstone":
		peerID, folderID, rel, err := badgerDecodePeerFolderPathKey(parts, key)
		if err != nil {
			return err
		}
		ensurePeerFolderState(snap, peerID, folderID)
		var rev uint64
		if err := json.Unmarshal(data, &rev); err != nil {
			return err
		}
		snap.PeerTombstones[peerID][folderID][rel] = rev
	case "peer-rename":
		peerID, folderID, from, err := badgerDecodePeerFolderPathKey(parts, key)
		if err != nil {
			return err
		}
		ensurePeerFolderState(snap, peerID, folderID)
		var hint RenameHint
		if err := json.Unmarshal(data, &hint); err != nil {
			return err
		}
		snap.PeerRenameHints[peerID][folderID][from] = hint
	case "peer-cursor":
		peerID, folderID, err := badgerDecodePeerFolderKey(parts, key)
		if err != nil {
			return err
		}
		ensurePeerFolderState(snap, peerID, folderID)
		var cursor uint64
		if err := json.Unmarshal(data, &cursor); err != nil {
			return err
		}
		snap.PeerCursors[peerID][folderID] = cursor
	case "peer-checkpoint":
		peerID, folderID, err := badgerDecodePeerFolderKey(parts, key)
		if err != nil {
			return err
		}
		ensurePeerFolderState(snap, peerID, folderID)
		var checkpoint PeerApplyCheckpoint
		if err := json.Unmarshal(data, &checkpoint); err != nil {
			return err
		}
		snap.PeerApplyCheckpoints[peerID][folderID] = checkpoint
	case "compaction-snapshot":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger compaction-snapshot key %q", key)
		}
		folderID, err := badgerDecodePart(parts[1])
		if err != nil {
			return err
		}
		var compactionSnapshot MetadataCompactionSnapshot
		if err := json.Unmarshal(data, &compactionSnapshot); err != nil {
			return err
		}
		if snap.CompactionSnapshots == nil {
			snap.CompactionSnapshots = map[string][]MetadataCompactionSnapshot{}
		}
		snap.CompactionSnapshots[folderID] = append(snap.CompactionSnapshots[folderID], compactionSnapshot)
	case "snapshot-marker":
		if len(parts) != 2 {
			return fmt.Errorf("invalid badger snapshot-marker key %q", key)
		}
		var marker SnapshotMarker
		if err := json.Unmarshal(data, &marker); err != nil {
			return err
		}
		if snap.SnapshotMarkers == nil {
			snap.SnapshotMarkers = map[string]SnapshotMarker{}
		}
		snap.SnapshotMarkers[marker.ID] = marker
	case "archive-intake":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger archive-intake key %q", key)
		}
		snapshotID, err := badgerDecodePart(parts[1])
		if err != nil {
			return err
		}
		var job ArchiveIntakeJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		if snap.ArchiveIntakeJobs == nil {
			snap.ArchiveIntakeJobs = map[string][]ArchiveIntakeJob{}
		}
		snap.ArchiveIntakeJobs[snapshotID] = append(snap.ArchiveIntakeJobs[snapshotID], job)
	case "backup-restore-job":
		if len(parts) != 2 {
			return fmt.Errorf("invalid badger backup-restore-job key %q", key)
		}
		var job BackupRestoreJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		if snap.BackupRestoreJobs == nil {
			snap.BackupRestoreJobs = map[string]BackupRestoreJob{}
		}
		snap.BackupRestoreJobs[job.ID] = job
	case "backup-retention-job":
		if len(parts) != 2 {
			return fmt.Errorf("invalid badger backup-retention-job key %q", key)
		}
		var job BackupRetentionJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		if snap.BackupRetentionJobs == nil {
			snap.BackupRetentionJobs = map[string]BackupRetentionJob{}
		}
		snap.BackupRetentionJobs[job.ID] = job
	case "backup-repair-job":
		if len(parts) != 2 {
			return fmt.Errorf("invalid badger backup-repair-job key %q", key)
		}
		var job BackupRepairJob
		if err := json.Unmarshal(data, &job); err != nil {
			return err
		}
		if snap.BackupRepairJobs == nil {
			snap.BackupRepairJobs = map[string]BackupRepairJob{}
		}
		snap.BackupRepairJobs[job.ID] = cloneBackupRepairJob(job)
	case "pending-write":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger pending-write key %q", key)
		}
		folderID, path, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		ensureApplyGateState(snap, folderID)
		var write PendingWrite
		if err := json.Unmarshal(data, &write); err != nil {
			return err
		}
		snap.PendingWrites[folderID][path] = write
	case "skipped-delete":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger skipped-delete key %q", key)
		}
		folderID, path, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		ensureApplyGateState(snap, folderID)
		var delete SkippedDelete
		if err := json.Unmarshal(data, &delete); err != nil {
			return err
		}
		snap.SkippedDeletes[folderID][path] = delete
	case "node-settings":
		if len(parts) != 2 {
			return fmt.Errorf("invalid badger node-settings key %q", key)
		}
		nodeID, err := badgerDecodePart(parts[1])
		if err != nil {
			return err
		}
		var doc NodeSettingsDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		if doc.NodeID != nodeID {
			return fmt.Errorf("node settings document owner mismatch: key %q document %q", nodeID, doc.NodeID)
		}
		if snap.NodeSettings == nil {
			snap.NodeSettings = map[string]NodeSettingsDocument{}
		}
		snap.NodeSettings[nodeID] = doc
	case "pending-settings":
		if len(parts) != 3 {
			return fmt.Errorf("invalid badger pending-settings key %q", key)
		}
		targetNodeID, changeID, err := badgerDecodeTwo(parts[1], parts[2])
		if err != nil {
			return err
		}
		var change PendingSettingsChange
		if err := json.Unmarshal(data, &change); err != nil {
			return err
		}
		if change.TargetNodeID != targetNodeID || change.ID != changeID {
			return fmt.Errorf("pending settings change owner mismatch: key %q/%q change %q/%q", targetNodeID, changeID, change.TargetNodeID, change.ID)
		}
		if snap.PendingSettings == nil {
			snap.PendingSettings = map[string]map[string]PendingSettingsChange{}
		}
		if snap.PendingSettings[targetNodeID] == nil {
			snap.PendingSettings[targetNodeID] = map[string]PendingSettingsChange{}
		}
		snap.PendingSettings[targetNodeID][changeID] = change
	case "block":
		return nil
	default:
		return nil
	}
	return nil
}

func badgerDecodeTwo(a string, b string) (string, string, error) {
	first, err := badgerDecodePart(a)
	if err != nil {
		return "", "", err
	}
	second, err := badgerDecodePart(b)
	if err != nil {
		return "", "", err
	}
	return first, second, nil
}

func badgerDecodePeerFolderKey(parts []string, key string) (string, string, error) {
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid badger peer/folder key %q", key)
	}
	return badgerDecodeTwo(parts[1], parts[2])
}

func badgerDecodePeerFolderPathKey(parts []string, key string) (string, string, string, error) {
	if len(parts) != 4 {
		return "", "", "", fmt.Errorf("invalid badger peer/folder/path key %q", key)
	}
	peerID, folderID, err := badgerDecodeTwo(parts[1], parts[2])
	if err != nil {
		return "", "", "", err
	}
	rel, err := badgerDecodePart(parts[3])
	if err != nil {
		return "", "", "", err
	}
	return peerID, folderID, rel, nil
}
