package daemonreload

import (
	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/daemonmonitor"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/monitor"
	"filesyncengine/internal/state"
)

// RuntimeState is the narrow daemon state mutated when a debounced config reload
// is adopted. Process-boundary effects stay injected by cmd/fse.
type RuntimeState struct {
	Config            config.Config
	Version           uint64
	MetadataStore     state.JSONStore
	MetadataStorePath string
	MetadataIdentity  metadatastore.Identity
	Monitor           daemonmonitor.Closable
	MonitorFolders    []monitor.Folder
}

type StoreReloader func(current metadatastore.Identity, currentStore state.JSONStore, next config.Config, configPath string) (state.JSONStore, string, metadatastore.Identity, bool, error)
type MonitorRebuilder func(current daemonmonitor.Closable, active []monitor.Folder, cfg config.Config) (daemonmonitor.Closable, bool, error)
type StateProjector func(cfg config.Config, configPath string, version uint64, status string, store state.JSONStore) api.State

type Options struct {
	ConfigPath            string
	ConfigureLogging      func(config.Config, string) error
	ReloadStore           StoreReloader
	RebuildMonitor        MonitorRebuilder
	StateForConfig        StateProjector
	UpdateState           func(api.State)
	RegisterStoreHandlers func(config.Config, string, state.JSONStore, string)
	Publish               func(api.Event)
}

type Result struct {
	LoggingError        error
	MetadataReloaded    bool
	MetadataReloadError error
	MonitorRebuilt      bool
	MonitorRebuildError error
}

func Apply(next config.Config, runtime *RuntimeState, opts Options) Result {
	result := Result{}
	if opts.ConfigureLogging != nil {
		if err := opts.ConfigureLogging(next, opts.ConfigPath); err != nil {
			result.LoggingError = err
			return result
		}
	}
	runtime.Config = next
	if opts.ReloadStore != nil {
		nextStore, openedPath, nextIdentity, reloaded, err := opts.ReloadStore(runtime.MetadataIdentity, runtime.MetadataStore, next, opts.ConfigPath)
		if err != nil {
			result.MetadataReloadError = err
		} else if reloaded {
			runtime.MetadataStore = nextStore
			runtime.MetadataStorePath = openedPath
			runtime.MetadataIdentity = nextIdentity
			result.MetadataReloaded = true
		}
	}
	if opts.RebuildMonitor != nil {
		nextMonitor, rebuilt, err := opts.RebuildMonitor(runtime.Monitor, runtime.MonitorFolders, next)
		if err != nil {
			result.MonitorRebuildError = err
		} else if rebuilt {
			runtime.Monitor = nextMonitor
			runtime.MonitorFolders = foldersFromConfig(next)
			result.MonitorRebuilt = true
			publish(opts.Publish, api.Event{Type: "monitor.rebuilt", Message: "folder monitor set rebuilt after config reload"})
		}
	}
	runtime.Version++
	if opts.StateForConfig != nil && opts.UpdateState != nil {
		opts.UpdateState(opts.StateForConfig(next, opts.ConfigPath, runtime.Version, "running", runtime.MetadataStore))
	}
	if opts.RegisterStoreHandlers != nil {
		opts.RegisterStoreHandlers(next, opts.ConfigPath, runtime.MetadataStore, runtime.MetadataStorePath)
	}
	publish(opts.Publish, api.Event{Type: "config.reloaded", Message: "configuration adopted after quiet period"})
	return result
}

func foldersFromConfig(cfg config.Config) []monitor.Folder {
	folders := make([]monitor.Folder, 0, len(cfg.Folders))
	for _, folder := range cfg.Folders {
		folders = append(folders, monitor.Folder{ID: folder.ID, Path: folder.Path})
	}
	return folders
}

func publish(fn func(api.Event), event api.Event) {
	if fn != nil {
		fn(event)
	}
}
