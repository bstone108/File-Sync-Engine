package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dgraph-io/badger/v4"

	"filesyncengine/internal/block"
)

type JSONStore struct {
	backend snapshotBackend
}

type snapshotBackend interface {
	Load() (snapshot, error)
	Save(snapshot) error
	Close() error
}

type blockIndexBackend interface {
	FindBlocks(size int, hash []byte) ([]BlockRef, error)
	ListBlockRefs(folderID string) ([]BlockRef, error)
}

type fileSnapshotBackend struct {
	path string
}

type badgerSnapshotBackend struct {
	db *badger.DB
}

type perFolderBadgerBackend struct {
	stores map[string]JSONStore
}

var badgerSnapshotKey = []byte("state/snapshot/v1")

type snapshot struct {
	Folders              map[string]map[string]block.Manifest            `json:"folders"`
	ManifestHistory      map[string]map[string]map[uint64]block.Manifest `json:"manifestHistory,omitempty"`
	Revisions            map[string]map[string]uint64                    `json:"revisions,omitempty"`
	Tombstones           map[string]map[string]uint64                    `json:"tombstones,omitempty"`
	RenameHints          map[string]map[string]RenameHint                `json:"renameHints,omitempty"`
	Cursors              map[string]uint64                               `json:"cursors,omitempty"`
	PeerStates           map[string]map[string]FolderSummary             `json:"peerStates,omitempty"`
	PeerFolders          map[string]map[string]map[string]block.Manifest `json:"peerFolders,omitempty"`
	PeerRevisions        map[string]map[string]map[string]uint64         `json:"peerRevisions,omitempty"`
	PeerTombstones       map[string]map[string]map[string]uint64         `json:"peerTombstones,omitempty"`
	PeerRenameHints      map[string]map[string]map[string]RenameHint     `json:"peerRenameHints,omitempty"`
	PeerCursors          map[string]map[string]uint64                    `json:"peerCursors,omitempty"`
	PeerApplyCheckpoints map[string]map[string]PeerApplyCheckpoint       `json:"peerApplyCheckpoints,omitempty"`
	CompactionSnapshots  map[string][]MetadataCompactionSnapshot         `json:"compactionSnapshots,omitempty"`
	SnapshotMarkers      map[string]SnapshotMarker                       `json:"snapshotMarkers,omitempty"`
	ArchiveIntakeJobs    map[string][]ArchiveIntakeJob                   `json:"archiveIntakeJobs,omitempty"`
	BackupRestoreJobs    map[string]BackupRestoreJob                     `json:"backupRestoreJobs,omitempty"`
	BackupRetentionJobs  map[string]BackupRetentionJob                   `json:"backupRetentionJobs,omitempty"`
	BackupRepairJobs     map[string]BackupRepairJob                      `json:"backupRepairJobs,omitempty"`
	PendingWrites        map[string]map[string]PendingWrite              `json:"pendingWrites,omitempty"`
	SkippedDeletes       map[string]map[string]SkippedDelete             `json:"skippedDeletes,omitempty"`
	NodeSettings         map[string]NodeSettingsDocument                 `json:"nodeSettings,omitempty"`
	PendingSettings      map[string]map[string]PendingSettingsChange     `json:"pendingSettings,omitempty"`
}

type BlockRef struct {
	FolderID     string
	RelativePath string
	Block        block.Block
}

type ChangeKind string

const (
	ChangeUpsert ChangeKind = "upsert"
	ChangeDelete ChangeKind = "delete"
	ChangeMove   ChangeKind = "move"
)

type RenameHint struct {
	FromPath string `json:"fromPath"`
	ToPath   string `json:"toPath"`
	Revision uint64 `json:"revision"`
}

type FolderSummary struct {
	FolderID   string `json:"folderId"`
	Cursor     uint64 `json:"cursor"`
	Files      int    `json:"files"`
	Tombstones int    `json:"tombstones"`
	StateHash  string `json:"stateHash"`
}

type FolderChange struct {
	Kind     ChangeKind      `json:"kind"`
	FromPath string          `json:"fromPath,omitempty"`
	Path     string          `json:"path"`
	Revision uint64          `json:"revision"`
	Manifest *block.Manifest `json:"manifest,omitempty"`
}

type FolderChanges struct {
	FolderID   string         `json:"folderId"`
	FromCursor uint64         `json:"fromCursor"`
	ToCursor   uint64         `json:"toCursor"`
	StateHash  string         `json:"stateHash"`
	Changes    []FolderChange `json:"changes"`
}

type PeerStateVector struct {
	PeerID  string          `json:"peerId"`
	Folders []FolderSummary `json:"folders"`
}

type PeerFolderStatus struct {
	PeerID         string `json:"peerId"`
	FolderID       string `json:"folderId"`
	PeerCursor     uint64 `json:"peerCursor"`
	PeerStateHash  string `json:"peerStateHash"`
	LocalCursor    uint64 `json:"localCursor"`
	LocalStateHash string `json:"localStateHash"`
	InSync         bool   `json:"inSync"`
}

type PeerApplyCheckpoint struct {
	FolderID              string `json:"folderId"`
	FromCursor            uint64 `json:"fromCursor"`
	ToCursor              uint64 `json:"toCursor"`
	ChangeCount           int    `json:"changeCount"`
	LastVerifiedCursor    uint64 `json:"lastVerifiedCursor"`
	LastVerifiedStateHash string `json:"lastVerifiedStateHash"`
}

type NodeSettingsDocument struct {
	NodeID      string         `json:"nodeId"`
	Revision    uint64         `json:"revision"`
	UpdatedAt   string         `json:"updatedAt,omitempty"`
	Settings    map[string]any `json:"settings,omitempty"`
	Source      string         `json:"source,omitempty"`
	ApplyStatus string         `json:"applyStatus,omitempty"`
}

type PendingSettingsChange struct {
	ID             string                      `json:"id"`
	TargetNodeID   string                      `json:"targetNodeId"`
	OriginNodeID   string                      `json:"originNodeId"`
	IdempotencyKey string                      `json:"idempotencyKey"`
	Revision       uint64                      `json:"revision"`
	Status         string                      `json:"status"`
	CreatedAt      string                      `json:"createdAt,omitempty"`
	UpdatedAt      string                      `json:"updatedAt,omitempty"`
	SettingsPatch  map[string]any              `json:"settingsPatch,omitempty"`
	LastError      string                      `json:"lastError,omitempty"`
	AuditTrail     []PendingSettingsAuditEntry `json:"auditTrail,omitempty"`
}

type PendingSettingsAuditEntry struct {
	Transition   string `json:"transition"`
	Status       string `json:"status"`
	At           string `json:"at,omitempty"`
	TargetNodeID string `json:"targetNodeId"`
	ChangeID     string `json:"changeId"`
	OriginNodeID string `json:"originNodeId,omitempty"`
	LastError    string `json:"lastError,omitempty"`
}

type MetadataCompactionPolicy struct {
	PeerIDs           []string `json:"peerIds,omitempty"`
	RetainLastCursors uint64   `json:"retainLastCursors,omitempty"`
}

type MetadataCompactionPlan struct {
	FolderID           string            `json:"folderId"`
	CurrentCursor      uint64            `json:"currentCursor"`
	SafeCursor         uint64            `json:"safeCursor"`
	PeerCursors        map[string]uint64 `json:"peerCursors,omitempty"`
	BlockedPeers       []string          `json:"blockedPeers,omitempty"`
	EligibleTombstones int               `json:"eligibleTombstones"`
	RetainedTombstones int               `json:"retainedTombstones"`
}

type MetadataCompactionSnapshot struct {
	FolderID            string `json:"folderId"`
	Cursor              uint64 `json:"cursor"`
	StateHash           string `json:"stateHash"`
	Files               int    `json:"files"`
	Tombstones          int    `json:"tombstones"`
	SafeCursor          uint64 `json:"safeCursor"`
	CompactedTombstones int    `json:"compactedTombstones"`
}

type MetadataCompactionResult struct {
	Plan                MetadataCompactionPlan     `json:"plan"`
	Snapshot            MetadataCompactionSnapshot `json:"snapshot"`
	CompactedTombstones int                        `json:"compactedTombstones"`
}

type SnapshotMarker struct {
	ID                    string `json:"id"`
	FolderID              string `json:"folderId"`
	Cursor                uint64 `json:"cursor"`
	StateHash             string `json:"stateHash"`
	CreatedAt             string `json:"createdAt"`
	Description           string `json:"description,omitempty"`
	Pinned                bool   `json:"pinned,omitempty"`
	Deprecated            bool   `json:"deprecated,omitempty"`
	ArchiveFullyProtected bool   `json:"archiveFullyProtected,omitempty"`
	DBCheckpointAvailable bool   `json:"dbCheckpointAvailable,omitempty"`
}

type ArchiveIntakeJob struct {
	ID            string      `json:"id"`
	SnapshotID    string      `json:"snapshotId"`
	FolderID      string      `json:"folderId"`
	Path          string      `json:"path"`
	Block         block.Block `json:"block"`
	Status        string      `json:"status"`
	CreatedAt     string      `json:"createdAt"`
	Attempts      int         `json:"attempts,omitempty"`
	LastAttemptAt string      `json:"lastAttemptAt,omitempty"`
	NextAttemptAt string      `json:"nextAttemptAt,omitempty"`
	LastError     string      `json:"lastError,omitempty"`
	ArchivedAt    string      `json:"archivedAt,omitempty"`
}

type BackupRestoreJobFile struct {
	Path            string `json:"path"`
	DestinationPath string `json:"destinationPath"`
	Status          string `json:"status"`
	Size            int64  `json:"size,omitempty"`
	Error           string `json:"error,omitempty"`
}

type BackupRestoreJob struct {
	ID             string                 `json:"id"`
	SnapshotID     string                 `json:"snapshotId"`
	FolderID       string                 `json:"folderId"`
	Destination    string                 `json:"destination"`
	Status         string                 `json:"status"`
	TotalFiles     int                    `json:"totalFiles"`
	RestoredFiles  int                    `json:"restoredFiles"`
	RestoredBytes  int64                  `json:"restoredBytes"`
	SkippedFiles   int                    `json:"skippedFiles"`
	RemainingFiles int                    `json:"remainingFiles"`
	StartedAt      string                 `json:"startedAt"`
	UpdatedAt      string                 `json:"updatedAt"`
	CompletedAt    string                 `json:"completedAt,omitempty"`
	LastError      string                 `json:"lastError,omitempty"`
	Files          []BackupRestoreJobFile `json:"files,omitempty"`
}

type BackupRetentionJob struct {
	ID                  string `json:"id"`
	Status              string `json:"status"`
	KeepLast            int    `json:"keepLast"`
	DeprecatedSnapshots int    `json:"deprecatedSnapshots"`
	DeletedSnapshots    int    `json:"deletedSnapshots"`
	PromotedManifests   int    `json:"promotedManifests"`
	SweptArchiveBlocks  int    `json:"sweptArchiveBlocks"`
	TotalOperations     int    `json:"totalOperations"`
	RemainingOperations int    `json:"remainingOperations"`
	StartedAt           string `json:"startedAt"`
	UpdatedAt           string `json:"updatedAt"`
	CompletedAt         string `json:"completedAt,omitempty"`
	LastError           string `json:"lastError,omitempty"`
}

type BackupRepairJobBlock struct {
	SnapshotID string `json:"snapshotId,omitempty"`
	JobID      string `json:"jobId,omitempty"`
	FolderID   string `json:"folderId,omitempty"`
	Path       string `json:"path,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Status     string `json:"status"`
	SourceKind string `json:"sourceKind,omitempty"`
	Error      string `json:"error,omitempty"`
}

type BackupRepairJob struct {
	ID               string                 `json:"id"`
	Status           string                 `json:"status"`
	TotalBlocks      int                    `json:"totalBlocks"`
	RepairedBlocks   int                    `json:"repairedBlocks"`
	UnresolvedBlocks int                    `json:"unresolvedBlocks"`
	RemainingBlocks  int                    `json:"remainingBlocks"`
	StartedAt        string                 `json:"startedAt"`
	UpdatedAt        string                 `json:"updatedAt"`
	CompletedAt      string                 `json:"completedAt,omitempty"`
	LastError        string                 `json:"lastError,omitempty"`
	Blocks           []BackupRepairJobBlock `json:"blocks,omitempty"`
}

type MetadataCompactedError struct {
	FolderID          string
	RequestedCursor   uint64
	SafeCursor        uint64
	SnapshotCursor    uint64
	SnapshotStateHash string
}

func (e *MetadataCompactedError) Error() string {
	return fmt.Sprintf("metadata for folder %s before cursor %d was compacted; peer at cursor %d needs full refresh", e.FolderID, e.SafeCursor, e.RequestedCursor)
}

type VerifiedStagedBlock struct {
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Size   int    `json:"size"`
	Hash   []byte `json:"hash"`
}

type PendingWrite struct {
	FolderID                  string                `json:"folderId"`
	Path                      string                `json:"path"`
	Manifest                  block.Manifest        `json:"manifest"`
	ExpectedBaseManifest      *block.Manifest       `json:"expectedBaseManifest,omitempty"`
	RequiredMetadataCursor    uint64                `json:"requiredMetadataCursor,omitempty"`
	RequiredMetadataStateHash string                `json:"requiredMetadataStateHash,omitempty"`
	VerifiedBlocks            []VerifiedStagedBlock `json:"verifiedBlocks,omitempty"`
	Committed                 bool                  `json:"committed,omitempty"`
	Reason                    string                `json:"reason,omitempty"`
}

type SkippedDelete struct {
	FolderID                  string   `json:"folderId"`
	Path                      string   `json:"path"`
	RequiredMetadataCursor    uint64   `json:"requiredMetadataCursor,omitempty"`
	RequiredMetadataStateHash string   `json:"requiredMetadataStateHash,omitempty"`
	RequiredWrites            []string `json:"requiredWrites,omitempty"`
	Reason                    string   `json:"reason,omitempty"`
}

func NewJSONStore(path string) JSONStore {
	return JSONStore{backend: fileSnapshotBackend{path: path}}
}

func NewBadgerStore(path string) (JSONStore, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		return JSONStore{}, err
	}
	backend := badgerSnapshotBackend{db: db}
	if err := backend.migrateLegacySnapshot(); err != nil {
		_ = db.Close()
		return JSONStore{}, err
	}
	return JSONStore{backend: backend}, nil
}

func NewPerFolderBadgerStore(paths map[string]string) (JSONStore, error) {
	backend := perFolderBadgerBackend{stores: map[string]JSONStore{}}
	for folderID, path := range paths {
		store, err := NewBadgerStore(path)
		if err != nil {
			_ = backend.Close()
			return JSONStore{}, err
		}
		backend.stores[folderID] = store
	}
	return JSONStore{backend: backend}, nil
}

type ImportResult struct {
	Folders           int
	ImportedManifests int
}

func ImportJSONSnapshot(sourcePath string, destination JSONStore) (ImportResult, error) {
	source := NewJSONStore(sourcePath)
	snap, err := source.load()
	if err != nil {
		return ImportResult{}, err
	}
	normalizeSnapshot(&snap)
	result := ImportResult{Folders: len(snap.Folders)}
	for _, manifests := range snap.Folders {
		result.ImportedManifests += len(manifests)
	}
	if err := destination.save(snap); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func ImportStoreSnapshot(source JSONStore, destination JSONStore) (ImportResult, error) {
	snap, err := source.load()
	if err != nil {
		return ImportResult{}, err
	}
	normalizeSnapshot(&snap)
	result := ImportResult{Folders: len(snap.Folders)}
	for _, manifests := range snap.Folders {
		result.ImportedManifests += len(manifests)
	}
	if err := destination.save(snap); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func (s JSONStore) ExportCheckpoint(path string) error {
	snap, err := s.load()
	if err != nil {
		return err
	}
	normalizeSnapshot(&snap)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.checkpoint-staging")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (s JSONStore) Close() error {
	if s.backend == nil {
		return nil
	}
	return s.backend.Close()
}

func (s JSONStore) SaveManifest(folderID string, relativePath string, manifest block.Manifest) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveManifest(folderID, relativePath, manifest)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.Folders == nil {
		snap.Folders = map[string]map[string]block.Manifest{}
	}
	if snap.Revisions == nil {
		snap.Revisions = map[string]map[string]uint64{}
	}
	if snap.ManifestHistory == nil {
		snap.ManifestHistory = map[string]map[string]map[uint64]block.Manifest{}
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
	if snap.Folders[folderID] == nil {
		snap.Folders[folderID] = map[string]block.Manifest{}
	}
	if snap.Revisions[folderID] == nil {
		snap.Revisions[folderID] = map[string]uint64{}
	}
	if snap.ManifestHistory[folderID] == nil {
		snap.ManifestHistory[folderID] = map[string]map[uint64]block.Manifest{}
	}
	if snap.ManifestHistory[folderID][relativePath] == nil {
		snap.ManifestHistory[folderID][relativePath] = map[uint64]block.Manifest{}
	}
	if snap.Tombstones[folderID] == nil {
		snap.Tombstones[folderID] = map[string]uint64{}
	}
	rev := snap.Cursors[folderID] + 1
	snap.Cursors[folderID] = rev
	snap.Folders[folderID][relativePath] = manifest
	snap.Revisions[folderID][relativePath] = rev
	snap.ManifestHistory[folderID][relativePath][rev] = manifest
	delete(snap.Tombstones[folderID], relativePath)
	return s.save(snap)
}

func (s JSONStore) SaveSnapshotMarker(marker SnapshotMarker) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveSnapshotMarker(marker)
	}
	if marker.ID == "" {
		return fmt.Errorf("snapshot marker id is required")
	}
	if marker.FolderID == "" {
		return fmt.Errorf("snapshot marker folder id is required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.SnapshotMarkers == nil {
		snap.SnapshotMarkers = map[string]SnapshotMarker{}
	}
	snap.SnapshotMarkers[marker.ID] = marker
	return s.save(snap)
}

func (s JSONStore) SnapshotManifests(snapshotID string) (map[string]block.Manifest, error) {
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	marker, ok := snap.SnapshotMarkers[snapshotID]
	if !ok {
		return nil, fmt.Errorf("snapshot %q not found", snapshotID)
	}
	return manifestsAtCursor(snap, marker.FolderID, marker.Cursor), nil
}

func (s JSONStore) PreserveSnapshotManifest(folderID string, relativePath string, revision uint64, manifest block.Manifest) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.PreserveSnapshotManifest(folderID, relativePath, revision, manifest)
	}
	if folderID == "" {
		return fmt.Errorf("folder id required")
	}
	if relativePath == "" {
		return fmt.Errorf("relative path required")
	}
	if revision == 0 {
		return fmt.Errorf("revision required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	ensureFolderState(&snap, folderID)
	if snap.ManifestHistory[folderID][relativePath] == nil {
		snap.ManifestHistory[folderID][relativePath] = map[uint64]block.Manifest{}
	}
	snap.ManifestHistory[folderID][relativePath][revision] = manifest
	return s.save(snap)
}

func (s JSONStore) LoadSnapshotMarker(id string) (SnapshotMarker, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadSnapshotMarker(id)
	}
	snap, err := s.load()
	if err != nil {
		return SnapshotMarker{}, false, err
	}
	marker, ok := snap.SnapshotMarkers[id]
	return marker, ok, nil
}

func (s JSONStore) ListSnapshotMarkers(folderID string) ([]SnapshotMarker, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListSnapshotMarkers(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	markers := make([]SnapshotMarker, 0, len(snap.SnapshotMarkers))
	for _, marker := range snap.SnapshotMarkers {
		if folderID == "" || marker.FolderID == folderID {
			markers = append(markers, marker)
		}
	}
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].CreatedAt != markers[j].CreatedAt {
			return markers[i].CreatedAt < markers[j].CreatedAt
		}
		return markers[i].ID < markers[j].ID
	})
	return markers, nil
}

func (s JSONStore) DeleteSnapshotMarker(id string) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.DeleteSnapshotMarker(id)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	delete(snap.SnapshotMarkers, id)
	return s.save(snap)
}

func (s JSONStore) SaveArchiveIntakeJobs(snapshotID string, jobs []ArchiveIntakeJob) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveArchiveIntakeJobs(snapshotID, jobs)
	}
	if snapshotID == "" {
		return fmt.Errorf("snapshot id is required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.ArchiveIntakeJobs == nil {
		snap.ArchiveIntakeJobs = map[string][]ArchiveIntakeJob{}
	}
	snap.ArchiveIntakeJobs[snapshotID] = cloneArchiveIntakeJobs(jobs)
	return s.save(snap)
}

func (s JSONStore) ListArchiveIntakeJobs(snapshotID string) ([]ArchiveIntakeJob, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListArchiveIntakeJobs(snapshotID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	if snapshotID != "" {
		return cloneArchiveIntakeJobs(snap.ArchiveIntakeJobs[snapshotID]), nil
	}
	jobs := make([]ArchiveIntakeJob, 0)
	ids := make([]string, 0, len(snap.ArchiveIntakeJobs))
	for id := range snap.ArchiveIntakeJobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		jobs = append(jobs, cloneArchiveIntakeJobs(snap.ArchiveIntakeJobs[id])...)
	}
	return jobs, nil
}

func (s JSONStore) SaveBackupRestoreJob(job BackupRestoreJob) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveBackupRestoreJob(job)
	}
	if job.ID == "" {
		return fmt.Errorf("backup restore job id is required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.BackupRestoreJobs == nil {
		snap.BackupRestoreJobs = map[string]BackupRestoreJob{}
	}
	snap.BackupRestoreJobs[job.ID] = cloneBackupRestoreJob(job)
	return s.save(snap)
}

func (s JSONStore) LoadBackupRestoreJob(id string) (BackupRestoreJob, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadBackupRestoreJob(id)
	}
	snap, err := s.load()
	if err != nil {
		return BackupRestoreJob{}, false, err
	}
	job, ok := snap.BackupRestoreJobs[id]
	return cloneBackupRestoreJob(job), ok, nil
}

func (s JSONStore) ListBackupRestoreJobs(snapshotID string) ([]BackupRestoreJob, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListBackupRestoreJobs(snapshotID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(snap.BackupRestoreJobs))
	for id := range snap.BackupRestoreJobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	jobs := make([]BackupRestoreJob, 0, len(ids))
	for _, id := range ids {
		job := snap.BackupRestoreJobs[id]
		if snapshotID == "" || job.SnapshotID == snapshotID {
			jobs = append(jobs, cloneBackupRestoreJob(job))
		}
	}
	return jobs, nil
}

func (s JSONStore) SaveBackupRetentionJob(job BackupRetentionJob) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveBackupRetentionJob(job)
	}
	if job.ID == "" {
		return fmt.Errorf("backup retention job id is required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.BackupRetentionJobs == nil {
		snap.BackupRetentionJobs = map[string]BackupRetentionJob{}
	}
	snap.BackupRetentionJobs[job.ID] = job
	return s.save(snap)
}

func (s JSONStore) LoadBackupRetentionJob(id string) (BackupRetentionJob, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadBackupRetentionJob(id)
	}
	snap, err := s.load()
	if err != nil {
		return BackupRetentionJob{}, false, err
	}
	job, ok := snap.BackupRetentionJobs[id]
	return job, ok, nil
}

func (s JSONStore) ListBackupRetentionJobs() ([]BackupRetentionJob, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListBackupRetentionJobs()
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(snap.BackupRetentionJobs))
	for id := range snap.BackupRetentionJobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	jobs := make([]BackupRetentionJob, 0, len(ids))
	for _, id := range ids {
		jobs = append(jobs, snap.BackupRetentionJobs[id])
	}
	return jobs, nil
}

func (s JSONStore) SaveBackupRepairJob(job BackupRepairJob) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveBackupRepairJob(job)
	}
	if job.ID == "" {
		return fmt.Errorf("backup repair job id is required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.BackupRepairJobs == nil {
		snap.BackupRepairJobs = map[string]BackupRepairJob{}
	}
	snap.BackupRepairJobs[job.ID] = cloneBackupRepairJob(job)
	return s.save(snap)
}

func (s JSONStore) LoadBackupRepairJob(id string) (BackupRepairJob, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadBackupRepairJob(id)
	}
	snap, err := s.load()
	if err != nil {
		return BackupRepairJob{}, false, err
	}
	job, ok := snap.BackupRepairJobs[id]
	return cloneBackupRepairJob(job), ok, nil
}

func (s JSONStore) ListBackupRepairJobs() ([]BackupRepairJob, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListBackupRepairJobs()
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(snap.BackupRepairJobs))
	for id := range snap.BackupRepairJobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	jobs := make([]BackupRepairJob, 0, len(ids))
	for _, id := range ids {
		jobs = append(jobs, cloneBackupRepairJob(snap.BackupRepairJobs[id]))
	}
	return jobs, nil
}

func (s JSONStore) SaveNodeSettingsDocument(nodeID string, doc NodeSettingsDocument) error {
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
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveNodeSettingsDocument(nodeID, doc)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.NodeSettings == nil {
		snap.NodeSettings = map[string]NodeSettingsDocument{}
	}
	snap.NodeSettings[nodeID] = doc
	return s.save(snap)
}

func (s JSONStore) LoadNodeSettingsDocument(nodeID string) (NodeSettingsDocument, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadNodeSettingsDocument(nodeID)
	}
	snap, err := s.load()
	if err != nil {
		return NodeSettingsDocument{}, false, err
	}
	doc, ok := snap.NodeSettings[nodeID]
	return doc, ok, nil
}

func (s JSONStore) ListNodeSettingsDocuments() ([]NodeSettingsDocument, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListNodeSettingsDocuments()
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	nodeIDs := make([]string, 0, len(snap.NodeSettings))
	for nodeID := range snap.NodeSettings {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	docs := make([]NodeSettingsDocument, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		docs = append(docs, snap.NodeSettings[nodeID])
	}
	return docs, nil
}

func (s JSONStore) SavePendingSettingsChange(targetNodeID string, change PendingSettingsChange) error {
	if err := validatePendingSettingsChangeTarget(targetNodeID, change); err != nil {
		return err
	}
	if change.SettingsPatch == nil {
		change.SettingsPatch = map[string]any{}
	}
	if existing, ok, err := s.LoadPendingSettingsChange(targetNodeID, change.ID); err != nil {
		return err
	} else {
		change = withPendingSettingsAudit(existing, change, ok)
	}
	if backend, ok := s.badgerBackend(); ok {
		return backend.SavePendingSettingsChange(targetNodeID, change)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.PendingSettings == nil {
		snap.PendingSettings = map[string]map[string]PendingSettingsChange{}
	}
	if snap.PendingSettings[targetNodeID] == nil {
		snap.PendingSettings[targetNodeID] = map[string]PendingSettingsChange{}
	}
	snap.PendingSettings[targetNodeID][change.ID] = change
	return s.save(snap)
}

func (s JSONStore) LoadPendingSettingsChange(targetNodeID string, changeID string) (PendingSettingsChange, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadPendingSettingsChange(targetNodeID, changeID)
	}
	snap, err := s.load()
	if err != nil {
		return PendingSettingsChange{}, false, err
	}
	changes := snap.PendingSettings[targetNodeID]
	if changes == nil {
		return PendingSettingsChange{}, false, nil
	}
	change, ok := changes[changeID]
	return change, ok, nil
}

func (s JSONStore) ListPendingSettingsChanges(targetNodeID string) ([]PendingSettingsChange, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListPendingSettingsChanges(targetNodeID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	changes := make([]PendingSettingsChange, 0)
	if targetNodeID != "" {
		ids := make([]string, 0, len(snap.PendingSettings[targetNodeID]))
		for id := range snap.PendingSettings[targetNodeID] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			changes = append(changes, snap.PendingSettings[targetNodeID][id])
		}
		return changes, nil
	}
	targets := make([]string, 0, len(snap.PendingSettings))
	for target := range snap.PendingSettings {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		ids := make([]string, 0, len(snap.PendingSettings[target]))
		for id := range snap.PendingSettings[target] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			changes = append(changes, snap.PendingSettings[target][id])
		}
	}
	return changes, nil
}

func (s JSONStore) UpdatePendingSettingsChangeStatus(targetNodeID string, changeID string, status string, updatedAt string, lastError string) error {
	if targetNodeID == "" {
		return fmt.Errorf("target node id is required")
	}
	if changeID == "" {
		return fmt.Errorf("pending settings change id is required")
	}
	if status != "acked" && status != "failed" {
		return fmt.Errorf("unsupported pending settings change status %q", status)
	}
	if status == "failed" && lastError == "" {
		return fmt.Errorf("failed pending settings change status requires last error")
	}
	change, ok, err := s.LoadPendingSettingsChange(targetNodeID, changeID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("pending settings change %q for target %q not found", changeID, targetNodeID)
	}
	change.Status = status
	change.UpdatedAt = updatedAt
	change.LastError = lastError
	return s.SavePendingSettingsChange(targetNodeID, change)
}

func (s JSONStore) ApplyPendingSettingsChange(targetNodeID string, changeID string, appliedAt string) (NodeSettingsDocument, PendingSettingsChange, error) {
	if targetNodeID == "" {
		return NodeSettingsDocument{}, PendingSettingsChange{}, fmt.Errorf("target node id is required")
	}
	if changeID == "" {
		return NodeSettingsDocument{}, PendingSettingsChange{}, fmt.Errorf("pending settings change id is required")
	}
	change, ok, err := s.LoadPendingSettingsChange(targetNodeID, changeID)
	if err != nil {
		return NodeSettingsDocument{}, PendingSettingsChange{}, err
	}
	if !ok {
		return NodeSettingsDocument{}, PendingSettingsChange{}, fmt.Errorf("pending settings change %q for target %q not found", changeID, targetNodeID)
	}
	if err := validatePendingSettingsChangeTarget(targetNodeID, change); err != nil {
		return NodeSettingsDocument{}, PendingSettingsChange{}, err
	}
	doc, ok, err := s.LoadNodeSettingsDocument(targetNodeID)
	if err != nil {
		return NodeSettingsDocument{}, PendingSettingsChange{}, err
	}
	if !ok {
		doc = NodeSettingsDocument{NodeID: targetNodeID, Settings: map[string]any{}}
	}
	if doc.NodeID != targetNodeID {
		return NodeSettingsDocument{}, PendingSettingsChange{}, fmt.Errorf("node settings document owner mismatch: key %q document %q", targetNodeID, doc.NodeID)
	}
	if change.Status == "applied" {
		return doc, change, nil
	}
	if change.Status == "failed" {
		return NodeSettingsDocument{}, change, fmt.Errorf("pending settings change %q is failed: %s", change.ID, change.LastError)
	}
	if !settingsChangeOriginAuthorized(doc, change.OriginNodeID) {
		change.Status = "failed"
		change.UpdatedAt = appliedAt
		change.LastError = fmt.Sprintf("origin node %q is not authorized to change settings for %q", change.OriginNodeID, targetNodeID)
		if err := s.SavePendingSettingsChange(targetNodeID, change); err != nil {
			return NodeSettingsDocument{}, PendingSettingsChange{}, err
		}
		return NodeSettingsDocument{}, change, fmt.Errorf("%s", change.LastError)
	}
	if doc.Revision >= change.Revision {
		change.Status = "failed"
		change.UpdatedAt = appliedAt
		change.LastError = fmt.Sprintf("stale pending settings change revision %d is not newer than node document revision %d", change.Revision, doc.Revision)
		if err := s.SavePendingSettingsChange(targetNodeID, change); err != nil {
			return NodeSettingsDocument{}, PendingSettingsChange{}, err
		}
		return NodeSettingsDocument{}, change, fmt.Errorf("%s", change.LastError)
	}
	if doc.Settings == nil {
		doc.Settings = map[string]any{}
	}
	for key, value := range change.SettingsPatch {
		doc.Settings[key] = cloneSettingsValue(value)
	}
	if doc.Revision < change.Revision {
		doc.Revision = change.Revision
	}
	doc.UpdatedAt = appliedAt
	doc.Source = "mesh"
	doc.ApplyStatus = "applied"
	change.Status = "applied"
	change.UpdatedAt = appliedAt
	change.LastError = ""
	if err := s.SaveNodeSettingsDocument(targetNodeID, doc); err != nil {
		return NodeSettingsDocument{}, PendingSettingsChange{}, err
	}
	if err := s.SavePendingSettingsChange(targetNodeID, change); err != nil {
		return NodeSettingsDocument{}, PendingSettingsChange{}, err
	}
	if saved, ok, err := s.LoadPendingSettingsChange(targetNodeID, change.ID); err != nil {
		return NodeSettingsDocument{}, PendingSettingsChange{}, err
	} else if ok {
		change = saved
	}
	return doc, change, nil
}

func settingsChangeOriginAuthorized(doc NodeSettingsDocument, originNodeID string) bool {
	if doc.Settings == nil {
		return true
	}
	raw, ok := doc.Settings["mesh.authorizedSettingsPeers"]
	if !ok {
		return true
	}
	switch peers := raw.(type) {
	case []string:
		for _, peer := range peers {
			if peer == originNodeID {
				return true
			}
		}
	case []any:
		for _, item := range peers {
			if peer, ok := item.(string); ok && peer == originNodeID {
				return true
			}
		}
	}
	return false
}

func withPendingSettingsAudit(existing PendingSettingsChange, next PendingSettingsChange, existed bool) PendingSettingsChange {
	trail := append([]PendingSettingsAuditEntry(nil), existing.AuditTrail...)
	if len(next.AuditTrail) > len(trail) {
		trail = append(trail, next.AuditTrail[len(trail):]...)
	}
	if !existed || existing.Status != next.Status {
		trail = append(trail, PendingSettingsAuditEntry{
			Transition:   pendingSettingsAuditTransition(next.Status),
			Status:       next.Status,
			At:           pendingSettingsAuditTime(next),
			TargetNodeID: next.TargetNodeID,
			ChangeID:     next.ID,
			OriginNodeID: next.OriginNodeID,
			LastError:    next.LastError,
		})
	}
	next.AuditTrail = trail
	return next
}

func pendingSettingsAuditTransition(status string) string {
	if status == "" {
		return "queued"
	}
	return status
}

func pendingSettingsAuditTime(change PendingSettingsChange) string {
	if change.UpdatedAt != "" {
		return change.UpdatedAt
	}
	return change.CreatedAt
}

func cloneSettingsValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			out[key] = cloneSettingsValue(nested)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = cloneSettingsValue(nested)
		}
		return out
	default:
		return value
	}
}

func validatePendingSettingsChangeTarget(targetNodeID string, change PendingSettingsChange) error {
	if change.ID == "" {
		return fmt.Errorf("pending settings change id is required")
	}
	if change.TargetNodeID == "" {
		return fmt.Errorf("pending settings change target node id is required")
	}
	if targetNodeID == "" {
		targetNodeID = change.TargetNodeID
	}
	if change.TargetNodeID != targetNodeID {
		return fmt.Errorf("pending settings change target mismatch: key %q change %q", targetNodeID, change.TargetNodeID)
	}
	if change.OriginNodeID == "" {
		return fmt.Errorf("pending settings change origin node id is required")
	}
	if change.IdempotencyKey == "" {
		return fmt.Errorf("pending settings change idempotency key is required")
	}
	return nil
}

func (s JSONStore) LoadManifest(folderID string, relativePath string) (block.Manifest, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadManifest(folderID, relativePath)
	}
	snap, err := s.load()
	if err != nil {
		return block.Manifest{}, false, err
	}
	folder := snap.Folders[folderID]
	if folder == nil {
		return block.Manifest{}, false, nil
	}
	manifest, ok := folder[relativePath]
	return manifest, ok, nil
}

func (s JSONStore) ListManifests(folderID string) (map[string]block.Manifest, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListManifests(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	folder := snap.Folders[folderID]
	out := make(map[string]block.Manifest, len(folder))
	for rel, manifest := range folder {
		out[rel] = manifest
	}
	return out, nil
}

func (s JSONStore) DeleteManifest(folderID string, relativePath string) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.DeleteManifest(folderID, relativePath)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.Folders[folderID] != nil {
		delete(snap.Folders[folderID], relativePath)
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
	if snap.Revisions[folderID] != nil {
		delete(snap.Revisions[folderID], relativePath)
	}
	if snap.Tombstones[folderID] == nil {
		snap.Tombstones[folderID] = map[string]uint64{}
	}
	rev := snap.Cursors[folderID] + 1
	snap.Cursors[folderID] = rev
	snap.Tombstones[folderID][relativePath] = rev
	return s.save(snap)
}

func (s JSONStore) MoveManifest(folderID string, fromPath string, toPath string, manifest block.Manifest) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.MoveManifest(folderID, fromPath, toPath, manifest)
	}
	if folderID == "" {
		return fmt.Errorf("folder id required")
	}
	if fromPath == "" || toPath == "" {
		return fmt.Errorf("move paths required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	ensureFolderState(&snap, folderID)
	if snap.ManifestHistory[folderID] == nil {
		snap.ManifestHistory[folderID] = map[string]map[uint64]block.Manifest{}
	}
	if snap.ManifestHistory[folderID][toPath] == nil {
		snap.ManifestHistory[folderID][toPath] = map[uint64]block.Manifest{}
	}
	rev := snap.Cursors[folderID] + 1
	snap.Cursors[folderID] = rev
	delete(snap.Folders[folderID], fromPath)
	delete(snap.Revisions[folderID], fromPath)
	snap.Tombstones[folderID][fromPath] = rev
	snap.Folders[folderID][toPath] = manifest
	snap.Revisions[folderID][toPath] = rev
	snap.ManifestHistory[folderID][toPath][rev] = manifest
	snap.RenameHints[folderID][fromPath] = RenameHint{FromPath: fromPath, ToPath: toPath, Revision: rev}
	return s.save(snap)
}

func (s JSONStore) FolderSummary(folderID string) (FolderSummary, error) {
	snap, err := s.load()
	if err != nil {
		return FolderSummary{}, err
	}
	return folderSummary(snap, folderID)
}

func (s JSONStore) ChangesSince(folderID string, cursor uint64) (FolderChanges, error) {
	return s.ChangesSinceLimit(folderID, cursor, 0)
}

func (s JSONStore) ChangesSinceLimit(folderID string, cursor uint64, maxChanges int) (FolderChanges, error) {
	snap, err := s.load()
	if err != nil {
		return FolderChanges{}, err
	}
	if err := metadataCompactionCursorError(snap, folderID, cursor); err != nil {
		return FolderChanges{}, err
	}
	changes := make([]FolderChange, 0)
	for rel, rev := range snap.Revisions[folderID] {
		if rev <= cursor {
			continue
		}
		if hint, ok := renameHintForDestination(snap.RenameHints[folderID], rel, rev); ok {
			manifest := snap.Folders[folderID][rel]
			manifestCopy := manifest
			changes = append(changes, FolderChange{Kind: ChangeMove, FromPath: hint.FromPath, Path: rel, Revision: rev, Manifest: &manifestCopy})
			continue
		}
		manifest := snap.Folders[folderID][rel]
		manifestCopy := manifest
		changes = append(changes, FolderChange{Kind: ChangeUpsert, Path: rel, Revision: rev, Manifest: &manifestCopy})
	}
	for rel, rev := range snap.Tombstones[folderID] {
		if rev <= cursor {
			continue
		}
		if hint, ok := snap.RenameHints[folderID][rel]; ok && hint.Revision == rev {
			continue
		}
		changes = append(changes, FolderChange{Kind: ChangeDelete, Path: rel, Revision: rev})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Revision != changes[j].Revision {
			return changes[i].Revision < changes[j].Revision
		}
		return changes[i].Path < changes[j].Path
	})
	toCursor := snap.Cursors[folderID]
	if maxChanges > 0 && len(changes) > maxChanges {
		changes = changes[:maxChanges]
		toCursor = changes[len(changes)-1].Revision
	}
	summary, err := folderSummaryAtCursor(snap, folderID, toCursor)
	if err != nil {
		return FolderChanges{}, err
	}
	return FolderChanges{FolderID: folderID, FromCursor: cursor, ToCursor: toCursor, StateHash: summary.StateHash, Changes: changes}, nil
}

func (s JSONStore) SavePeerFolderState(peerID string, summary FolderSummary) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SavePeerFolderState(peerID, summary)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.PeerStates == nil {
		snap.PeerStates = map[string]map[string]FolderSummary{}
	}
	if snap.PeerStates[peerID] == nil {
		snap.PeerStates[peerID] = map[string]FolderSummary{}
	}
	snap.PeerStates[peerID][summary.FolderID] = summary
	if snap.PeerCursors == nil {
		snap.PeerCursors = map[string]map[string]uint64{}
	}
	if snap.PeerCursors[peerID] == nil {
		snap.PeerCursors[peerID] = map[string]uint64{}
	}
	snap.PeerCursors[peerID][summary.FolderID] = summary.Cursor
	return s.save(snap)
}

func (s JSONStore) PeerStateVector(peerID string) (PeerStateVector, error) {
	snap, err := s.load()
	if err != nil {
		return PeerStateVector{}, err
	}
	folders := make([]FolderSummary, 0, len(snap.PeerStates[peerID]))
	for _, summary := range snap.PeerStates[peerID] {
		folders = append(folders, summary)
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].FolderID < folders[j].FolderID })
	return PeerStateVector{PeerID: peerID, Folders: folders}, nil
}

func (s JSONStore) PeerFolderStatuses(peerID string) ([]PeerFolderStatus, error) {
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	folderIDs := map[string]struct{}{}
	for folderID := range snap.Folders {
		folderIDs[folderID] = struct{}{}
	}
	for folderID := range snap.Cursors {
		folderIDs[folderID] = struct{}{}
	}
	for folderID := range snap.Tombstones {
		folderIDs[folderID] = struct{}{}
	}
	for folderID := range snap.PeerStates[peerID] {
		folderIDs[folderID] = struct{}{}
	}
	ids := make([]string, 0, len(folderIDs))
	for folderID := range folderIDs {
		ids = append(ids, folderID)
	}
	sort.Strings(ids)
	statuses := make([]PeerFolderStatus, 0, len(ids))
	for _, folderID := range ids {
		local, err := folderSummary(snap, folderID)
		if err != nil {
			return nil, err
		}
		peer := snap.PeerStates[peerID][folderID]
		statuses = append(statuses, PeerFolderStatus{
			PeerID:         peerID,
			FolderID:       folderID,
			PeerCursor:     peer.Cursor,
			PeerStateHash:  peer.StateHash,
			LocalCursor:    local.Cursor,
			LocalStateHash: local.StateHash,
			InSync:         peer.Cursor == local.Cursor && peer.StateHash == local.StateHash,
		})
	}
	return statuses, nil
}

func (s JSONStore) ApplyPeerFolderChanges(peerID string, changes FolderChanges) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ApplyPeerFolderChanges(peerID, changes)
	}
	if peerID == "" {
		return fmt.Errorf("peer id required")
	}
	if changes.FolderID == "" {
		return fmt.Errorf("folder id required")
	}
	snap, err := s.load()
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
	candidate.PeerStates[peerID][changes.FolderID] = summary
	if candidate.PeerApplyCheckpoints == nil {
		candidate.PeerApplyCheckpoints = map[string]map[string]PeerApplyCheckpoint{}
	}
	if candidate.PeerApplyCheckpoints[peerID] == nil {
		candidate.PeerApplyCheckpoints[peerID] = map[string]PeerApplyCheckpoint{}
	}
	candidate.PeerApplyCheckpoints[peerID][changes.FolderID] = PeerApplyCheckpoint{
		FolderID:              changes.FolderID,
		FromCursor:            changes.FromCursor,
		ToCursor:              changes.ToCursor,
		ChangeCount:           len(changes.Changes),
		LastVerifiedCursor:    summary.Cursor,
		LastVerifiedStateHash: summary.StateHash,
	}
	return s.save(candidate)
}

func (s JSONStore) PeerApplyCheckpoint(peerID string, folderID string) (PeerApplyCheckpoint, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.PeerApplyCheckpoint(peerID, folderID)
	}
	snap, err := s.load()
	if err != nil {
		return PeerApplyCheckpoint{}, false, err
	}
	if snap.PeerApplyCheckpoints[peerID] == nil {
		return PeerApplyCheckpoint{}, false, nil
	}
	checkpoint, ok := snap.PeerApplyCheckpoints[peerID][folderID]
	return checkpoint, ok, nil
}

func (s JSONStore) MetadataCompactionPlan(folderID string, policy MetadataCompactionPolicy) (MetadataCompactionPlan, error) {
	snap, err := s.load()
	if err != nil {
		return MetadataCompactionPlan{}, err
	}
	return metadataCompactionPlan(snap, folderID, policy), nil
}

func (s JSONStore) CompactFolderMetadata(folderID string, policy MetadataCompactionPolicy) (MetadataCompactionResult, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.CompactFolderMetadata(folderID, policy)
	}
	snap, err := s.load()
	if err != nil {
		return MetadataCompactionResult{}, err
	}
	plan := metadataCompactionPlan(snap, folderID, policy)
	before, err := folderSummary(snap, folderID)
	if err != nil {
		return MetadataCompactionResult{}, err
	}
	compacted := 0
	for rel, rev := range snap.Tombstones[folderID] {
		if rev <= plan.SafeCursor {
			delete(snap.Tombstones[folderID], rel)
			compacted++
		}
	}
	snapshot := MetadataCompactionSnapshot{
		FolderID:            folderID,
		Cursor:              before.Cursor,
		StateHash:           before.StateHash,
		Files:               before.Files,
		Tombstones:          before.Tombstones,
		SafeCursor:          plan.SafeCursor,
		CompactedTombstones: compacted,
	}
	if compacted > 0 {
		if snap.CompactionSnapshots == nil {
			snap.CompactionSnapshots = map[string][]MetadataCompactionSnapshot{}
		}
		snap.CompactionSnapshots[folderID] = append(snap.CompactionSnapshots[folderID], snapshot)
		if err := s.save(snap); err != nil {
			return MetadataCompactionResult{}, err
		}
	}
	return MetadataCompactionResult{Plan: plan, Snapshot: snapshot, CompactedTombstones: compacted}, nil
}

func (s JSONStore) MetadataCompactionSnapshots(folderID string) ([]MetadataCompactionSnapshot, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.MetadataCompactionSnapshots(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	snapshots := append([]MetadataCompactionSnapshot(nil), snap.CompactionSnapshots[folderID]...)
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Cursor < snapshots[j].Cursor })
	return snapshots, nil
}

func (s JSONStore) SavePendingWrite(write PendingWrite) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SavePendingWrite(write)
	}
	if write.FolderID == "" {
		return fmt.Errorf("folder id required")
	}
	if write.Path == "" {
		return fmt.Errorf("pending write path required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	ensureApplyGateState(&snap, write.FolderID)
	if write.ExpectedBaseManifest != nil {
		base := *write.ExpectedBaseManifest
		write.ExpectedBaseManifest = &base
	}
	write.VerifiedBlocks = append([]VerifiedStagedBlock(nil), write.VerifiedBlocks...)
	snap.PendingWrites[write.FolderID][write.Path] = write
	return s.save(snap)
}

func (s JSONStore) PendingWrite(folderID string, path string) (PendingWrite, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.PendingWrite(folderID, path)
	}
	snap, err := s.load()
	if err != nil {
		return PendingWrite{}, false, err
	}
	write, ok := snap.PendingWrites[folderID][path]
	if write.ExpectedBaseManifest != nil {
		base := *write.ExpectedBaseManifest
		write.ExpectedBaseManifest = &base
	}
	write.VerifiedBlocks = append([]VerifiedStagedBlock(nil), write.VerifiedBlocks...)
	return write, ok, nil
}

func (s JSONStore) PendingWrites(folderID string) ([]PendingWrite, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.PendingWrites(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	writes := make([]PendingWrite, 0, len(snap.PendingWrites[folderID]))
	for _, write := range snap.PendingWrites[folderID] {
		if write.ExpectedBaseManifest != nil {
			base := *write.ExpectedBaseManifest
			write.ExpectedBaseManifest = &base
		}
		write.VerifiedBlocks = append([]VerifiedStagedBlock(nil), write.VerifiedBlocks...)
		writes = append(writes, write)
	}
	sort.Slice(writes, func(i, j int) bool { return writes[i].Path < writes[j].Path })
	return writes, nil
}

func (s JSONStore) AddVerifiedStagedBlock(folderID string, path string, verified VerifiedStagedBlock) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.AddVerifiedStagedBlock(folderID, path, verified)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	ensureApplyGateState(&snap, folderID)
	write, ok := snap.PendingWrites[folderID][path]
	if !ok {
		return fmt.Errorf("pending write %s/%s not found", folderID, path)
	}
	write.VerifiedBlocks = append(write.VerifiedBlocks, verified)
	snap.PendingWrites[folderID][path] = write
	return s.save(snap)
}

func (s JSONStore) MarkPendingWriteCommitted(folderID string, path string) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.MarkPendingWriteCommitted(folderID, path)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	ensureApplyGateState(&snap, folderID)
	write, ok := snap.PendingWrites[folderID][path]
	if !ok {
		return fmt.Errorf("pending write %s/%s not found", folderID, path)
	}
	write.Committed = true
	snap.PendingWrites[folderID][path] = write
	return s.save(snap)
}

func (s JSONStore) RemovePendingWrite(folderID string, path string) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.RemovePendingWrite(folderID, path)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.PendingWrites[folderID] != nil {
		delete(snap.PendingWrites[folderID], path)
	}
	return s.save(snap)
}

func (s JSONStore) SaveSkippedDelete(delete SkippedDelete) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SaveSkippedDelete(delete)
	}
	if delete.FolderID == "" {
		return fmt.Errorf("folder id required")
	}
	if delete.Path == "" {
		return fmt.Errorf("skipped delete path required")
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	ensureApplyGateState(&snap, delete.FolderID)
	delete.RequiredWrites = append([]string(nil), delete.RequiredWrites...)
	sort.Strings(delete.RequiredWrites)
	snap.SkippedDeletes[delete.FolderID][delete.Path] = delete
	return s.save(snap)
}

func (s JSONStore) ReadySkippedDeletes(folderID string, current FolderSummary) ([]SkippedDelete, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ReadySkippedDeletes(folderID, current)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	deletes := make([]SkippedDelete, 0)
	for _, delete := range snap.SkippedDeletes[folderID] {
		if !metadataPrerequisitesMet(delete, current) {
			continue
		}
		if !requiredWritesCommitted(snap.PendingWrites[folderID], delete.RequiredWrites) {
			continue
		}
		delete.RequiredWrites = append([]string(nil), delete.RequiredWrites...)
		deletes = append(deletes, delete)
	}
	sort.Slice(deletes, func(i, j int) bool { return deletes[i].Path < deletes[j].Path })
	return deletes, nil
}

func (s JSONStore) SkippedDeletes(folderID string) ([]SkippedDelete, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.SkippedDeletes(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	deletes := make([]SkippedDelete, 0, len(snap.SkippedDeletes[folderID]))
	for _, delete := range snap.SkippedDeletes[folderID] {
		delete.RequiredWrites = append([]string(nil), delete.RequiredWrites...)
		deletes = append(deletes, delete)
	}
	sort.Slice(deletes, func(i, j int) bool { return deletes[i].Path < deletes[j].Path })
	return deletes, nil
}

func (s JSONStore) RemoveSkippedDelete(folderID string, path string) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.RemoveSkippedDelete(folderID, path)
	}
	snap, err := s.load()
	if err != nil {
		return err
	}
	if snap.SkippedDeletes[folderID] != nil {
		delete(snap.SkippedDeletes[folderID], path)
	}
	return s.save(snap)
}

func (s JSONStore) LoadPeerManifest(peerID string, folderID string, relativePath string) (block.Manifest, bool, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.LoadPeerManifest(peerID, folderID, relativePath)
	}
	snap, err := s.load()
	if err != nil {
		return block.Manifest{}, false, err
	}
	folders := snap.PeerFolders[peerID]
	if folders == nil || folders[folderID] == nil {
		return block.Manifest{}, false, nil
	}
	manifest, ok := folders[folderID][relativePath]
	return manifest, ok, nil
}

func (s JSONStore) ReplacePeerFolderFromFullRefresh(peerID string, folderID string, summary FolderSummary, manifests map[string]block.Manifest, revisions map[string]uint64) error {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ReplacePeerFolderFromFullRefresh(peerID, folderID, summary, manifests, revisions)
	}
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
	snap, err := s.load()
	if err != nil {
		return err
	}
	ensurePeerFolderState(&snap, peerID, folderID)
	snap.PeerFolders[peerID][folderID] = map[string]block.Manifest{}
	snap.PeerRevisions[peerID][folderID] = map[string]uint64{}
	snap.PeerTombstones[peerID][folderID] = map[string]uint64{}
	for rel, manifest := range manifests {
		snap.PeerFolders[peerID][folderID][rel] = manifest
		rev := revisions[rel]
		if rev == 0 {
			rev = summary.Cursor
		}
		snap.PeerRevisions[peerID][folderID][rel] = rev
	}
	snap.PeerCursors[peerID][folderID] = summary.Cursor
	snap.PeerStates[peerID][folderID] = summary
	snap.PeerApplyCheckpoints[peerID][folderID] = PeerApplyCheckpoint{FolderID: folderID, FromCursor: 0, ToCursor: summary.Cursor, ChangeCount: len(manifests), LastVerifiedCursor: summary.Cursor, LastVerifiedStateHash: summary.StateHash}
	return s.save(snap)
}

func (s JSONStore) ListManifestRevisions(folderID string) (map[string]uint64, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListManifestRevisions(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make(map[string]uint64, len(snap.Revisions[folderID]))
	for rel, rev := range snap.Revisions[folderID] {
		out[rel] = rev
	}
	return out, nil
}

func (s JSONStore) ListTombstones(folderID string) (map[string]uint64, error) {
	if backend, ok := s.badgerBackend(); ok {
		return backend.ListTombstones(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make(map[string]uint64, len(snap.Tombstones[folderID]))
	for rel, rev := range snap.Tombstones[folderID] {
		out[rel] = rev
	}
	return out, nil
}

func (s JSONStore) FindBlocks(size int, hash []byte) ([]BlockRef, error) {
	if backend, ok := s.blockIndexBackend(); ok {
		return backend.FindBlocks(size, hash)
	}
	refs, err := s.ListBlockRefs("")
	if err != nil {
		return nil, err
	}
	out := make([]BlockRef, 0)
	for _, ref := range refs {
		if ref.Block.Size == size && bytes.Equal(ref.Block.Hash, hash) {
			out = append(out, ref)
		}
	}
	return sortBlockRefs(out), nil
}

func (s JSONStore) ListBlockRefs(folderID string) ([]BlockRef, error) {
	if backend, ok := s.blockIndexBackend(); ok {
		return backend.ListBlockRefs(folderID)
	}
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	refs := make([]BlockRef, 0)
	for currentFolderID, folder := range snap.Folders {
		if folderID != "" && currentFolderID != folderID {
			continue
		}
		for rel, manifest := range folder {
			if manifest.Damaged {
				continue
			}
			for _, b := range manifest.Blocks {
				refs = append(refs, BlockRef{FolderID: currentFolderID, RelativePath: rel, Block: b})
			}
		}
	}
	return sortBlockRefs(refs), nil
}

func sortBlockRefs(refs []BlockRef) []BlockRef {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].FolderID != refs[j].FolderID {
			return refs[i].FolderID < refs[j].FolderID
		}
		if refs[i].RelativePath != refs[j].RelativePath {
			return refs[i].RelativePath < refs[j].RelativePath
		}
		if refs[i].Block.Index != refs[j].Block.Index {
			return refs[i].Block.Index < refs[j].Block.Index
		}
		if refs[i].Block.Offset != refs[j].Block.Offset {
			return refs[i].Block.Offset < refs[j].Block.Offset
		}
		if refs[i].Block.Size != refs[j].Block.Size {
			return refs[i].Block.Size < refs[j].Block.Size
		}
		return bytes.Compare(refs[i].Block.Hash, refs[j].Block.Hash) < 0
	})
	return refs
}

func (s JSONStore) blockIndexBackend() (blockIndexBackend, bool) {
	backend, ok := s.backend.(blockIndexBackend)
	return backend, ok
}

func (s JSONStore) badgerBackend() (badgerSnapshotBackend, bool) {
	backend, ok := s.backend.(badgerSnapshotBackend)
	return backend, ok
}

func (s JSONStore) load() (snapshot, error) {
	if s.backend == nil {
		return snapshot{}, fmt.Errorf("metadata store backend is not configured")
	}
	snap, err := s.backend.Load()
	if err != nil {
		return snapshot{}, err
	}
	return normalizeLoadedSnapshot(snap), nil
}

func normalizeLoadedSnapshot(snap snapshot) snapshot {
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
	if snap.SnapshotMarkers == nil {
		snap.SnapshotMarkers = map[string]SnapshotMarker{}
	}
	if snap.ArchiveIntakeJobs == nil {
		snap.ArchiveIntakeJobs = map[string][]ArchiveIntakeJob{}
	}
	if snap.BackupRestoreJobs == nil {
		snap.BackupRestoreJobs = map[string]BackupRestoreJob{}
	}
	if snap.BackupRetentionJobs == nil {
		snap.BackupRetentionJobs = map[string]BackupRetentionJob{}
	}
	if snap.BackupRepairJobs == nil {
		snap.BackupRepairJobs = map[string]BackupRepairJob{}
	}
	if snap.PendingWrites == nil {
		snap.PendingWrites = map[string]map[string]PendingWrite{}
	}
	if snap.SkippedDeletes == nil {
		snap.SkippedDeletes = map[string]map[string]SkippedDelete{}
	}
	if snap.NodeSettings == nil {
		snap.NodeSettings = map[string]NodeSettingsDocument{}
	}
	if snap.PendingSettings == nil {
		snap.PendingSettings = map[string]map[string]PendingSettingsChange{}
	}
	normalizeSnapshot(&snap)
	return snap
}

func normalizeSnapshot(snap *snapshot) {
	if snap.ManifestHistory == nil {
		snap.ManifestHistory = map[string]map[string]map[uint64]block.Manifest{}
	}
	for folderID, files := range snap.Folders {
		if snap.Revisions[folderID] == nil {
			snap.Revisions[folderID] = map[string]uint64{}
		}
		if snap.ManifestHistory[folderID] == nil {
			snap.ManifestHistory[folderID] = map[string]map[uint64]block.Manifest{}
		}
		paths := make([]string, 0, len(files))
		for rel := range files {
			if snap.Revisions[folderID][rel] == 0 {
				paths = append(paths, rel)
			}
		}
		sort.Strings(paths)
		for _, rel := range paths {
			rev := snap.Cursors[folderID] + 1
			snap.Cursors[folderID] = rev
			snap.Revisions[folderID][rel] = rev
		}
		for rel, manifest := range files {
			rev := snap.Revisions[folderID][rel]
			if rev == 0 {
				continue
			}
			if snap.ManifestHistory[folderID][rel] == nil {
				snap.ManifestHistory[folderID][rel] = map[uint64]block.Manifest{}
			}
			if _, ok := snap.ManifestHistory[folderID][rel][rev]; !ok {
				snap.ManifestHistory[folderID][rel][rev] = manifest
			}
		}
	}
	for folderID, tombstones := range snap.Tombstones {
		for _, rev := range tombstones {
			if rev > snap.Cursors[folderID] {
				snap.Cursors[folderID] = rev
			}
		}
	}
}

func ensurePeerFolderState(snap *snapshot, peerID string, folderID string) {
	if snap.PeerStates == nil {
		snap.PeerStates = map[string]map[string]FolderSummary{}
	}
	if snap.PeerStates[peerID] == nil {
		snap.PeerStates[peerID] = map[string]FolderSummary{}
	}
	if snap.PeerFolders == nil {
		snap.PeerFolders = map[string]map[string]map[string]block.Manifest{}
	}
	if snap.PeerFolders[peerID] == nil {
		snap.PeerFolders[peerID] = map[string]map[string]block.Manifest{}
	}
	if snap.PeerFolders[peerID][folderID] == nil {
		snap.PeerFolders[peerID][folderID] = map[string]block.Manifest{}
	}
	if snap.PeerRevisions == nil {
		snap.PeerRevisions = map[string]map[string]map[string]uint64{}
	}
	if snap.PeerRevisions[peerID] == nil {
		snap.PeerRevisions[peerID] = map[string]map[string]uint64{}
	}
	if snap.PeerRevisions[peerID][folderID] == nil {
		snap.PeerRevisions[peerID][folderID] = map[string]uint64{}
	}
	if snap.PeerTombstones == nil {
		snap.PeerTombstones = map[string]map[string]map[string]uint64{}
	}
	if snap.PeerTombstones[peerID] == nil {
		snap.PeerTombstones[peerID] = map[string]map[string]uint64{}
	}
	if snap.PeerTombstones[peerID][folderID] == nil {
		snap.PeerTombstones[peerID][folderID] = map[string]uint64{}
	}
	if snap.PeerRenameHints == nil {
		snap.PeerRenameHints = map[string]map[string]map[string]RenameHint{}
	}
	if snap.PeerRenameHints[peerID] == nil {
		snap.PeerRenameHints[peerID] = map[string]map[string]RenameHint{}
	}
	if snap.PeerRenameHints[peerID][folderID] == nil {
		snap.PeerRenameHints[peerID][folderID] = map[string]RenameHint{}
	}
	if snap.PeerCursors == nil {
		snap.PeerCursors = map[string]map[string]uint64{}
	}
	if snap.PeerCursors[peerID] == nil {
		snap.PeerCursors[peerID] = map[string]uint64{}
	}
	if snap.PeerApplyCheckpoints == nil {
		snap.PeerApplyCheckpoints = map[string]map[string]PeerApplyCheckpoint{}
	}
	if snap.PeerApplyCheckpoints[peerID] == nil {
		snap.PeerApplyCheckpoints[peerID] = map[string]PeerApplyCheckpoint{}
	}
}

func ensureFolderState(snap *snapshot, folderID string) {
	if snap.Folders == nil {
		snap.Folders = map[string]map[string]block.Manifest{}
	}
	if snap.Folders[folderID] == nil {
		snap.Folders[folderID] = map[string]block.Manifest{}
	}
	if snap.Revisions == nil {
		snap.Revisions = map[string]map[string]uint64{}
	}
	if snap.Revisions[folderID] == nil {
		snap.Revisions[folderID] = map[string]uint64{}
	}
	if snap.ManifestHistory == nil {
		snap.ManifestHistory = map[string]map[string]map[uint64]block.Manifest{}
	}
	if snap.ManifestHistory[folderID] == nil {
		snap.ManifestHistory[folderID] = map[string]map[uint64]block.Manifest{}
	}
	if snap.Tombstones == nil {
		snap.Tombstones = map[string]map[string]uint64{}
	}
	if snap.Tombstones[folderID] == nil {
		snap.Tombstones[folderID] = map[string]uint64{}
	}
	if snap.RenameHints == nil {
		snap.RenameHints = map[string]map[string]RenameHint{}
	}
	if snap.RenameHints[folderID] == nil {
		snap.RenameHints[folderID] = map[string]RenameHint{}
	}
	if snap.Cursors == nil {
		snap.Cursors = map[string]uint64{}
	}
}

func ensureApplyGateState(snap *snapshot, folderID string) {
	if snap.PendingWrites == nil {
		snap.PendingWrites = map[string]map[string]PendingWrite{}
	}
	if snap.PendingWrites[folderID] == nil {
		snap.PendingWrites[folderID] = map[string]PendingWrite{}
	}
	if snap.SkippedDeletes == nil {
		snap.SkippedDeletes = map[string]map[string]SkippedDelete{}
	}
	if snap.SkippedDeletes[folderID] == nil {
		snap.SkippedDeletes[folderID] = map[string]SkippedDelete{}
	}
}

func metadataCompactionCursorError(snap snapshot, folderID string, cursor uint64) error {
	var latest *MetadataCompactionSnapshot
	for i := range snap.CompactionSnapshots[folderID] {
		snapshot := snap.CompactionSnapshots[folderID][i]
		if snapshot.CompactedTombstones == 0 || cursor >= snapshot.SafeCursor {
			continue
		}
		if latest == nil || snapshot.SafeCursor > latest.SafeCursor {
			latest = &snapshot
		}
	}
	if latest == nil {
		return nil
	}
	return &MetadataCompactedError{
		FolderID:          folderID,
		RequestedCursor:   cursor,
		SafeCursor:        latest.SafeCursor,
		SnapshotCursor:    latest.Cursor,
		SnapshotStateHash: latest.StateHash,
	}
}

func metadataCompactionPlan(snap snapshot, folderID string, policy MetadataCompactionPolicy) MetadataCompactionPlan {
	current := snap.Cursors[folderID]
	safe := current
	peerCursors := make(map[string]uint64, len(policy.PeerIDs))
	blockedPeers := make([]string, 0)
	for _, peerID := range policy.PeerIDs {
		peerCursor := snap.PeerStates[peerID][folderID].Cursor
		peerCursors[peerID] = peerCursor
		if peerCursor < safe {
			safe = peerCursor
		}
		if peerCursor < current {
			blockedPeers = append(blockedPeers, peerID)
		}
	}
	if policy.RetainLastCursors > 0 {
		retentionFloor := uint64(0)
		if current > policy.RetainLastCursors {
			retentionFloor = current - policy.RetainLastCursors
		}
		if safe > retentionFloor {
			safe = retentionFloor
		}
	}
	eligible := 0
	retained := 0
	for _, revision := range snap.Tombstones[folderID] {
		if revision <= safe {
			eligible++
		} else {
			retained++
		}
	}
	sort.Strings(blockedPeers)
	return MetadataCompactionPlan{
		FolderID:           folderID,
		CurrentCursor:      current,
		SafeCursor:         safe,
		PeerCursors:        peerCursors,
		BlockedPeers:       blockedPeers,
		EligibleTombstones: eligible,
		RetainedTombstones: retained,
	}
}

func renameHintForDestination(hints map[string]RenameHint, toPath string, revision uint64) (RenameHint, bool) {
	for _, hint := range hints {
		if hint.ToPath == toPath && hint.Revision == revision {
			return hint, true
		}
	}
	return RenameHint{}, false
}

func metadataPrerequisitesMet(delete SkippedDelete, current FolderSummary) bool {
	if delete.RequiredMetadataCursor > 0 && current.Cursor < delete.RequiredMetadataCursor {
		return false
	}
	if delete.RequiredMetadataStateHash != "" && current.StateHash != delete.RequiredMetadataStateHash {
		return false
	}
	return true
}

func requiredWritesCommitted(writes map[string]PendingWrite, required []string) bool {
	for _, path := range required {
		write, ok := writes[path]
		if !ok || !write.Committed {
			return false
		}
	}
	return true
}

func cloneArchiveIntakeJobs(in []ArchiveIntakeJob) []ArchiveIntakeJob {
	out := make([]ArchiveIntakeJob, len(in))
	copy(out, in)
	for i := range out {
		out[i].Block.Hash = append([]byte(nil), out[i].Block.Hash...)
	}
	return out
}

func cloneBackupRestoreJob(in BackupRestoreJob) BackupRestoreJob {
	out := in
	if in.Files != nil {
		out.Files = append([]BackupRestoreJobFile(nil), in.Files...)
	}
	return out
}

func cloneBackupRepairJob(in BackupRepairJob) BackupRepairJob {
	out := in
	if in.Blocks != nil {
		out.Blocks = append([]BackupRepairJobBlock(nil), in.Blocks...)
	}
	return out
}

func cloneSnapshot(in snapshot) snapshot {
	data, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	var out snapshot
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}

func summaryPathCapacity(fileCount int, tombstoneCount int) (int, error) {
	maxInt := int(^uint(0) >> 1)
	if fileCount < 0 || tombstoneCount < 0 {
		return 0, fmt.Errorf("folder summary counts must be non-negative")
	}
	if tombstoneCount > maxInt-fileCount {
		return 0, fmt.Errorf("folder summary path count overflows int: files=%d tombstones=%d", fileCount, tombstoneCount)
	}
	return fileCount + tombstoneCount, nil
}

func peerFolderSummary(snap snapshot, peerID string, folderID string) (FolderSummary, error) {
	return folderSummary(snapshot{
		Folders:    map[string]map[string]block.Manifest{folderID: snap.PeerFolders[peerID][folderID]},
		Revisions:  map[string]map[string]uint64{folderID: snap.PeerRevisions[peerID][folderID]},
		Tombstones: map[string]map[string]uint64{folderID: snap.PeerTombstones[peerID][folderID]},
		Cursors:    map[string]uint64{folderID: snap.PeerCursors[peerID][folderID]},
	}, folderID)
}

func folderSummary(snap snapshot, folderID string) (FolderSummary, error) {
	files := snap.Folders[folderID]
	revisions := snap.Revisions[folderID]
	tombstones := snap.Tombstones[folderID]
	h := sha256.New()
	pathCap, err := summaryPathCapacity(len(files), len(tombstones))
	if err != nil {
		return FolderSummary{}, err
	}
	paths := make([]string, 0, pathCap)
	for rel := range files {
		paths = append(paths, "f\x00"+rel)
	}
	for rel := range tombstones {
		paths = append(paths, "d\x00"+rel)
	}
	sort.Strings(paths)
	for _, tagged := range paths {
		kind := tagged[:1]
		rel := tagged[2:]
		switch kind {
		case "f":
			manifestData, err := json.Marshal(files[rel])
			if err != nil {
				return FolderSummary{}, err
			}
			fmt.Fprintf(h, "file\x00%s\x00%d\x00", rel, revisions[rel])
			h.Write(manifestData)
			h.Write([]byte{0})
		case "d":
			fmt.Fprintf(h, "delete\x00%s\x00%d\x00", rel, tombstones[rel])
		}
	}
	return FolderSummary{FolderID: folderID, Cursor: snap.Cursors[folderID], Files: len(files), Tombstones: len(tombstones), StateHash: hex.EncodeToString(h.Sum(nil))}, nil
}

func folderSummaryAtCursor(snap snapshot, folderID string, cursor uint64) (FolderSummary, error) {
	if cursor >= snap.Cursors[folderID] {
		return folderSummary(snap, folderID)
	}
	manifests, historicalRevisions := manifestsAtCursorWithRevisions(snap, folderID, cursor)
	folders := map[string]map[string]block.Manifest{folderID: manifests}
	revisions := map[string]map[string]uint64{folderID: historicalRevisions}
	tombstones := map[string]map[string]uint64{folderID: {}}
	for rel, rev := range snap.Tombstones[folderID] {
		if rev <= cursor {
			delete(revisions[folderID], rel)
			tombstones[folderID][rel] = rev
		}
	}
	return folderSummary(snapshot{Folders: folders, Revisions: revisions, Tombstones: tombstones, Cursors: map[string]uint64{folderID: cursor}}, folderID)
}

func manifestsAtCursor(snap snapshot, folderID string, cursor uint64) map[string]block.Manifest {
	manifests, _ := manifestsAtCursorWithRevisions(snap, folderID, cursor)
	return manifests
}

func manifestsAtCursorWithRevisions(snap snapshot, folderID string, cursor uint64) (map[string]block.Manifest, map[string]uint64) {
	out := map[string]block.Manifest{}
	revisions := map[string]uint64{}
	for rel, history := range snap.ManifestHistory[folderID] {
		var selectedRev uint64
		var selected block.Manifest
		for rev, manifest := range history {
			if rev <= cursor && rev >= selectedRev {
				selectedRev = rev
				selected = manifest
			}
		}
		if selectedRev > 0 {
			out[rel] = selected
			revisions[rel] = selectedRev
		}
	}
	for rel, rev := range snap.Revisions[folderID] {
		if rev <= cursor {
			if _, ok := out[rel]; !ok {
				out[rel] = snap.Folders[folderID][rel]
				revisions[rel] = rev
			}
		}
	}
	for rel, rev := range snap.Tombstones[folderID] {
		if rev <= cursor {
			delete(out, rel)
			delete(revisions, rel)
		}
	}
	return out, revisions
}

func (s JSONStore) save(snap snapshot) error {
	if s.backend == nil {
		return fmt.Errorf("metadata store backend is not configured")
	}
	return s.backend.Save(snap)
}

func (b fileSnapshotBackend) Load() (snapshot, error) {
	data, err := os.ReadFile(b.path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot{Folders: map[string]map[string]block.Manifest{}}, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return snapshot{}, err
	}
	return snap, nil
}

func (b fileSnapshotBackend) Save(snap snapshot) error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

func (b fileSnapshotBackend) Close() error { return nil }
