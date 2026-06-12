package apicontrol

import (
	"filesyncengine/internal/api"
	"filesyncengine/internal/state"
)

func HandleBackupJobs(store state.JSONStore, req api.BackupJobsRequest) (api.BackupJobsResponse, error) {
	restoreJobs, err := store.ListBackupRestoreJobs(req.SnapshotID)
	if err != nil {
		return api.BackupJobsResponse{}, err
	}
	retentionJobs, err := store.ListBackupRetentionJobs()
	if err != nil {
		return api.BackupJobsResponse{}, err
	}
	repairJobs, err := store.ListBackupRepairJobs()
	if err != nil {
		return api.BackupJobsResponse{}, err
	}
	return api.BackupJobsResponse{RestoreJobs: restoreJobs, RetentionJobs: retentionJobs, RepairJobs: repairJobs}, nil
}
