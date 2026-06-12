package daemonreload

import (
	"errors"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/daemonmonitor"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/monitor"
	"filesyncengine/internal/state"
)

type fakeMonitor struct{ closed bool }

func (f *fakeMonitor) Close() error {
	f.closed = true
	return nil
}

func TestApplyConfigReloadAdoptsNewRuntimeStateAndRefreshesHandlers(t *testing.T) {
	oldStore := state.NewJSONStore(t.TempDir() + "/old.json")
	newStore := state.NewJSONStore(t.TempDir() + "/new.json")
	oldMonitor := &fakeMonitor{}
	newMonitor := &fakeMonitor{}
	runtime := RuntimeState{
		Config:            config.Config{NodeName: "old", Folders: []config.FolderConfig{{ID: "old-folder", Path: "/old"}}},
		Version:           3,
		MetadataStore:     oldStore,
		MetadataStorePath: "/state/old",
		MetadataIdentity:  metadatastore.Identity{Backend: config.MetadataBackendJSON, Path: "/state/old"},
		Monitor:           oldMonitor,
		MonitorFolders:    []monitor.Folder{{ID: "old-folder", Path: "/old"}},
	}
	nextConfig := config.Config{NodeName: "new", Folders: []config.FolderConfig{{ID: "new-folder", Path: "/new"}}}

	var loggingConfigured bool
	var stateUpdated api.State
	var registeredPath string
	var published []api.Event
	result := Apply(nextConfig, &runtime, Options{
		ConfigPath: "/config/fse.jsonc",
		ConfigureLogging: func(cfg config.Config, path string) error {
			if cfg.NodeName != "new" || path != "/config/fse.jsonc" {
				t.Fatalf("logging configured with cfg=%q path=%q", cfg.NodeName, path)
			}
			loggingConfigured = true
			return nil
		},
		ReloadStore: func(current metadatastore.Identity, currentStore state.JSONStore, next config.Config, configPath string) (state.JSONStore, string, metadatastore.Identity, bool, error) {
			if current.Path != "/state/old" || next.NodeName != "new" || configPath != "/config/fse.jsonc" {
				t.Fatalf("metadata reload called with current=%+v next=%q path=%q", current, next.NodeName, configPath)
			}
			return newStore, "/state/new", metadatastore.Identity{Backend: config.MetadataBackendBadger, Path: "/state/new"}, true, nil
		},
		RebuildMonitor: func(current daemonmonitor.Closable, active []monitor.Folder, cfg config.Config) (daemonmonitor.Closable, bool, error) {
			if current != oldMonitor || len(active) != 1 || active[0].ID != "old-folder" || cfg.NodeName != "new" {
				t.Fatalf("monitor rebuild called with current=%v active=%+v cfg=%q", current, active, cfg.NodeName)
			}
			return newMonitor, true, nil
		},
		StateForConfig: func(cfg config.Config, configPath string, version uint64, status string, store state.JSONStore) api.State {
			if cfg.NodeName != "new" || version != 4 || status != "running" || configPath != "/config/fse.jsonc" {
				t.Fatalf("state projection got cfg=%q version=%d status=%q path=%q", cfg.NodeName, version, status, configPath)
			}
			return api.State{Status: status, ConfigVersion: version}
		},
		UpdateState: func(st api.State) { stateUpdated = st },
		RegisterStoreHandlers: func(cfg config.Config, configPath string, store state.JSONStore, storePath string) {
			if cfg.NodeName != "new" || configPath != "/config/fse.jsonc" {
				t.Fatalf("handler registration got cfg=%q configPath=%q", cfg.NodeName, configPath)
			}
			registeredPath = storePath
		},
		Publish: func(event api.Event) { published = append(published, event) },
	})

	if !loggingConfigured {
		t.Fatalf("expected logging to be configured")
	}
	if result.MetadataReloadError != nil || !result.MetadataReloaded || !result.MonitorRebuilt {
		t.Fatalf("unexpected result: %+v", result)
	}
	if runtime.Config.NodeName != "new" || runtime.Version != 4 || runtime.MetadataStorePath != "/state/new" || runtime.MetadataIdentity.Path != "/state/new" || runtime.Monitor != newMonitor {
		t.Fatalf("runtime state was not adopted: %+v", runtime)
	}
	if len(runtime.MonitorFolders) != 1 || runtime.MonitorFolders[0].ID != "new-folder" {
		t.Fatalf("monitor folders not refreshed: %+v", runtime.MonitorFolders)
	}
	if registeredPath != "/state/new" {
		t.Fatalf("store handlers registered with path %q", registeredPath)
	}
	if stateUpdated.ConfigVersion != 4 || stateUpdated.Status != "running" {
		t.Fatalf("state not updated: %+v", stateUpdated)
	}
	if len(published) != 2 || published[0].Type != "monitor.rebuilt" || published[1].Type != "config.reloaded" {
		t.Fatalf("unexpected published events: %+v", published)
	}
}

func TestApplyConfigReloadPreservesCurrentMetadataStoreOnReloadFailure(t *testing.T) {
	oldStore := state.NewJSONStore(t.TempDir() + "/old.json")
	runtime := RuntimeState{
		Config:            config.Config{NodeName: "old"},
		Version:           1,
		MetadataStore:     oldStore,
		MetadataStorePath: "/state/old",
		MetadataIdentity:  metadatastore.Identity{Backend: config.MetadataBackendJSON, Path: "/state/old"},
	}
	reloadErr := errors.New("cannot open new store")
	var registeredStorePath string

	result := Apply(config.Config{NodeName: "new"}, &runtime, Options{
		ConfigPath:       "/config/fse.jsonc",
		ConfigureLogging: func(config.Config, string) error { return nil },
		ReloadStore: func(metadatastore.Identity, state.JSONStore, config.Config, string) (state.JSONStore, string, metadatastore.Identity, bool, error) {
			return oldStore, "", runtime.MetadataIdentity, false, reloadErr
		},
		RebuildMonitor: func(current daemonmonitor.Closable, active []monitor.Folder, cfg config.Config) (daemonmonitor.Closable, bool, error) {
			return current, false, nil
		},
		StateForConfig: func(cfg config.Config, configPath string, version uint64, status string, store state.JSONStore) api.State {
			return api.State{ConfigVersion: version, Status: status}
		},
		UpdateState: func(api.State) {},
		RegisterStoreHandlers: func(cfg config.Config, configPath string, store state.JSONStore, storePath string) {
			registeredStorePath = storePath
		},
		Publish: func(api.Event) {},
	})

	if !errors.Is(result.MetadataReloadError, reloadErr) {
		t.Fatalf("expected metadata reload error, got %+v", result)
	}
	if runtime.MetadataStorePath != "/state/old" || runtime.MetadataIdentity.Path != "/state/old" {
		t.Fatalf("metadata store was not preserved: %+v", runtime)
	}
	if runtime.Config.NodeName != "new" || runtime.Version != 2 {
		t.Fatalf("non-metadata config adoption should continue: %+v", runtime)
	}
	if registeredStorePath != "/state/old" {
		t.Fatalf("handlers should keep preserved store path, got %q", registeredStorePath)
	}
}
