package metadatastore

import (
	"errors"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestResolveStorePathsHonorsConfigRelativeSingleAndPerFolderPaths(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")

	jsonCfg := config.Config{}
	if got := ConfiguredStorePath(jsonCfg, configPath); got != configPath+".state.json" {
		t.Fatalf("default JSON path = %q", got)
	}

	badgerCfg := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: "metadata/main.badger"}}
	if got, want := ConfiguredStorePath(badgerCfg, configPath), filepath.Join(filepath.Dir(configPath), "metadata/main.badger"); got != want {
		t.Fatalf("relative badger path = %q, want %q", got, want)
	}

	perFolderCfg := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, PerFolder: true, Path: "metadata/by-folder"}}
	if got, want := ConfiguredFolderStorePath(perFolderCfg, configPath, "docs/photos 2026"), filepath.Join(filepath.Dir(configPath), "metadata/by-folder", "docs_photos_2026.badger"); got != want {
		t.Fatalf("per-folder path = %q, want %q", got, want)
	}
}

func TestOpenRejectsPerFolderNonBadgerBackend(t *testing.T) {
	cfg := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendJSON, PerFolder: true}}

	store, path, err := Open(cfg, filepath.Join(t.TempDir(), "config.jsonc"))
	if err == nil {
		store.Close()
		t.Fatalf("Open unexpectedly succeeded at %q", path)
	}
	if err.Error() != "metadata.perFolder requires metadata.backend to be badger" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReloadIdentityChangesOnlyWhenBackendOrResolvedPathChanges(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")
	current := Identity{Backend: config.MetadataBackendBadger, Path: filepath.Join(filepath.Dir(configPath), "metadata/main.badger")}

	same := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: "metadata/main.badger"}}
	if ReloadNeeded(current, same, configPath) {
		t.Fatal("ReloadNeeded returned true for unchanged backend/path")
	}

	jsonBackend := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendJSON, Path: "metadata/main.badger"}}
	if !ReloadNeeded(current, jsonBackend, configPath) {
		t.Fatal("ReloadNeeded returned false after backend changed")
	}

	moved := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: "metadata/other.badger"}}
	if !ReloadNeeded(current, moved, configPath) {
		t.Fatal("ReloadNeeded returned false after resolved path changed")
	}
}

func TestReloadStoreWhenIdentityChangesOpensNewStoreAndReportsNewIdentity(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")
	currentCfg := config.Config{}
	current := CurrentIdentity(currentCfg, configPath)
	currentStore := state.NewJSONStore(filepath.Join(t.TempDir(), "current.json"))
	opened := 0
	nextCfg := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendJSON, Path: "metadata/next.json"}}
	nextStore := state.NewJSONStore(filepath.Join(t.TempDir(), "next.json"))

	gotStore, gotPath, gotIdentity, reloaded, err := ReloadStore(current, currentStore, nextCfg, configPath, func(cfg config.Config, path string) (state.JSONStore, string, error) {
		opened++
		if path != configPath {
			t.Fatalf("open config path = %q, want %q", path, configPath)
		}
		return nextStore, ConfiguredStorePath(cfg, path), nil
	})
	defer gotStore.Close()

	if err != nil {
		t.Fatalf("ReloadStore returned error: %v", err)
	}
	if !reloaded {
		t.Fatal("ReloadStore did not report reload after identity change")
	}
	if opened != 1 {
		t.Fatalf("open calls = %d, want 1", opened)
	}
	if gotPath != ConfiguredStorePath(nextCfg, configPath) {
		t.Fatalf("path = %q, want %q", gotPath, ConfiguredStorePath(nextCfg, configPath))
	}
	if gotIdentity != CurrentIdentity(nextCfg, configPath) {
		t.Fatalf("identity = %#v, want %#v", gotIdentity, CurrentIdentity(nextCfg, configPath))
	}
	if gotStore != nextStore {
		t.Fatal("ReloadStore did not return newly opened store")
	}
}

func TestReloadStorePreservesCurrentStoreWhenIdentityUnchangedOrOpenFails(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")
	currentCfg := config.Config{}
	current := CurrentIdentity(currentCfg, configPath)
	currentStore := state.NewJSONStore(filepath.Join(t.TempDir(), "current.json"))
	defer currentStore.Close()

	gotStore, gotPath, gotIdentity, reloaded, err := ReloadStore(current, currentStore, currentCfg, configPath, func(config.Config, string) (state.JSONStore, string, error) {
		t.Fatal("open should not be called for unchanged identity")
		return state.JSONStore{}, "", nil
	})
	if err != nil || reloaded || gotStore != currentStore || gotPath != "" || gotIdentity != current {
		t.Fatalf("unchanged reload = store:%v path:%q identity:%#v reloaded:%v err:%v", gotStore, gotPath, gotIdentity, reloaded, err)
	}

	nextCfg := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendJSON, Path: "metadata/next.json"}}
	openErr := errors.New("badger unavailable")
	gotStore, gotPath, gotIdentity, reloaded, err = ReloadStore(current, currentStore, nextCfg, configPath, func(config.Config, string) (state.JSONStore, string, error) {
		return state.JSONStore{}, "", openErr
	})
	if !errors.Is(err, openErr) {
		t.Fatalf("open error = %v, want %v", err, openErr)
	}
	if reloaded || gotStore != currentStore || gotPath != "" || gotIdentity != current {
		t.Fatalf("failed reload did not preserve current store/identity: store:%v path:%q identity:%#v reloaded:%v", gotStore, gotPath, gotIdentity, reloaded)
	}
}

func TestCompactionStatePathUsesConfiguredMetadataStorePath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")
	cfg := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: "metadata/main.badger"}}

	got := CompactionStatePath(configPath, func(path string) (config.Config, error) {
		if path != configPath {
			t.Fatalf("loader path = %q, want %q", path, configPath)
		}
		return cfg, nil
	})

	want := filepath.Join(filepath.Dir(configPath), "metadata/main.badger")
	if got != want {
		t.Fatalf("CompactionStatePath = %q, want %q", got, want)
	}
}

func TestCompactionStatePathFallsBackToDefaultStatePathWhenConfigCannotLoad(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")

	got := CompactionStatePath(configPath, func(string) (config.Config, error) {
		return config.Config{}, errors.New("partial config")
	})

	if want := DefaultStatePath(configPath); got != want {
		t.Fatalf("CompactionStatePath fallback = %q, want %q", got, want)
	}
}
