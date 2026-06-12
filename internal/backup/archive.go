package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

const (
	ArchiveJobStatusPending  = "pending"
	ArchiveJobStatusArchived = "archived"
	ArchiveJobStatusFailed   = "failed"
)

type ArchiveIntakeResult struct {
	Archived int
	Reused   int
	Failed   int
}

type ArchiveIntakeWorkerOptions struct {
	ArchiveRoot string
	SourceRoots map[string]string
	Store       state.JSONStore
	Now         time.Time
	MaxJobs     int
	MaxAttempts int
	RetryDelay  time.Duration
}

type ArchiveIntakeWorkerResult struct {
	Processed int
	Archived  int
	Reused    int
	Failed    int
	Remaining int
}

type ArchiveProtectionStatusOptions struct {
	ArchiveRoot string
	Store       state.JSONStore
}

type ArchiveProtectionSnapshotStatus struct {
	TotalBlocks          int `json:"totalBlocks"`
	ProtectedBlocks      int `json:"protectedBlocks"`
	PendingBlocks        int `json:"pendingBlocks"`
	FailedBlocks         int `json:"failedBlocks"`
	MissingArchiveBlocks int `json:"missingArchiveBlocks"`
}

type ArchiveProtectionStatus struct {
	ArchiveProtectionSnapshotStatus
	Snapshots map[string]ArchiveProtectionSnapshotStatus `json:"snapshots,omitempty"`
}

type BackupArchiveScrubOptions struct {
	ArchiveRoot string
	Store       state.JSONStore
}

type BackupArchiveScrubIssue struct {
	Kind       string `json:"kind"`
	SnapshotID string `json:"snapshotId,omitempty"`
	FolderID   string `json:"folderId,omitempty"`
	JobID      string `json:"jobId,omitempty"`
	Path       string `json:"path,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
}

type BackupArchiveScrubResult struct {
	CheckedJobs     int                       `json:"checkedJobs"`
	ProtectedBlocks int                       `json:"protectedBlocks"`
	MissingBlocks   int                       `json:"missingBlocks"`
	CorruptBlocks   int                       `json:"corruptBlocks"`
	IncompleteJobs  int                       `json:"incompleteJobs"`
	OrphanBlocks    int                       `json:"orphanBlocks"`
	Issues          []BackupArchiveScrubIssue `json:"issues,omitempty"`
}

type BackupCheckpointScrubOptions struct {
	ArchiveRoot    string
	CheckpointRoot string
	Store          state.JSONStore
}

type BackupCheckpointScrubIssue struct {
	Kind       string `json:"kind"`
	SnapshotID string `json:"snapshotId,omitempty"`
	FolderID   string `json:"folderId,omitempty"`
	Path       string `json:"path,omitempty"`
	Message    string `json:"message,omitempty"`
}

type BackupCheckpointScrubResult struct {
	CheckedSnapshots     int                          `json:"checkedSnapshots"`
	AvailableCheckpoints int                          `json:"availableCheckpoints"`
	MissingCheckpoints   int                          `json:"missingCheckpoints"`
	CorruptCheckpoints   int                          `json:"corruptCheckpoints"`
	DegradedSnapshots    int                          `json:"degradedSnapshots"`
	Issues               []BackupCheckpointScrubIssue `json:"issues,omitempty"`
}

type BackupCheckpointRepairOptions struct {
	CheckpointRoot      string
	PeerCheckpointRoots map[string]string
	Store               state.JSONStore
}

type BackupCheckpointRepairAction struct {
	Kind           string `json:"kind"`
	SnapshotID     string `json:"snapshotId"`
	FolderID       string `json:"folderId"`
	CheckpointPath string `json:"checkpointPath"`
	SourcePath     string `json:"sourcePath"`
	SourcePeerID   string `json:"sourcePeerId"`
	Message        string `json:"message,omitempty"`
}

type BackupCheckpointRepairIssue struct {
	Kind       string `json:"kind"`
	SnapshotID string `json:"snapshotId"`
	FolderID   string `json:"folderId"`
	Path       string `json:"path,omitempty"`
	Message    string `json:"message,omitempty"`
}

type BackupCheckpointRepairResult struct {
	RepairedCheckpoints   int                            `json:"repairedCheckpoints"`
	UnresolvedCheckpoints int                            `json:"unresolvedCheckpoints"`
	Actions               []BackupCheckpointRepairAction `json:"actions,omitempty"`
	Unresolved            []BackupCheckpointRepairIssue  `json:"unresolved,omitempty"`
}

type BackupArchiveRepairPlanOptions struct {
	ArchiveRoot      string
	SourceRoots      map[string]string
	PeerArchiveRoots map[string]string
	Store            state.JSONStore
}

type BackupArchiveRepairOptions struct {
	ArchiveRoot      string
	SourceRoots      map[string]string
	PeerArchiveRoots map[string]string
	Store            state.JSONStore
	Now              time.Time
	JobID            string
}

type BackupArchiveRepairAction struct {
	Kind         string `json:"kind"`
	SnapshotID   string `json:"snapshotId,omitempty"`
	FolderID     string `json:"folderId,omitempty"`
	JobID        string `json:"jobId,omitempty"`
	Path         string `json:"path,omitempty"`
	Hash         string `json:"hash,omitempty"`
	SourceKind   string `json:"sourceKind,omitempty"`
	SourcePath   string `json:"sourcePath,omitempty"`
	SourcePeerID string `json:"sourcePeerId,omitempty"`
	Message      string `json:"message,omitempty"`
}

type BackupArchiveRepairIssue struct {
	Kind       string `json:"kind"`
	SnapshotID string `json:"snapshotId,omitempty"`
	FolderID   string `json:"folderId,omitempty"`
	JobID      string `json:"jobId,omitempty"`
	Path       string `json:"path,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Message    string `json:"message,omitempty"`
}

type BackupArchiveRepairPlan struct {
	RepairableBlocks int                         `json:"repairableBlocks"`
	UnresolvedBlocks int                         `json:"unresolvedBlocks"`
	Actions          []BackupArchiveRepairAction `json:"actions,omitempty"`
	Unresolved       []BackupArchiveRepairIssue  `json:"unresolved,omitempty"`
}

type BackupArchiveRepairResult struct {
	RepairedBlocks   int                         `json:"repairedBlocks"`
	UnresolvedBlocks int                         `json:"unresolvedBlocks"`
	Actions          []BackupArchiveRepairAction `json:"actions,omitempty"`
	Unresolved       []BackupArchiveRepairIssue  `json:"unresolved,omitempty"`
}

type SnapshotAvailabilityOptions struct {
	ArchiveRoot    string
	CheckpointRoot string
	Store          state.JSONStore
}

type SnapshotAvailabilitySnapshotStatus struct {
	SnapshotID            string                          `json:"snapshotId"`
	FolderID              string                          `json:"folderId"`
	MetadataPresent       bool                            `json:"metadataPresent"`
	DBCheckpointAvailable bool                            `json:"dbCheckpointAvailable"`
	ArchiveFullyProtected bool                            `json:"archiveFullyProtected"`
	Archive               ArchiveProtectionSnapshotStatus `json:"archive"`
}

type SnapshotAvailabilityStatus struct {
	TotalSnapshots            int                                           `json:"totalSnapshots"`
	MetadataSnapshots         int                                           `json:"metadataSnapshots"`
	ArchiveProtectedSnapshots int                                           `json:"archiveProtectedSnapshots"`
	DBCheckpointSnapshots     int                                           `json:"dbCheckpointSnapshots"`
	Snapshots                 map[string]SnapshotAvailabilitySnapshotStatus `json:"snapshots,omitempty"`
}

type BackupIntakePruneOptions struct {
	SourceRoot  string
	ArchiveRoot string
	Store       state.JSONStore
	Now         time.Time
	MinAge      time.Duration
}

type BackupIntakePruneResult struct {
	ScannedFiles int
	DeletedFiles int
	KeptFiles    int
}

type SnapshotRetentionOptions struct {
	Store       state.JSONStore
	KeepLast    int
	ArchiveRoot string
	JobID       string
	Now         time.Time
}

type SnapshotRetentionPlan struct {
	JobID                         string
	KeepLast                      int
	DeprecateSnapshots            []string
	DeleteSnapshots               []string
	Promotions                    []SnapshotRetentionPromotion
	ArchiveBlocksEligibleForSweep []ArchiveBlockRef
}

type SnapshotRetentionPromotion struct {
	FromSnapshotID string
	ToSnapshotID   string
	FolderID       string
	Path           string
}

type ArchiveBlockRef struct {
	Hash string
	Size int
}

type RestorePlanOptions struct {
	Store           state.JSONStore
	ArchiveRoot     string
	SnapshotID      string
	Paths           []string
	DestinationRoot string
	OriginalRoot    string
	AlternatePath   string
	DryRun          bool
	JobID           string
	Now             time.Time
}

type RestorePlan struct {
	SnapshotID    string            `json:"snapshotId"`
	FolderID      string            `json:"folderId"`
	Destination   string            `json:"destination"`
	DryRun        bool              `json:"dryRun"`
	TotalFiles    int               `json:"totalFiles"`
	TotalBytes    int64             `json:"totalBytes"`
	MissingBlocks int               `json:"missingBlocks"`
	Files         []RestorePlanFile `json:"files"`
}

type RestorePlanFile struct {
	Path             string        `json:"path"`
	DestinationPath  string        `json:"destinationPath"`
	Size             int64         `json:"size"`
	Blocks           int           `json:"blocks"`
	ArchiveAvailable bool          `json:"archiveAvailable"`
	MissingBlocks    []block.Block `json:"missingBlocks,omitempty"`
}

type RestoreResult struct {
	JobID          string `json:"jobId,omitempty"`
	SnapshotID     string `json:"snapshotId"`
	FolderID       string `json:"folderId"`
	Destination    string `json:"destination"`
	TotalFiles     int    `json:"totalFiles"`
	RestoredFiles  int    `json:"restoredFiles"`
	RestoredBytes  int64  `json:"restoredBytes"`
	SkippedFiles   int    `json:"skippedFiles"`
	RemainingFiles int    `json:"remainingFiles"`
}

type DatabaseReversionOptions struct {
	Store                  state.JSONStore
	CheckpointRoot         string
	SnapshotID             string
	AllowDatabaseReversion bool
	ConfirmSnapshotID      string
}

type DatabaseReversionPlan struct {
	SnapshotID     string `json:"snapshotId"`
	FolderID       string `json:"folderId"`
	CheckpointPath string `json:"checkpointPath"`
	Authorized     bool   `json:"authorized"`
}

type ArchiveIntakeProgressFunc func(ArchiveIntakeWorkerResult, error)

func ComputeArchiveProtectionStatus(opts ArchiveProtectionStatusOptions) (ArchiveProtectionStatus, error) {
	if opts.ArchiveRoot == "" {
		return ArchiveProtectionStatus{}, fmt.Errorf("archive root is required")
	}
	jobs, err := opts.Store.ListArchiveIntakeJobs("")
	if err != nil {
		return ArchiveProtectionStatus{}, err
	}
	return ComputeArchiveProtectionStatusFromJobs(opts.ArchiveRoot, jobs), nil
}

func ComputeArchiveProtectionStatusFromJobs(archiveRoot string, jobs []state.ArchiveIntakeJob) ArchiveProtectionStatus {
	status := ArchiveProtectionStatus{Snapshots: map[string]ArchiveProtectionSnapshotStatus{}}
	if archiveRoot == "" {
		return status
	}
	for _, job := range jobs {
		snap := status.Snapshots[job.SnapshotID]
		addProtectionJobStatus(&status.ArchiveProtectionSnapshotStatus, archiveRoot, job)
		addProtectionJobStatus(&snap, archiveRoot, job)
		status.Snapshots[job.SnapshotID] = snap
	}
	return status
}

func ScrubBackupArchive(opts BackupArchiveScrubOptions) (BackupArchiveScrubResult, error) {
	if opts.ArchiveRoot == "" {
		return BackupArchiveScrubResult{}, fmt.Errorf("archive root is required")
	}
	jobs, err := opts.Store.ListArchiveIntakeJobs("")
	if err != nil {
		return BackupArchiveScrubResult{}, err
	}
	result := BackupArchiveScrubResult{}
	referenced := map[string]struct{}{}
	for _, job := range jobs {
		result.CheckedJobs++
		hexHash := hex.EncodeToString(job.Block.Hash)
		if hexHash != "" {
			referenced[hexHash] = struct{}{}
		}
		if job.Status != ArchiveJobStatusArchived {
			result.IncompleteJobs++
			result.Issues = append(result.Issues, backupArchiveScrubIssue("archive_intake_incomplete", job, hexHash, fmt.Sprintf("archive intake job status is %q", job.Status)))
			continue
		}
		path, err := archiveBlockPath(opts.ArchiveRoot, job.Block)
		if err != nil {
			result.CorruptBlocks++
			result.Issues = append(result.Issues, backupArchiveScrubIssue("archive_block_corrupt", job, hexHash, err.Error()))
			continue
		}
		ok, err := verifyExistingArchiveBlock(path, job.Block)
		if err != nil {
			result.CorruptBlocks++
			result.Issues = append(result.Issues, backupArchiveScrubIssue("archive_block_corrupt", job, hexHash, err.Error()))
			continue
		}
		if !ok {
			result.MissingBlocks++
			result.Issues = append(result.Issues, backupArchiveScrubIssue("archive_block_missing", job, hexHash, "archive block is missing"))
			continue
		}
		result.ProtectedBlocks++
	}
	orphans, err := orphanArchiveBlocks(opts.ArchiveRoot, referenced)
	if err != nil {
		return BackupArchiveScrubResult{}, err
	}
	for _, hash := range orphans {
		result.OrphanBlocks++
		result.Issues = append(result.Issues, BackupArchiveScrubIssue{Kind: "archive_block_orphaned", Hash: hash, Message: "archive block has no intake job reference"})
	}
	return result, nil
}

func backupArchiveScrubIssue(kind string, job state.ArchiveIntakeJob, hash string, message string) BackupArchiveScrubIssue {
	return BackupArchiveScrubIssue{Kind: kind, SnapshotID: job.SnapshotID, FolderID: job.FolderID, JobID: job.ID, Path: job.Path, Hash: hash, Status: job.Status, Message: message}
}

func ScrubBackupCheckpoints(opts BackupCheckpointScrubOptions) (BackupCheckpointScrubResult, error) {
	if opts.CheckpointRoot == "" {
		return BackupCheckpointScrubResult{}, fmt.Errorf("checkpoint root is required")
	}
	availability, err := ComputeSnapshotAvailabilityStatus(SnapshotAvailabilityOptions{ArchiveRoot: opts.ArchiveRoot, CheckpointRoot: opts.CheckpointRoot, Store: opts.Store})
	if err != nil {
		return BackupCheckpointScrubResult{}, err
	}
	markers, err := opts.Store.ListSnapshotMarkers("")
	if err != nil {
		return BackupCheckpointScrubResult{}, err
	}
	result := BackupCheckpointScrubResult{}
	for _, marker := range markers {
		result.CheckedSnapshots++
		checkpointPath := filepath.Join(opts.CheckpointRoot, marker.FolderID, marker.ID+".json")
		checkpointOK := false
		data, err := os.ReadFile(checkpointPath)
		if os.IsNotExist(err) {
			result.MissingCheckpoints++
			result.Issues = append(result.Issues, backupCheckpointScrubIssue("checkpoint_missing", marker, checkpointPath, "offline database checkpoint is missing"))
		} else if err != nil {
			result.CorruptCheckpoints++
			result.Issues = append(result.Issues, backupCheckpointScrubIssue("checkpoint_corrupt", marker, checkpointPath, err.Error()))
		} else if !json.Valid(data) {
			result.CorruptCheckpoints++
			result.Issues = append(result.Issues, backupCheckpointScrubIssue("checkpoint_corrupt", marker, checkpointPath, "offline database checkpoint is not valid JSON"))
		} else {
			result.AvailableCheckpoints++
			checkpointOK = true
		}
		snapshot := availability.Snapshots[marker.ID]
		archiveOK := opts.ArchiveRoot == "" || snapshot.ArchiveFullyProtected
		if !checkpointOK || !archiveOK {
			result.DegradedSnapshots++
			result.Issues = append(result.Issues, backupCheckpointScrubIssue("snapshot_degraded", marker, checkpointPath, backupSnapshotDegradedMessage(checkpointOK, archiveOK)))
		}
	}
	return result, nil
}

func backupCheckpointScrubIssue(kind string, marker state.SnapshotMarker, path string, message string) BackupCheckpointScrubIssue {
	return BackupCheckpointScrubIssue{Kind: kind, SnapshotID: marker.ID, FolderID: marker.FolderID, Path: path, Message: message}
}

func ExecuteBackupCheckpointRepair(opts BackupCheckpointRepairOptions) (BackupCheckpointRepairResult, error) {
	if opts.CheckpointRoot == "" {
		return BackupCheckpointRepairResult{}, fmt.Errorf("checkpoint root is required")
	}
	markers, err := opts.Store.ListSnapshotMarkers("")
	if err != nil {
		return BackupCheckpointRepairResult{}, err
	}
	result := BackupCheckpointRepairResult{}
	for _, marker := range markers {
		localPath := snapshotCheckpointPathForRoot(opts.CheckpointRoot, marker)
		if ok, _ := verifyCheckpointFileForMarker(localPath, marker); ok {
			continue
		}
		action, ok := findCheckpointRepairSource(opts.PeerCheckpointRoots, marker, localPath)
		if !ok {
			result.UnresolvedCheckpoints++
			result.Unresolved = append(result.Unresolved, BackupCheckpointRepairIssue{Kind: "checkpoint_unrepairable", SnapshotID: marker.ID, FolderID: marker.FolderID, Path: localPath, Message: "no verified peer checkpoint source found"})
			continue
		}
		data, err := os.ReadFile(action.SourcePath)
		if err != nil {
			return result, err
		}
		if err := writeCheckpointBytesAtomic(localPath, data); err != nil {
			return result, err
		}
		result.RepairedCheckpoints++
		result.Actions = append(result.Actions, action)
	}
	return result, nil
}

func findCheckpointRepairSource(peerRoots map[string]string, marker state.SnapshotMarker, targetPath string) (BackupCheckpointRepairAction, bool) {
	peerIDs := make([]string, 0, len(peerRoots))
	for id := range peerRoots {
		peerIDs = append(peerIDs, id)
	}
	sort.Strings(peerIDs)
	for _, peerID := range peerIDs {
		root := peerRoots[peerID]
		if root == "" {
			continue
		}
		path := snapshotCheckpointPathForRoot(root, marker)
		if ok, _ := verifyCheckpointFileForMarker(path, marker); !ok {
			continue
		}
		return BackupCheckpointRepairAction{Kind: "restore_checkpoint", SnapshotID: marker.ID, FolderID: marker.FolderID, CheckpointPath: targetPath, SourcePath: path, SourcePeerID: peerID, Message: "verified matching offline database checkpoint exists on peer backup"}, true
	}
	return BackupCheckpointRepairAction{}, false
}

func snapshotCheckpointPathForRoot(root string, marker state.SnapshotMarker) string {
	return filepath.Join(root, marker.FolderID, marker.ID+".json")
}

func verifyCheckpointFileForMarker(path string, marker state.SnapshotMarker) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if !json.Valid(data) {
		return false, fmt.Errorf("checkpoint %q is not valid JSON", path)
	}
	var payload struct {
		SnapshotMarkers map[string]state.SnapshotMarker `json:"snapshotMarkers"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return false, err
	}
	stored, ok := payload.SnapshotMarkers[marker.ID]
	if !ok {
		return false, fmt.Errorf("checkpoint %q does not contain snapshot marker %q", path, marker.ID)
	}
	if stored.ID != marker.ID || stored.FolderID != marker.FolderID || stored.Cursor != marker.Cursor || stored.StateHash != marker.StateHash {
		return false, fmt.Errorf("checkpoint %q snapshot marker does not match local metadata", path)
	}
	return true, nil
}

func writeCheckpointBytesAtomic(path string, data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("checkpoint repair source is not valid JSON")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.checkpoint-repair-staging")
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

func PlanBackupArchiveRepair(opts BackupArchiveRepairPlanOptions) (BackupArchiveRepairPlan, error) {
	jobs, err := opts.Store.ListArchiveIntakeJobs("")
	if err != nil {
		return BackupArchiveRepairPlan{}, err
	}
	plan := BackupArchiveRepairPlan{}
	for _, job := range jobs {
		hash := hex.EncodeToString(job.Block.Hash)
		if job.Status == ArchiveJobStatusArchived && opts.ArchiveRoot != "" {
			path, err := archiveBlockPath(opts.ArchiveRoot, job.Block)
			if err == nil {
				if ok, verifyErr := verifyExistingArchiveBlock(path, job.Block); verifyErr == nil && ok {
					continue
				}
			}
		}
		source := findBackupArchiveRepairSource(opts, job)
		if source.SourceKind == "" {
			plan.UnresolvedBlocks++
			plan.Unresolved = append(plan.Unresolved, BackupArchiveRepairIssue{Kind: "archive_block_unrepairable", SnapshotID: job.SnapshotID, FolderID: job.FolderID, JobID: job.ID, Path: job.Path, Hash: hash, Message: "no verified live, retained, or peer archive source found"})
			continue
		}
		plan.RepairableBlocks++
		source.Kind = "restore_archive_block"
		source.SnapshotID = job.SnapshotID
		source.FolderID = job.FolderID
		source.JobID = job.ID
		source.Path = job.Path
		source.Hash = hash
		plan.Actions = append(plan.Actions, source)
	}
	return plan, nil
}

func ExecuteBackupArchiveRepair(opts BackupArchiveRepairOptions) (BackupArchiveRepairResult, error) {
	if opts.ArchiveRoot == "" {
		return BackupArchiveRepairResult{}, fmt.Errorf("archive root is required")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	jobs, err := opts.Store.ListArchiveIntakeJobs("")
	if err != nil {
		return BackupArchiveRepairResult{}, err
	}
	jobsByKey := make(map[string]state.ArchiveIntakeJob, len(jobs))
	for _, job := range jobs {
		jobsByKey[archiveRepairJobKey(job.SnapshotID, job.ID)] = job
	}
	plan, err := PlanBackupArchiveRepair(BackupArchiveRepairPlanOptions{ArchiveRoot: opts.ArchiveRoot, SourceRoots: opts.SourceRoots, PeerArchiveRoots: opts.PeerArchiveRoots, Store: opts.Store})
	if err != nil {
		return BackupArchiveRepairResult{}, err
	}
	result := BackupArchiveRepairResult{UnresolvedBlocks: plan.UnresolvedBlocks, Unresolved: append([]BackupArchiveRepairIssue(nil), plan.Unresolved...)}
	job := newBackupRepairJob(opts, plan)
	if job.ID != "" {
		if err := opts.Store.SaveBackupRepairJob(job); err != nil {
			return result, err
		}
	}
	for _, action := range plan.Actions {
		archiveJob, ok := jobsByKey[archiveRepairJobKey(action.SnapshotID, action.JobID)]
		if !ok {
			return result, fmt.Errorf("archive intake job %q not found", action.JobID)
		}
		if err := executeBackupArchiveRepairAction(opts.ArchiveRoot, action, archiveJob); err != nil {
			if job.ID != "" {
				job.Status = "failed"
				job.LastError = err.Error()
				job.UpdatedAt = opts.Now.Format(time.RFC3339)
				_ = opts.Store.SaveBackupRepairJob(job)
			}
			return result, err
		}
		for i := range jobs {
			if jobs[i].SnapshotID != archiveJob.SnapshotID || jobs[i].ID != archiveJob.ID {
				continue
			}
			jobs[i].Status = ArchiveJobStatusArchived
			jobs[i].LastError = ""
			jobs[i].NextAttemptAt = ""
			jobs[i].LastAttemptAt = opts.Now.Format(time.RFC3339)
			jobs[i].ArchivedAt = opts.Now.Format(time.RFC3339)
			break
		}
		result.RepairedBlocks++
		result.Actions = append(result.Actions, action)
		if job.ID != "" {
			markBackupRepairJobBlock(&job, action.SnapshotID, action.JobID, "repaired", action.SourceKind, "")
			job.RepairedBlocks = result.RepairedBlocks
			job.RemainingBlocks = maxInt(0, job.TotalBlocks-job.RepairedBlocks-job.UnresolvedBlocks)
			job.UpdatedAt = opts.Now.Format(time.RFC3339)
			if err := opts.Store.SaveBackupRepairJob(job); err != nil {
				return result, err
			}
		}
		if err := saveArchiveJobsBySnapshot(opts.Store, "", jobs); err != nil {
			return result, err
		}
	}
	if job.ID != "" {
		job.Status = "completed"
		job.RepairedBlocks = result.RepairedBlocks
		job.UnresolvedBlocks = result.UnresolvedBlocks
		job.RemainingBlocks = 0
		job.UpdatedAt = opts.Now.Format(time.RFC3339)
		job.CompletedAt = opts.Now.Format(time.RFC3339)
		if err := opts.Store.SaveBackupRepairJob(job); err != nil {
			return result, err
		}
	}
	return result, nil
}

func archiveRepairJobKey(snapshotID string, jobID string) string {
	return snapshotID + "\x00" + jobID
}

func newBackupRepairJob(opts BackupArchiveRepairOptions, plan BackupArchiveRepairPlan) state.BackupRepairJob {
	if opts.JobID == "" {
		return state.BackupRepairJob{}
	}
	now := opts.Now.Format(time.RFC3339)
	total := len(plan.Actions) + len(plan.Unresolved)
	job := state.BackupRepairJob{ID: opts.JobID, Status: "running", TotalBlocks: total, UnresolvedBlocks: len(plan.Unresolved), RemainingBlocks: total, StartedAt: now, UpdatedAt: now}
	for _, action := range plan.Actions {
		job.Blocks = append(job.Blocks, state.BackupRepairJobBlock{SnapshotID: action.SnapshotID, JobID: action.JobID, FolderID: action.FolderID, Path: action.Path, Hash: action.Hash, Status: "pending", SourceKind: action.SourceKind})
	}
	for _, issue := range plan.Unresolved {
		job.Blocks = append(job.Blocks, state.BackupRepairJobBlock{SnapshotID: issue.SnapshotID, JobID: issue.JobID, FolderID: issue.FolderID, Path: issue.Path, Hash: issue.Hash, Status: "unresolved", Error: issue.Message})
	}
	return job
}

func markBackupRepairJobBlock(job *state.BackupRepairJob, snapshotID string, archiveJobID string, status string, sourceKind string, message string) {
	for i := range job.Blocks {
		if job.Blocks[i].SnapshotID != snapshotID || job.Blocks[i].JobID != archiveJobID {
			continue
		}
		job.Blocks[i].Status = status
		job.Blocks[i].SourceKind = sourceKind
		job.Blocks[i].Error = message
		return
	}
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func executeBackupArchiveRepairAction(archiveRoot string, action BackupArchiveRepairAction, job state.ArchiveIntakeJob) error {
	var data []byte
	var err error
	switch action.SourceKind {
	case "live_file", "retained_backup_intake":
		data, err = readAndVerifySourceBlock(action.SourcePath, job.Block)
	case "peer_archive":
		data, err = os.ReadFile(action.SourcePath)
		if err == nil {
			err = verifyArchiveRepairBlockBytes(action.SourcePath, data, job.Block)
		}
	default:
		err = fmt.Errorf("unsupported archive repair source kind %q", action.SourceKind)
	}
	if err != nil {
		return err
	}
	targetPath, err := archiveBlockPath(archiveRoot, job.Block)
	if err != nil {
		return err
	}
	return writeArchiveBlockAtomic(targetPath, data)
}

func verifyArchiveRepairBlockBytes(path string, data []byte, b block.Block) error {
	if len(data) != b.Size {
		return fmt.Errorf("archive repair source %q has size %d, expected %d", path, len(data), b.Size)
	}
	hash := sha256.Sum256(data)
	if !bytes.Equal(hash[:], b.Hash) {
		return fmt.Errorf("archive repair source %q hash mismatch", path)
	}
	return nil
}

func findBackupArchiveRepairSource(opts BackupArchiveRepairPlanOptions, job state.ArchiveIntakeJob) BackupArchiveRepairAction {
	if sourceRoot := opts.SourceRoots[job.FolderID]; sourceRoot != "" {
		if action := findLocalBackupRepairSource(sourceRoot, job.Path, job.Block); action.SourceKind != "" {
			return action
		}
	}
	peerIDs := make([]string, 0, len(opts.PeerArchiveRoots))
	for id := range opts.PeerArchiveRoots {
		peerIDs = append(peerIDs, id)
	}
	sort.Strings(peerIDs)
	for _, peerID := range peerIDs {
		root := opts.PeerArchiveRoots[peerID]
		path, err := archiveBlockPath(root, job.Block)
		if err != nil {
			continue
		}
		if ok, err := verifyExistingArchiveBlock(path, job.Block); err == nil && ok {
			return BackupArchiveRepairAction{SourceKind: "peer_archive", SourcePath: path, SourcePeerID: peerID, Message: "verified matching block exists in peer backup archive"}
		}
	}
	return BackupArchiveRepairAction{}
}

func findLocalBackupRepairSource(sourceRoot string, rel string, b block.Block) BackupArchiveRepairAction {
	clean, err := cleanArchiveSourceRel(rel)
	if err != nil {
		return BackupArchiveRepairAction{}
	}
	livePath := filepath.Join(sourceRoot, filepath.FromSlash(clean))
	if _, err := readAndVerifySourceBlock(livePath, b); err == nil {
		return BackupArchiveRepairAction{SourceKind: "live_file", SourcePath: livePath, Message: "verified matching block exists in the live folder"}
	}
	retained, err := retainedBackupIntakeCandidates(sourceRoot, clean)
	if err != nil {
		return BackupArchiveRepairAction{}
	}
	for _, candidate := range retained {
		if _, err := readAndVerifySourceBlock(candidate, b); err == nil {
			return BackupArchiveRepairAction{SourceKind: "retained_backup_intake", SourcePath: candidate, Message: "verified matching block exists in retained backup-intake bytes"}
		}
	}
	return BackupArchiveRepairAction{}
}

func backupSnapshotDegradedMessage(checkpointOK bool, archiveOK bool) string {
	if !checkpointOK && !archiveOK {
		return "snapshot is degraded: offline checkpoint and archive block protection are unavailable"
	}
	if !checkpointOK {
		return "snapshot is degraded: offline checkpoint is unavailable"
	}
	return "snapshot is degraded: archive block protection is incomplete"
}

func orphanArchiveBlocks(archiveRoot string, referenced map[string]struct{}) ([]string, error) {
	blocksRoot := filepath.Join(archiveRoot, "blocks")
	if _, err := os.Stat(blocksRoot); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var orphans []string
	err := filepath.WalkDir(blocksRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		hash := filepath.Base(path)
		if _, ok := referenced[hash]; ok {
			return nil
		}
		orphans = append(orphans, hash)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(orphans)
	return orphans, nil
}

func ComputeSnapshotAvailabilityStatus(opts SnapshotAvailabilityOptions) (SnapshotAvailabilityStatus, error) {
	markers, err := opts.Store.ListSnapshotMarkers("")
	if err != nil {
		return SnapshotAvailabilityStatus{}, err
	}
	jobs, err := opts.Store.ListArchiveIntakeJobs("")
	if err != nil {
		return SnapshotAvailabilityStatus{}, err
	}
	archiveBySnapshot := map[string]ArchiveProtectionSnapshotStatus{}
	if opts.ArchiveRoot != "" {
		for _, job := range jobs {
			snap := archiveBySnapshot[job.SnapshotID]
			addProtectionJobStatus(&snap, opts.ArchiveRoot, job)
			archiveBySnapshot[job.SnapshotID] = snap
		}
	}
	status := SnapshotAvailabilityStatus{Snapshots: map[string]SnapshotAvailabilitySnapshotStatus{}}
	for _, marker := range markers {
		snapshot := SnapshotAvailabilitySnapshotStatus{
			SnapshotID:      marker.ID,
			FolderID:        marker.FolderID,
			MetadataPresent: true,
			Archive:         archiveBySnapshot[marker.ID],
		}
		snapshot.ArchiveFullyProtected = snapshot.Archive.TotalBlocks > 0 && snapshot.Archive.ProtectedBlocks == snapshot.Archive.TotalBlocks
		if opts.CheckpointRoot != "" {
			snapshot.DBCheckpointAvailable = snapshotCheckpointExists(opts.CheckpointRoot, marker)
		}
		status.TotalSnapshots++
		status.MetadataSnapshots++
		if snapshot.ArchiveFullyProtected {
			status.ArchiveProtectedSnapshots++
		}
		if snapshot.DBCheckpointAvailable {
			status.DBCheckpointSnapshots++
		}
		status.Snapshots[marker.ID] = snapshot
	}
	return status, nil
}

func PlanSnapshotRetention(opts SnapshotRetentionOptions) (SnapshotRetentionPlan, error) {
	if opts.KeepLast < 1 {
		return SnapshotRetentionPlan{}, fmt.Errorf("keep last must be at least 1")
	}
	markers, err := opts.Store.ListSnapshotMarkers("")
	if err != nil {
		return SnapshotRetentionPlan{}, err
	}
	byFolder := map[string][]state.SnapshotMarker{}
	for _, marker := range markers {
		byFolder[marker.FolderID] = append(byFolder[marker.FolderID], marker)
	}
	plan := SnapshotRetentionPlan{KeepLast: opts.KeepLast}
	deleteSet := map[string]state.SnapshotMarker{}
	retained := make([]state.SnapshotMarker, 0, len(markers))
	for _, folderMarkers := range byFolder {
		sort.Slice(folderMarkers, func(i, j int) bool {
			if folderMarkers[i].CreatedAt != folderMarkers[j].CreatedAt {
				return folderMarkers[i].CreatedAt < folderMarkers[j].CreatedAt
			}
			return folderMarkers[i].ID < folderMarkers[j].ID
		})
		cutoff := len(folderMarkers) - opts.KeepLast
		if cutoff < 0 {
			cutoff = 0
		}
		for i, marker := range folderMarkers {
			if i >= cutoff || marker.Pinned {
				retained = append(retained, marker)
				continue
			}
			if !marker.Deprecated {
				plan.DeprecateSnapshots = append(plan.DeprecateSnapshots, marker.ID)
				retained = append(retained, marker)
				continue
			}
			next, ok := nextRetainedSnapshot(folderMarkers, i, opts.KeepLast)
			if !ok {
				retained = append(retained, marker)
				continue
			}
			plan.DeleteSnapshots = append(plan.DeleteSnapshots, marker.ID)
			deleteSet[marker.ID] = marker
			promotions, err := snapshotRetentionPromotions(opts.Store, marker, next)
			if err != nil {
				return SnapshotRetentionPlan{}, err
			}
			plan.Promotions = append(plan.Promotions, promotions...)
		}
	}
	blocks, err := snapshotRetentionSweepBlocks(opts.Store, deleteSet, retained)
	if err != nil {
		return SnapshotRetentionPlan{}, err
	}
	plan.ArchiveBlocksEligibleForSweep = blocks
	sort.Strings(plan.DeprecateSnapshots)
	sort.Strings(plan.DeleteSnapshots)
	sort.Slice(plan.Promotions, func(i, j int) bool {
		if plan.Promotions[i].FromSnapshotID != plan.Promotions[j].FromSnapshotID {
			return plan.Promotions[i].FromSnapshotID < plan.Promotions[j].FromSnapshotID
		}
		return plan.Promotions[i].Path < plan.Promotions[j].Path
	})
	return plan, nil
}

func ExecuteSnapshotRetention(opts SnapshotRetentionOptions) (SnapshotRetentionPlan, error) {
	plan, err := PlanSnapshotRetention(opts)
	if err != nil {
		return SnapshotRetentionPlan{}, err
	}
	plan.JobID = retentionJobID(opts)
	job := newBackupRetentionJob(opts, plan)
	if job.ID != "" {
		if err := opts.Store.SaveBackupRetentionJob(job); err != nil {
			return plan, err
		}
	}
	for _, id := range plan.DeprecateSnapshots {
		marker, ok, err := opts.Store.LoadSnapshotMarker(id)
		if err != nil {
			return SnapshotRetentionPlan{}, err
		}
		if !ok {
			return SnapshotRetentionPlan{}, fmt.Errorf("snapshot %q not found", id)
		}
		marker.Deprecated = true
		if err := opts.Store.SaveSnapshotMarker(marker); err != nil {
			return SnapshotRetentionPlan{}, err
		}
	}
	for _, promotion := range plan.Promotions {
		to, ok, err := opts.Store.LoadSnapshotMarker(promotion.ToSnapshotID)
		if err != nil {
			return SnapshotRetentionPlan{}, err
		}
		if !ok {
			return SnapshotRetentionPlan{}, fmt.Errorf("snapshot %q not found", promotion.ToSnapshotID)
		}
		manifests, err := opts.Store.SnapshotManifests(promotion.FromSnapshotID)
		if err != nil {
			return SnapshotRetentionPlan{}, err
		}
		manifest, ok := manifests[promotion.Path]
		if !ok {
			return SnapshotRetentionPlan{}, fmt.Errorf("snapshot %q missing promoted manifest %q", promotion.FromSnapshotID, promotion.Path)
		}
		if err := opts.Store.PreserveSnapshotManifest(promotion.FolderID, promotion.Path, to.Cursor, manifest); err != nil {
			return SnapshotRetentionPlan{}, err
		}
	}
	for _, id := range plan.DeleteSnapshots {
		if err := opts.Store.DeleteSnapshotMarker(id); err != nil {
			return SnapshotRetentionPlan{}, err
		}
	}
	if opts.ArchiveRoot != "" {
		for _, ref := range plan.ArchiveBlocksEligibleForSweep {
			if err := removeArchiveBlock(opts.ArchiveRoot, ref); err != nil {
				return SnapshotRetentionPlan{}, err
			}
		}
	}
	if job.ID != "" {
		job.DeprecatedSnapshots = len(plan.DeprecateSnapshots)
		job.DeletedSnapshots = len(plan.DeleteSnapshots)
		job.PromotedManifests = len(plan.Promotions)
		job.SweptArchiveBlocks = len(plan.ArchiveBlocksEligibleForSweep)
		job.RemainingOperations = 0
		job.Status = "completed"
		job.CompletedAt = retentionJobTime(opts).Format(time.RFC3339)
		job.UpdatedAt = job.CompletedAt
		if err := opts.Store.SaveBackupRetentionJob(job); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

func newBackupRetentionJob(opts SnapshotRetentionOptions, plan SnapshotRetentionPlan) state.BackupRetentionJob {
	jobID := retentionJobID(opts)
	if jobID == "" {
		return state.BackupRetentionJob{}
	}
	now := retentionJobTime(opts).Format(time.RFC3339)
	total := len(plan.DeprecateSnapshots) + len(plan.DeleteSnapshots) + len(plan.Promotions) + len(plan.ArchiveBlocksEligibleForSweep)
	return state.BackupRetentionJob{ID: jobID, Status: "running", KeepLast: plan.KeepLast, TotalOperations: total, RemainingOperations: total, StartedAt: now, UpdatedAt: now}
}

func retentionJobID(opts SnapshotRetentionOptions) string {
	if opts.JobID != "" {
		return opts.JobID
	}
	return fmt.Sprintf("retention-%d", retentionJobTime(opts).UnixNano())
}

func retentionJobTime(opts SnapshotRetentionOptions) time.Time {
	if !opts.Now.IsZero() {
		return opts.Now.UTC()
	}
	return time.Now().UTC()
}

func removeArchiveBlock(archiveRoot string, ref ArchiveBlockRef) error {
	hash, err := hex.DecodeString(ref.Hash)
	if err != nil {
		return fmt.Errorf("decode archive block hash %q: %w", ref.Hash, err)
	}
	path, err := archiveBlockPath(archiveRoot, block.Block{Hash: hash, Size: ref.Size})
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func nextRetainedSnapshot(markers []state.SnapshotMarker, deleteIndex int, keepLast int) (state.SnapshotMarker, bool) {
	cutoff := len(markers) - keepLast
	if cutoff < 0 {
		cutoff = 0
	}
	for i := deleteIndex + 1; i < len(markers); i++ {
		if i >= cutoff || markers[i].Pinned || !markers[i].Deprecated {
			return markers[i], true
		}
	}
	return state.SnapshotMarker{}, false
}

func snapshotRetentionPromotions(store state.JSONStore, from state.SnapshotMarker, to state.SnapshotMarker) ([]SnapshotRetentionPromotion, error) {
	if from.FolderID != to.FolderID {
		return nil, nil
	}
	changes, err := store.ChangesSince(from.FolderID, from.Cursor)
	if err != nil {
		return nil, err
	}
	changed := map[string]struct{}{}
	for _, change := range changes.Changes {
		if change.Revision > to.Cursor {
			continue
		}
		changed[change.Path] = struct{}{}
		if change.FromPath != "" {
			changed[change.FromPath] = struct{}{}
		}
	}
	manifests, err := store.SnapshotManifests(to.ID)
	if err != nil {
		return nil, err
	}
	promotions := make([]SnapshotRetentionPromotion, 0)
	for rel := range manifests {
		if _, ok := changed[rel]; ok {
			continue
		}
		promotions = append(promotions, SnapshotRetentionPromotion{FromSnapshotID: from.ID, ToSnapshotID: to.ID, FolderID: from.FolderID, Path: rel})
	}
	return promotions, nil
}

func snapshotRetentionSweepBlocks(store state.JSONStore, deleteSet map[string]state.SnapshotMarker, retained []state.SnapshotMarker) ([]ArchiveBlockRef, error) {
	if len(deleteSet) == 0 {
		return nil, nil
	}
	deleteBlocks := map[string]ArchiveBlockRef{}
	for _, marker := range deleteSet {
		manifests, err := store.SnapshotManifests(marker.ID)
		if err != nil {
			return nil, err
		}
		addManifestBlocks(deleteBlocks, manifests)
	}
	referenced := map[string]struct{}{}
	for _, marker := range retained {
		if _, deleting := deleteSet[marker.ID]; deleting {
			continue
		}
		manifests, err := store.SnapshotManifests(marker.ID)
		if err != nil {
			return nil, err
		}
		markManifestBlocks(referenced, manifests)
	}
	folders := map[string]struct{}{}
	for _, marker := range retained {
		folders[marker.FolderID] = struct{}{}
	}
	for _, marker := range deleteSet {
		folders[marker.FolderID] = struct{}{}
	}
	for folderID := range folders {
		manifests, err := store.ListManifests(folderID)
		if err != nil {
			return nil, err
		}
		markManifestBlocks(referenced, manifests)
	}
	blocks := make([]ArchiveBlockRef, 0)
	for hash, ref := range deleteBlocks {
		if _, ok := referenced[hash]; ok {
			continue
		}
		blocks = append(blocks, ref)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Hash < blocks[j].Hash })
	return blocks, nil
}

func addManifestBlocks(out map[string]ArchiveBlockRef, manifests map[string]block.Manifest) {
	for _, manifest := range manifests {
		for _, b := range manifest.Blocks {
			hash := hex.EncodeToString(b.Hash)
			if hash == "" {
				continue
			}
			out[hash] = ArchiveBlockRef{Hash: hash, Size: b.Size}
		}
	}
}

func markManifestBlocks(out map[string]struct{}, manifests map[string]block.Manifest) {
	for _, manifest := range manifests {
		for _, b := range manifest.Blocks {
			hash := hex.EncodeToString(b.Hash)
			if hash != "" {
				out[hash] = struct{}{}
			}
		}
	}
}

func snapshotCheckpointExists(root string, marker state.SnapshotMarker) bool {
	path := filepath.Join(root, marker.FolderID, marker.ID+".json")
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func PlanSnapshotRestore(opts RestorePlanOptions) (RestorePlan, error) {
	if opts.SnapshotID == "" {
		return RestorePlan{}, fmt.Errorf("snapshot id is required")
	}
	if opts.ArchiveRoot == "" {
		return RestorePlan{}, fmt.Errorf("archive root is required")
	}
	destinationRoot := opts.DestinationRoot
	if destinationRoot == "" {
		destinationRoot = opts.OriginalRoot
	}
	if destinationRoot == "" {
		return RestorePlan{}, fmt.Errorf("destination root is required")
	}
	marker, ok, err := opts.Store.LoadSnapshotMarker(opts.SnapshotID)
	if err != nil {
		return RestorePlan{}, err
	}
	if !ok {
		return RestorePlan{}, fmt.Errorf("snapshot %q not found", opts.SnapshotID)
	}
	manifests, err := opts.Store.SnapshotManifests(opts.SnapshotID)
	if err != nil {
		return RestorePlan{}, err
	}
	selected, err := normalizeRestoreSelections(opts.Paths)
	if err != nil {
		return RestorePlan{}, err
	}
	return buildRestorePlan(marker, manifests, opts.ArchiveRoot, destinationRoot, opts.AlternatePath, selected, opts.DryRun)
}

func ExecuteSnapshotRestore(opts RestorePlanOptions) (RestoreResult, error) {
	if opts.SnapshotID == "" {
		return RestoreResult{}, fmt.Errorf("snapshot id is required")
	}
	if opts.ArchiveRoot == "" {
		return RestoreResult{}, fmt.Errorf("archive root is required")
	}
	destinationRoot := opts.DestinationRoot
	if destinationRoot == "" {
		destinationRoot = opts.OriginalRoot
	}
	if destinationRoot == "" {
		return RestoreResult{}, fmt.Errorf("destination root is required")
	}
	marker, ok, err := opts.Store.LoadSnapshotMarker(opts.SnapshotID)
	if err != nil {
		return RestoreResult{}, err
	}
	if !ok {
		return RestoreResult{}, fmt.Errorf("snapshot %q not found", opts.SnapshotID)
	}
	manifests, err := opts.Store.SnapshotManifests(opts.SnapshotID)
	if err != nil {
		return RestoreResult{}, err
	}
	selected, err := normalizeRestoreSelections(opts.Paths)
	if err != nil {
		return RestoreResult{}, err
	}
	plan, err := buildRestorePlan(marker, manifests, opts.ArchiveRoot, destinationRoot, opts.AlternatePath, selected, false)
	if err != nil {
		return RestoreResult{}, err
	}
	if plan.MissingBlocks > 0 {
		return RestoreResult{}, fmt.Errorf("restore plan has %d missing archive blocks", plan.MissingBlocks)
	}
	result := RestoreResult{JobID: restoreJobID(opts), SnapshotID: plan.SnapshotID, FolderID: plan.FolderID, Destination: plan.Destination, TotalFiles: len(plan.Files), RemainingFiles: len(plan.Files)}
	job := newBackupRestoreJob(opts, plan, result)
	if job.ID != "" {
		if err := opts.Store.SaveBackupRestoreJob(job); err != nil {
			return result, err
		}
	}
	for i, file := range plan.Files {
		manifest := manifests[file.Path]
		if destinationAlreadyRestored(file.DestinationPath, manifest) {
			result.SkippedFiles++
			result.RemainingFiles--
			if job.ID != "" {
				job.SkippedFiles = result.SkippedFiles
				job.RemainingFiles = result.RemainingFiles
				job.Files[i].Status = "skipped"
				job.UpdatedAt = restoreJobTime(opts).Format(time.RFC3339)
				if err := opts.Store.SaveBackupRestoreJob(job); err != nil {
					return result, err
				}
			}
			continue
		}
		if err := restoreOneFileFromArchive(opts.ArchiveRoot, file.DestinationPath, manifest); err != nil {
			if job.ID != "" {
				job.Status = "failed"
				job.LastError = err.Error()
				job.Files[i].Status = "failed"
				job.Files[i].Error = err.Error()
				job.UpdatedAt = restoreJobTime(opts).Format(time.RFC3339)
				_ = opts.Store.SaveBackupRestoreJob(job)
			}
			return result, err
		}
		result.RestoredFiles++
		result.RestoredBytes += manifest.Size
		result.RemainingFiles--
		if job.ID != "" {
			job.RestoredFiles = result.RestoredFiles
			job.RestoredBytes = result.RestoredBytes
			job.RemainingFiles = result.RemainingFiles
			job.Files[i].Status = "restored"
			job.UpdatedAt = restoreJobTime(opts).Format(time.RFC3339)
			if err := opts.Store.SaveBackupRestoreJob(job); err != nil {
				return result, err
			}
		}
	}
	if job.ID != "" {
		job.Status = "completed"
		job.CompletedAt = restoreJobTime(opts).Format(time.RFC3339)
		job.UpdatedAt = job.CompletedAt
		if err := opts.Store.SaveBackupRestoreJob(job); err != nil {
			return result, err
		}
	}
	return result, nil
}

func newBackupRestoreJob(opts RestorePlanOptions, plan RestorePlan, result RestoreResult) state.BackupRestoreJob {
	jobID := restoreJobID(opts)
	if jobID == "" {
		return state.BackupRestoreJob{}
	}
	now := restoreJobTime(opts).Format(time.RFC3339)
	job := state.BackupRestoreJob{ID: jobID, SnapshotID: plan.SnapshotID, FolderID: plan.FolderID, Destination: plan.Destination, Status: "running", TotalFiles: result.TotalFiles, RemainingFiles: result.RemainingFiles, StartedAt: now, UpdatedAt: now}
	for _, file := range plan.Files {
		job.Files = append(job.Files, state.BackupRestoreJobFile{Path: file.Path, DestinationPath: file.DestinationPath, Status: "pending", Size: file.Size})
	}
	return job
}

func restoreJobID(opts RestorePlanOptions) string {
	if opts.JobID != "" {
		return opts.JobID
	}
	if opts.SnapshotID == "" {
		return ""
	}
	return fmt.Sprintf("restore-%s-%d", opts.SnapshotID, restoreJobTime(opts).UnixNano())
}

func restoreJobTime(opts RestorePlanOptions) time.Time {
	if !opts.Now.IsZero() {
		return opts.Now.UTC()
	}
	return time.Now().UTC()
}

func destinationAlreadyRestored(path string, manifest block.Manifest) bool {
	return verifyRestoredManifest(path, manifest) == nil
}

func AuthorizeSnapshotDatabaseReversion(opts DatabaseReversionOptions) (DatabaseReversionPlan, error) {
	if opts.SnapshotID == "" {
		return DatabaseReversionPlan{}, fmt.Errorf("snapshot id is required")
	}
	if !opts.AllowDatabaseReversion {
		return DatabaseReversionPlan{}, fmt.Errorf("database reversion requires explicit authorization")
	}
	if opts.ConfirmSnapshotID != opts.SnapshotID {
		return DatabaseReversionPlan{}, fmt.Errorf("database reversion confirmation must exactly match snapshot id %q", opts.SnapshotID)
	}
	if opts.CheckpointRoot == "" {
		return DatabaseReversionPlan{}, fmt.Errorf("database checkpoint root is required")
	}
	marker, ok, err := opts.Store.LoadSnapshotMarker(opts.SnapshotID)
	if err != nil {
		return DatabaseReversionPlan{}, err
	}
	if !ok {
		return DatabaseReversionPlan{}, fmt.Errorf("snapshot %q not found", opts.SnapshotID)
	}
	checkpointPath := filepath.Join(opts.CheckpointRoot, marker.FolderID, marker.ID+".json")
	info, err := os.Stat(checkpointPath)
	if err != nil {
		return DatabaseReversionPlan{}, fmt.Errorf("snapshot database checkpoint %q is not available: %w", checkpointPath, err)
	}
	if info.IsDir() {
		return DatabaseReversionPlan{}, fmt.Errorf("snapshot database checkpoint %q is a directory", checkpointPath)
	}
	return DatabaseReversionPlan{SnapshotID: marker.ID, FolderID: marker.FolderID, CheckpointPath: checkpointPath, Authorized: true}, nil
}

func buildRestorePlan(marker state.SnapshotMarker, manifests map[string]block.Manifest, archiveRoot string, destinationRoot string, alternatePath string, selected map[string]struct{}, dryRun bool) (RestorePlan, error) {
	if alternatePath != "" && len(selected) == 1 {
		clean, err := cleanArchiveSourceRel(alternatePath)
		if err != nil {
			return RestorePlan{}, fmt.Errorf("restore alternate path: %w", err)
		}
		alternatePath = clean
	}
	plan := RestorePlan{SnapshotID: marker.ID, FolderID: marker.FolderID, Destination: destinationRoot, DryRun: dryRun}
	paths := make([]string, 0, len(manifests))
	for rel := range manifests {
		if restorePathSelected(rel, selected) {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	for _, rel := range paths {
		manifest := manifests[rel]
		filePlan := RestorePlanFile{Path: rel, DestinationPath: restoreDestinationPath(destinationRoot, alternatePath, rel, len(selected) == 1), Size: manifest.Size, Blocks: len(manifest.Blocks), ArchiveAvailable: true}
		for _, b := range manifest.Blocks {
			path, err := archiveBlockPath(archiveRoot, b)
			if err != nil {
				return RestorePlan{}, err
			}
			ok, err := verifyExistingArchiveBlock(path, b)
			if err != nil || !ok {
				filePlan.ArchiveAvailable = false
				filePlan.MissingBlocks = append(filePlan.MissingBlocks, b)
			}
		}
		plan.TotalFiles++
		plan.TotalBytes += manifest.Size
		plan.MissingBlocks += len(filePlan.MissingBlocks)
		plan.Files = append(plan.Files, filePlan)
	}
	return plan, nil
}

func restoreOneFileFromArchive(archiveRoot string, destinationPath string, manifest block.Manifest) error {
	if destinationPath == "" {
		return fmt.Errorf("restore destination path is required")
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destinationPath), "."+filepath.Base(destinationPath)+".*.restore-staging")
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
	for _, b := range manifest.Blocks {
		archivePath, err := archiveBlockPath(archiveRoot, b)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		data, err := os.ReadFile(archivePath)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if len(data) != b.Size {
			_ = tmp.Close()
			return fmt.Errorf("archive block %q has size %d, expected %d", archivePath, len(data), b.Size)
		}
		hash := sha256.Sum256(data)
		if !bytes.Equal(hash[:], b.Hash) {
			_ = tmp.Close()
			return fmt.Errorf("archive block %q hash mismatch", archivePath)
		}
		if _, err := tmp.WriteAt(data, b.Offset); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Truncate(manifest.Size); err != nil {
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
	if err := verifyRestoredManifest(tmpName, manifest); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destinationPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func verifyRestoredManifest(path string, manifest block.Manifest) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() != manifest.Size {
		return fmt.Errorf("restored file %q has size %d, expected %d", path, info.Size(), manifest.Size)
	}
	for _, b := range manifest.Blocks {
		buf := make([]byte, b.Size)
		if _, err := file.ReadAt(buf, b.Offset); err != nil {
			return err
		}
		hash := sha256.Sum256(buf)
		if !bytes.Equal(hash[:], b.Hash) {
			return fmt.Errorf("restored file %q block %d hash mismatch", path, b.Index)
		}
	}
	return nil
}

func normalizeRestoreSelections(paths []string) (map[string]struct{}, error) {
	selected := map[string]struct{}{}
	for _, rel := range paths {
		clean, err := cleanArchiveSourceRel(rel)
		if err != nil {
			return nil, err
		}
		selected[clean] = struct{}{}
	}
	return selected, nil
}

func restorePathSelected(rel string, selected map[string]struct{}) bool {
	if len(selected) == 0 {
		return true
	}
	for path := range selected {
		if rel == path || strings.HasPrefix(rel, path+"/") {
			return true
		}
	}
	return false
}

func restoreDestinationPath(root string, alternate string, rel string, singleSelection bool) string {
	if alternate != "" && singleSelection {
		return filepath.Join(root, filepath.FromSlash(alternate))
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

func addProtectionJobStatus(status *ArchiveProtectionSnapshotStatus, archiveRoot string, job state.ArchiveIntakeJob) {
	status.TotalBlocks++
	switch job.Status {
	case ArchiveJobStatusArchived:
		if archiveJobBlockProtected(archiveRoot, job) {
			status.ProtectedBlocks++
		} else {
			status.MissingArchiveBlocks++
		}
	case ArchiveJobStatusFailed:
		status.FailedBlocks++
	default:
		status.PendingBlocks++
	}
}

func archiveJobBlockProtected(archiveRoot string, job state.ArchiveIntakeJob) bool {
	path, err := archiveBlockPath(archiveRoot, job.Block)
	if err != nil {
		return false
	}
	ok, err := verifyExistingArchiveBlock(path, job.Block)
	return err == nil && ok
}

func RunArchiveIntakeWorker(ctx context.Context, opts ArchiveIntakeWorkerOptions, interval time.Duration, onProgress ArchiveIntakeProgressFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = time.Minute
	}
	for {
		result, err := RunArchiveIntakeOnce(opts)
		if onProgress != nil {
			onProgress(result, err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func RunArchiveIntakeOnce(opts ArchiveIntakeWorkerOptions) (ArchiveIntakeWorkerResult, error) {
	if opts.ArchiveRoot == "" {
		return ArchiveIntakeWorkerResult{}, fmt.Errorf("archive root is required")
	}
	if len(opts.SourceRoots) == 0 {
		return ArchiveIntakeWorkerResult{}, fmt.Errorf("at least one source root is required")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	maxJobs := opts.MaxJobs
	if maxJobs <= 0 {
		maxJobs = 1
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	retryDelay := opts.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Minute
	}

	jobs, err := opts.Store.ListArchiveIntakeJobs("")
	if err != nil {
		return ArchiveIntakeWorkerResult{}, err
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].SnapshotID != jobs[j].SnapshotID {
			return jobs[i].SnapshotID < jobs[j].SnapshotID
		}
		return jobs[i].ID < jobs[j].ID
	})

	result := ArchiveIntakeWorkerResult{}
	var firstErr error
	for i := range jobs {
		if jobs[i].Status == ArchiveJobStatusArchived {
			continue
		}
		if !archiveJobReady(jobs[i], now, maxAttempts) {
			result.Remaining++
			continue
		}
		if result.Processed >= maxJobs {
			result.Remaining++
			continue
		}
		sourceRoot := opts.SourceRoots[jobs[i].FolderID]
		if sourceRoot == "" {
			err := fmt.Errorf("source root for folder %q is required", jobs[i].FolderID)
			if firstErr == nil {
				firstErr = err
			}
			jobs[i].Attempts++
			markArchiveJobFailed(&jobs[i], now, retryDelay, err)
			result.Processed++
			result.Failed++
			continue
		}
		reused, err := archiveOneBlock(sourceRoot, opts.ArchiveRoot, jobs[i].Path, jobs[i].Block)
		jobs[i].Attempts++
		jobs[i].LastAttemptAt = now.Format(time.RFC3339)
		result.Processed++
		if err != nil {
			markArchiveJobFailed(&jobs[i], now, retryDelay, err)
			result.Failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		jobs[i].Status = ArchiveJobStatusArchived
		jobs[i].LastError = ""
		jobs[i].NextAttemptAt = ""
		jobs[i].ArchivedAt = now.Format(time.RFC3339)
		if reused {
			result.Reused++
		} else {
			result.Archived++
		}
	}
	if err := saveArchiveJobsBySnapshot(opts.Store, "", jobs); err != nil {
		return result, err
	}
	result.Remaining = countRemainingArchiveJobs(jobs)
	return result, firstErr
}

func archiveJobReady(job state.ArchiveIntakeJob, now time.Time, maxAttempts int) bool {
	if job.Attempts >= maxAttempts {
		return false
	}
	if job.Status != ArchiveJobStatusFailed || job.NextAttemptAt == "" {
		return true
	}
	next, err := time.Parse(time.RFC3339, job.NextAttemptAt)
	if err != nil {
		return true
	}
	return !now.Before(next)
}

func markArchiveJobFailed(job *state.ArchiveIntakeJob, now time.Time, retryDelay time.Duration, err error) {
	job.Status = ArchiveJobStatusFailed
	job.LastAttemptAt = now.Format(time.RFC3339)
	job.NextAttemptAt = now.Add(retryDelay).Format(time.RFC3339)
	job.LastError = err.Error()
}

func countRemainingArchiveJobs(jobs []state.ArchiveIntakeJob) int {
	remaining := 0
	for _, job := range jobs {
		if job.Status != ArchiveJobStatusArchived {
			remaining++
		}
	}
	return remaining
}

func ProcessArchiveIntakeJobs(sourceRoot string, archiveRoot string, store state.JSONStore, snapshotID string) (ArchiveIntakeResult, error) {
	if archiveRoot == "" {
		return ArchiveIntakeResult{}, fmt.Errorf("archive root is required")
	}
	jobs, err := store.ListArchiveIntakeJobs(snapshotID)
	if err != nil {
		return ArchiveIntakeResult{}, err
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].SnapshotID != jobs[j].SnapshotID {
			return jobs[i].SnapshotID < jobs[j].SnapshotID
		}
		return jobs[i].ID < jobs[j].ID
	})
	result := ArchiveIntakeResult{}
	for i := range jobs {
		if jobs[i].Status == ArchiveJobStatusArchived {
			continue
		}
		reused, err := archiveOneBlock(sourceRoot, archiveRoot, jobs[i].Path, jobs[i].Block)
		if err != nil {
			jobs[i].Status = ArchiveJobStatusFailed
			result.Failed++
			_ = saveArchiveJobsBySnapshot(store, snapshotID, jobs)
			return result, err
		}
		if reused {
			result.Reused++
		} else {
			result.Archived++
		}
		jobs[i].Status = ArchiveJobStatusArchived
	}
	if err := saveArchiveJobsBySnapshot(store, snapshotID, jobs); err != nil {
		return result, err
	}
	return result, nil
}

func saveArchiveJobsBySnapshot(store state.JSONStore, snapshotID string, jobs []state.ArchiveIntakeJob) error {
	if snapshotID != "" {
		return store.SaveArchiveIntakeJobs(snapshotID, jobs)
	}
	bySnapshot := map[string][]state.ArchiveIntakeJob{}
	for _, job := range jobs {
		bySnapshot[job.SnapshotID] = append(bySnapshot[job.SnapshotID], job)
	}
	for id, grouped := range bySnapshot {
		if err := store.SaveArchiveIntakeJobs(id, grouped); err != nil {
			return err
		}
	}
	return nil
}

func archiveOneBlock(sourceRoot string, archiveRoot string, rel string, b block.Block) (bool, error) {
	clean, err := cleanArchiveSourceRel(rel)
	if err != nil {
		return false, err
	}
	if b.Size < 0 || b.Offset < 0 || len(b.Hash) == 0 {
		return false, fmt.Errorf("archive block for %q has invalid metadata", rel)
	}
	targetPath, err := archiveBlockPath(archiveRoot, b)
	if err != nil {
		return false, err
	}
	if ok, err := verifyExistingArchiveBlock(targetPath, b); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	data, err := readAndVerifySourceBlockWithRetainedFallback(sourceRoot, clean, b)
	if err != nil {
		return false, err
	}
	return false, writeArchiveBlockAtomic(targetPath, data)
}

func RetainExistingBackupIntakeFile(sourceRoot string, rel string, retainedAt time.Time) (string, bool, error) {
	clean, err := cleanArchiveSourceRel(rel)
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(sourceRoot, filepath.FromSlash(clean))
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("backup intake source %q is a directory", rel)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	retainedPath, err := RetainBackupIntakeFile(sourceRoot, clean, data, retainedAt)
	if err != nil {
		return "", false, err
	}
	return retainedPath, true, nil
}

func RetainBackupIntakeFile(sourceRoot string, rel string, data []byte, retainedAt time.Time) (string, error) {
	clean, err := cleanArchiveSourceRel(rel)
	if err != nil {
		return "", err
	}
	if retainedAt.IsZero() {
		retainedAt = time.Now().UTC()
	}
	stamp := retainedAt.UTC().Format("20060102T150405.000000000Z")
	targetPath := filepath.Join(sourceRoot, ".sync", "backup-intake", stamp, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".*.backup-intake-staging")
	if err != nil {
		return "", err
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
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return "", err
	}
	cleanup = false
	return targetPath, nil
}

func PruneBackupIntakeFiles(opts BackupIntakePruneOptions) (BackupIntakePruneResult, error) {
	if opts.SourceRoot == "" {
		return BackupIntakePruneResult{}, fmt.Errorf("source root is required")
	}
	if opts.ArchiveRoot == "" {
		return BackupIntakePruneResult{}, fmt.Errorf("archive root is required")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	jobs, err := opts.Store.ListArchiveIntakeJobs("")
	if err != nil {
		return BackupIntakePruneResult{}, err
	}
	jobsByPath := map[string][]state.ArchiveIntakeJob{}
	for _, job := range jobs {
		clean, err := cleanArchiveSourceRel(job.Path)
		if err != nil {
			continue
		}
		jobsByPath[clean] = append(jobsByPath[clean], job)
	}
	root := filepath.Join(opts.SourceRoot, ".sync", "backup-intake")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return BackupIntakePruneResult{}, nil
	} else if err != nil {
		return BackupIntakePruneResult{}, err
	}
	result := BackupIntakePruneResult{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		result.ScannedFiles++
		relFromRoot, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relFromRoot, string(filepath.Separator))
		if len(parts) < 2 {
			result.KeptFiles++
			return nil
		}
		retainedAt, err := time.Parse("20060102T150405.000000000Z", parts[0])
		if err != nil || now.Sub(retainedAt) < opts.MinAge {
			result.KeptFiles++
			return nil
		}
		retainedRel := filepath.ToSlash(filepath.Join(parts[1:]...))
		if !backupIntakePathFullyProtected(opts.ArchiveRoot, jobsByPath[retainedRel]) {
			result.KeptFiles++
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		result.DeletedFiles++
		return nil
	})
	if err != nil {
		return result, err
	}
	_ = pruneEmptyBackupIntakeDirs(root)
	return result, nil
}

func backupIntakePathFullyProtected(archiveRoot string, jobs []state.ArchiveIntakeJob) bool {
	if len(jobs) == 0 {
		return false
	}
	for _, job := range jobs {
		if job.Status != ArchiveJobStatusArchived || !archiveJobBlockProtected(archiveRoot, job) {
			return false
		}
	}
	return true
}

func pruneEmptyBackupIntakeDirs(root string) error {
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func cleanArchiveSourceRel(rel string) (string, error) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return "", fmt.Errorf("archive source path %q is not a safe relative path", rel)
	}
	clean := pathpkg.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive source path %q is not a safe relative path", rel)
	}
	if clean == ".sync" || strings.HasPrefix(clean, ".sync/") {
		return "", fmt.Errorf("archive source path %q targets engine metadata", rel)
	}
	return clean, nil
}

func archiveBlockPath(root string, b block.Block) (string, error) {
	hexHash := hex.EncodeToString(b.Hash)
	if len(hexHash) < 2 {
		return "", fmt.Errorf("archive block hash is required")
	}
	return filepath.Join(root, "blocks", hexHash[:2], hexHash), nil
}

func verifyExistingArchiveBlock(path string, b block.Block) (bool, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != int64(b.Size) {
		return false, fmt.Errorf("archive block %q has size %d, expected %d", path, info.Size(), b.Size)
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return false, err
	}
	if !bytes.Equal(h.Sum(nil), b.Hash) {
		return false, fmt.Errorf("archive block %q hash mismatch", path)
	}
	return true, nil
}

func readAndVerifySourceBlockWithRetainedFallback(sourceRoot string, rel string, b block.Block) ([]byte, error) {
	livePath := filepath.Join(sourceRoot, filepath.FromSlash(rel))
	data, liveErr := readAndVerifySourceBlock(livePath, b)
	if liveErr == nil {
		return data, nil
	}
	retained, err := retainedBackupIntakeCandidates(sourceRoot, rel)
	if err != nil {
		return nil, liveErr
	}
	for _, candidate := range retained {
		data, err := readAndVerifySourceBlock(candidate, b)
		if err == nil {
			return data, nil
		}
	}
	return nil, liveErr
}

func retainedBackupIntakeCandidates(sourceRoot string, rel string) ([]string, error) {
	root := filepath.Join(sourceRoot, ".sync", "backup-intake")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var candidates []string
	targetSuffix := filepath.FromSlash(rel)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relFromRoot, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relFromRoot, string(filepath.Separator))
		if len(parts) < 2 {
			return nil
		}
		candidateRel := filepath.Join(parts[1:]...)
		if candidateRel == targetSuffix {
			candidates = append(candidates, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	return candidates, nil
}

func readAndVerifySourceBlock(path string, b block.Block) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buf := make([]byte, b.Size)
	if _, err := file.ReadAt(buf, b.Offset); err != nil {
		return nil, err
	}
	h := sha256.Sum256(buf)
	if !bytes.Equal(h[:], b.Hash) {
		return nil, fmt.Errorf("source block %q at offset %d failed archive hash verification", path, b.Offset)
	}
	return buf, nil
}

func writeArchiveBlockAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.archive-staging")
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
