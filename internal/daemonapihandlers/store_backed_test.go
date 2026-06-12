package daemonapihandlers

import (
	"context"
	"testing"

	"filesyncengine/internal/api"
)

type recordingRegistrar struct {
	maintenanceScrub    api.MaintenanceScrubHandler
	backupScrub         api.BackupScrubHandler
	backupJobs          api.BackupJobsHandler
	snapshot            api.SnapshotHandler
	restorePlan         api.RestorePlanHandler
	restore             api.RestoreHandler
	snapshotRetention   api.SnapshotRetentionHandler
	meshSettings        api.MeshSettingsHandler
	meshSettingsCommand api.MeshSettingsCommandHandler
}

func (r *recordingRegistrar) SetMaintenanceScrubHandler(handler api.MaintenanceScrubHandler) {
	r.maintenanceScrub = handler
}
func (r *recordingRegistrar) SetBackupScrubHandler(handler api.BackupScrubHandler) {
	r.backupScrub = handler
}
func (r *recordingRegistrar) SetBackupJobsHandler(handler api.BackupJobsHandler) {
	r.backupJobs = handler
}
func (r *recordingRegistrar) SetSnapshotHandler(handler api.SnapshotHandler) { r.snapshot = handler }
func (r *recordingRegistrar) SetRestorePlanHandler(handler api.RestorePlanHandler) {
	r.restorePlan = handler
}
func (r *recordingRegistrar) SetRestoreHandler(handler api.RestoreHandler) { r.restore = handler }
func (r *recordingRegistrar) SetSnapshotRetentionHandler(handler api.SnapshotRetentionHandler) {
	r.snapshotRetention = handler
}
func (r *recordingRegistrar) SetMeshSettingsHandler(handler api.MeshSettingsHandler) {
	r.meshSettings = handler
}
func (r *recordingRegistrar) SetMeshSettingsCommandHandler(handler api.MeshSettingsCommandHandler) {
	r.meshSettingsCommand = handler
}

func TestRegisterStoreBackedHandlersInstallsAllDynamicStoreHandlers(t *testing.T) {
	registrar := &recordingRegistrar{}
	called := map[string]bool{}

	RegisterStoreBacked(registrar, StoreBackedHandlers{
		MaintenanceScrub: func(context.Context, api.MaintenanceScrubRequest) (api.MaintenanceScrubResponse, error) {
			called["maintenance"] = true
			return api.MaintenanceScrubResponse{}, nil
		},
		BackupScrub: func(context.Context, api.BackupScrubRequest) (api.BackupScrubResponse, error) {
			called["backupScrub"] = true
			return api.BackupScrubResponse{}, nil
		},
		BackupJobs: func(context.Context, api.BackupJobsRequest) (api.BackupJobsResponse, error) {
			called["backupJobs"] = true
			return api.BackupJobsResponse{}, nil
		},
		Snapshot: func(context.Context, api.SnapshotRequest) (api.SnapshotResponse, error) {
			called["snapshot"] = true
			return api.SnapshotResponse{}, nil
		},
		RestorePlan: func(context.Context, api.RestorePlanRequest) (api.RestorePlanResponse, error) {
			called["restorePlan"] = true
			return api.RestorePlanResponse{}, nil
		},
		Restore: func(context.Context, api.RestoreRequest) (api.RestoreResponse, error) {
			called["restore"] = true
			return api.RestoreResponse{}, nil
		},
		SnapshotRetention: func(context.Context, api.SnapshotRetentionRequest) (api.SnapshotRetentionResponse, error) {
			called["retention"] = true
			return api.SnapshotRetentionResponse{}, nil
		},
		MeshSettings: func(context.Context, api.MeshSettingsRequest) (api.MeshSettingsResponse, error) {
			called["mesh"] = true
			return api.MeshSettingsResponse{}, nil
		},
		MeshSettingsCommand: func(context.Context, api.MeshSettingsCommandRequest) (api.MeshSettingsCommandResponse, error) {
			called["meshCommand"] = true
			return api.MeshSettingsCommandResponse{}, nil
		},
	})

	invokeRegisteredHandlers(t, registrar)
	for _, name := range []string{"maintenance", "backupScrub", "backupJobs", "snapshot", "restorePlan", "restore", "retention", "mesh", "meshCommand"} {
		if !called[name] {
			t.Fatalf("handler %s was not registered/invoked", name)
		}
	}
}

func TestRegisterStoreBackedHandlersRefreshesHandlersAfterStoreReload(t *testing.T) {
	registrar := &recordingRegistrar{}
	selected := "old"
	RegisterStoreBacked(registrar, StoreBackedHandlers{
		BackupJobs: func(context.Context, api.BackupJobsRequest) (api.BackupJobsResponse, error) {
			selected = "old"
			return api.BackupJobsResponse{}, nil
		},
	})
	RegisterStoreBacked(registrar, StoreBackedHandlers{
		BackupJobs: func(context.Context, api.BackupJobsRequest) (api.BackupJobsResponse, error) {
			selected = "new"
			return api.BackupJobsResponse{}, nil
		},
	})

	if _, err := registrar.backupJobs(context.Background(), api.BackupJobsRequest{}); err != nil {
		t.Fatal(err)
	}
	if selected != "new" {
		t.Fatalf("expected refreshed handler to be active, got %q", selected)
	}
}

func invokeRegisteredHandlers(t *testing.T, registrar *recordingRegistrar) {
	t.Helper()
	ctx := context.Background()
	calls := []struct {
		name string
		fn   func() error
	}{
		{"maintenance", func() error { _, err := registrar.maintenanceScrub(ctx, api.MaintenanceScrubRequest{}); return err }},
		{"backupScrub", func() error { _, err := registrar.backupScrub(ctx, api.BackupScrubRequest{}); return err }},
		{"backupJobs", func() error { _, err := registrar.backupJobs(ctx, api.BackupJobsRequest{}); return err }},
		{"snapshot", func() error { _, err := registrar.snapshot(ctx, api.SnapshotRequest{}); return err }},
		{"restorePlan", func() error { _, err := registrar.restorePlan(ctx, api.RestorePlanRequest{}); return err }},
		{"restore", func() error { _, err := registrar.restore(ctx, api.RestoreRequest{}); return err }},
		{"retention", func() error { _, err := registrar.snapshotRetention(ctx, api.SnapshotRetentionRequest{}); return err }},
		{"mesh", func() error { _, err := registrar.meshSettings(ctx, api.MeshSettingsRequest{}); return err }},
		{"meshCommand", func() error {
			_, err := registrar.meshSettingsCommand(ctx, api.MeshSettingsCommandRequest{})
			return err
		}},
	}
	for _, call := range calls {
		if call.fn == nil {
			t.Fatalf("handler %s was not installed", call.name)
		}
		if err := call.fn(); err != nil {
			t.Fatalf("handler %s returned error: %v", call.name, err)
		}
	}
}
