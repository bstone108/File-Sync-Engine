package daemonapihandlers

import "filesyncengine/internal/api"

// StoreBackedRegistrar is the subset of the daemon API server whose handlers
// must be refreshed when the runtime metadata store is opened or hot-reloaded.
type StoreBackedRegistrar interface {
	SetMaintenanceScrubHandler(api.MaintenanceScrubHandler)
	SetBackupScrubHandler(api.BackupScrubHandler)
	SetBackupJobsHandler(api.BackupJobsHandler)
	SetSnapshotHandler(api.SnapshotHandler)
	SetRestorePlanHandler(api.RestorePlanHandler)
	SetRestoreHandler(api.RestoreHandler)
	SetSnapshotRetentionHandler(api.SnapshotRetentionHandler)
	SetMeshSettingsHandler(api.MeshSettingsHandler)
	SetMeshSettingsCommandHandler(api.MeshSettingsCommandHandler)
}

// StoreBackedHandlers groups API handlers that close over the currently active
// metadata store and therefore need to be installed together at startup and
// after a metadata-store reload.
type StoreBackedHandlers struct {
	MaintenanceScrub    api.MaintenanceScrubHandler
	BackupScrub         api.BackupScrubHandler
	BackupJobs          api.BackupJobsHandler
	Snapshot            api.SnapshotHandler
	RestorePlan         api.RestorePlanHandler
	Restore             api.RestoreHandler
	SnapshotRetention   api.SnapshotRetentionHandler
	MeshSettings        api.MeshSettingsHandler
	MeshSettingsCommand api.MeshSettingsCommandHandler
}

// RegisterStoreBacked installs the complete dynamic store-backed handler group.
// Nil handlers are allowed so focused tests and partial call sites can refresh a
// single handler without requiring unrelated no-op closures.
func RegisterStoreBacked(registrar StoreBackedRegistrar, handlers StoreBackedHandlers) {
	registrar.SetMaintenanceScrubHandler(handlers.MaintenanceScrub)
	registrar.SetBackupScrubHandler(handlers.BackupScrub)
	registrar.SetBackupJobsHandler(handlers.BackupJobs)
	registrar.SetSnapshotHandler(handlers.Snapshot)
	registrar.SetRestorePlanHandler(handlers.RestorePlan)
	registrar.SetRestoreHandler(handlers.Restore)
	registrar.SetSnapshotRetentionHandler(handlers.SnapshotRetention)
	registrar.SetMeshSettingsHandler(handlers.MeshSettings)
	registrar.SetMeshSettingsCommandHandler(handlers.MeshSettingsCommand)
}
