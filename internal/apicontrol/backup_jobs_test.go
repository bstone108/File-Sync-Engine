package apicontrol

import (
	"path/filepath"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/state"
)

func TestHandleBackupJobsListsDurableOperationJobs(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveBackupRestoreJob(state.BackupRestoreJob{ID: "restore-a", SnapshotID: "snap-a", Status: "running"}); err != nil {
		t.Fatalf("save restore job: %v", err)
	}
	if err := store.SaveBackupRestoreJob(state.BackupRestoreJob{ID: "restore-b", SnapshotID: "snap-b", Status: "done"}); err != nil {
		t.Fatalf("save restore job: %v", err)
	}
	if err := store.SaveBackupRetentionJob(state.BackupRetentionJob{ID: "retention-a", Status: "running"}); err != nil {
		t.Fatalf("save retention job: %v", err)
	}
	if err := store.SaveBackupRepairJob(state.BackupRepairJob{ID: "repair-a", Status: "pending"}); err != nil {
		t.Fatalf("save repair job: %v", err)
	}

	resp, err := HandleBackupJobs(store, api.BackupJobsRequest{SnapshotID: "snap-a"})
	if err != nil {
		t.Fatalf("HandleBackupJobs: %v", err)
	}
	if len(resp.RestoreJobs) != 1 || resp.RestoreJobs[0].ID != "restore-a" {
		t.Fatalf("restore jobs not filtered by snapshot: %+v", resp.RestoreJobs)
	}
	if len(resp.RetentionJobs) != 1 || resp.RetentionJobs[0].ID != "retention-a" {
		t.Fatalf("retention jobs missing: %+v", resp.RetentionJobs)
	}
	if len(resp.RepairJobs) != 1 || resp.RepairJobs[0].ID != "repair-a" {
		t.Fatalf("repair jobs missing: %+v", resp.RepairJobs)
	}
}
