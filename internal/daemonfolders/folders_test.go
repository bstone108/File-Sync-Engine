package daemonfolders

import (
	"testing"

	"filesyncengine/internal/config"
)

func TestSyncFoldersProjectsConfiguredFolderFields(t *testing.T) {
	permissions := config.PermissionPolicy{Mode: config.PermissionFixed, FileMode: "0640", DirMode: "0750"}
	cfg := config.Config{Folders: []config.FolderConfig{{
		ID:          "photos",
		Path:        "/data/photos",
		SyncGroup:   "media",
		Mode:        config.ModeSendOnly,
		BlockSize:   131072,
		Ignore:      []string{"*.tmp", "cache/"},
		Permissions: permissions,
	}}}

	folders := SyncFolders(cfg)
	if len(folders) != 1 {
		t.Fatalf("expected one sync folder, got %d", len(folders))
	}
	folder := folders[0]
	if folder.ID != "photos" || folder.Path != "/data/photos" || folder.SyncGroup != "media" {
		t.Fatalf("folder identity mismatch: %+v", folder)
	}
	if folder.Mode != config.ModeSendOnly || folder.BlockSize != 131072 {
		t.Fatalf("folder sync settings mismatch: %+v", folder)
	}
	if len(folder.IgnoreSuffixes) != 2 || folder.IgnoreSuffixes[0] != "*.tmp" || folder.IgnoreSuffixes[1] != "cache/" {
		t.Fatalf("ignore suffixes not preserved: %#v", folder.IgnoreSuffixes)
	}
	if folder.Permissions != permissions {
		t.Fatalf("permissions not preserved: %+v", folder.Permissions)
	}
}
