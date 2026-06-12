package snapshotcontrol

import (
	"time"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

// ConfigLoader loads the active daemon configuration for configured snapshot operations.
type ConfigLoader func(string) (config.Config, error)

// StoreOpener opens the metadata store selected by the active configuration.
type StoreOpener func(config.Config, string) (state.JSONStore, string, error)

// ConfiguredOptions contains the process-boundary dependencies for snapshot operations
// that must load config and open the configured metadata backend before delegating to
// the snapshot domain helpers.
type ConfiguredOptions struct {
	ConfigPath string
	FolderID   string

	LoadConfig ConfigLoader
	OpenStore  StoreOpener
}

type RestorePlanner func(config.Config, state.JSONStore, string, string, []string, string, string) (backup.RestorePlan, error)
type RestoreExecutor func(config.Config, state.JSONStore, string, string, []string, string, string) (backup.RestoreResult, error)
type RetentionExecutor func(backup.SnapshotRetentionOptions) (backup.SnapshotRetentionPlan, error)

type RestoreConfiguredOptions struct {
	ConfigPath      string
	SnapshotID      string
	Paths           []string
	DestinationRoot string
	AlternatePath   string

	LoadConfig     ConfigLoader
	OpenStore      StoreOpener
	PlanRestore    RestorePlanner
	ExecuteRestore RestoreExecutor
}

type MarkerConfiguredOptions struct {
	ConfigPath string
	Action     cli.Action
	ID         string
	Mode       string

	LoadConfig ConfigLoader
	OpenStore  StoreOpener
	Now        func() time.Time
}

type RetentionConfiguredOptions struct {
	ConfigPath string
	KeepLast   int

	LoadConfig       ConfigLoader
	OpenStore        StoreOpener
	ExecuteRetention RetentionExecutor
}

// ListConfigured loads the active config, opens its selected metadata store, and
// lists snapshot markers, optionally scoped to one folder ID.
func ListConfigured(opts ConfiguredOptions) ([]state.SnapshotMarker, error) {
	cfg, err := opts.LoadConfig(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	store, _, err := opts.OpenStore(cfg, opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListSnapshotMarkers(opts.FolderID)
}

func MarkerConfigured(opts MarkerConfiguredOptions) (state.SnapshotMarker, error) {
	cfg, store, err := loadConfiguredSnapshotStore(opts.ConfigPath, opts.LoadConfig, opts.OpenStore)
	if err != nil {
		return state.SnapshotMarker{}, err
	}
	defer store.Close()
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return RunMarkerAction(opts.Action, cfg, store, opts.ID, opts.Mode, opts.ConfigPath, now().UTC())
}

func PlanRestoreConfigured(opts RestoreConfiguredOptions) (backup.RestorePlan, error) {
	cfg, store, err := loadConfiguredSnapshotStore(opts.ConfigPath, opts.LoadConfig, opts.OpenStore)
	if err != nil {
		return backup.RestorePlan{}, err
	}
	defer store.Close()
	planner := opts.PlanRestore
	if planner == nil {
		planner = PlanRestore
	}
	return planner(cfg, store, opts.ConfigPath, opts.SnapshotID, opts.Paths, opts.DestinationRoot, opts.AlternatePath)
}

func ExecuteRestoreConfigured(opts RestoreConfiguredOptions) (backup.RestoreResult, error) {
	cfg, store, err := loadConfiguredSnapshotStore(opts.ConfigPath, opts.LoadConfig, opts.OpenStore)
	if err != nil {
		return backup.RestoreResult{}, err
	}
	defer store.Close()
	executor := opts.ExecuteRestore
	if executor == nil {
		executor = ExecuteRestore
	}
	return executor(cfg, store, opts.ConfigPath, opts.SnapshotID, opts.Paths, opts.DestinationRoot, opts.AlternatePath)
}

func RetentionConfigured(opts RetentionConfiguredOptions) (backup.SnapshotRetentionPlan, error) {
	cfg, store, err := loadConfiguredSnapshotStore(opts.ConfigPath, opts.LoadConfig, opts.OpenStore)
	if err != nil {
		return backup.SnapshotRetentionPlan{}, err
	}
	defer store.Close()
	executor := opts.ExecuteRetention
	if executor == nil {
		executor = backup.ExecuteSnapshotRetention
	}
	return executor(backup.SnapshotRetentionOptions{Store: store, KeepLast: opts.KeepLast, ArchiveRoot: ArchivePath(cfg, opts.ConfigPath)})
}

func loadConfiguredSnapshotStore(configPath string, load ConfigLoader, open StoreOpener) (config.Config, state.JSONStore, error) {
	cfg, err := load(configPath)
	if err != nil {
		return config.Config{}, state.JSONStore{}, err
	}
	store, _, err := open(cfg, configPath)
	if err != nil {
		return config.Config{}, state.JSONStore{}, err
	}
	return cfg, store, nil
}
