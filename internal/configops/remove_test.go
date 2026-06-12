package configops

import (
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestRemovePeerAndFolderPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fse.jsonc")
	if _, _, err := config.EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if err := AddPeer(path, "peer-remove", "pipe:stdio"); err != nil {
		t.Fatal(err)
	}
	if err := AddFolder(path, "remove-me", "/tmp/remove", "recvonly"); err != nil {
		t.Fatal(err)
	}
	if err := RemovePeer(path, "peer-remove"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	if err := RemoveFolder(path, "remove-me"); err != nil {
		t.Fatalf("RemoveFolder: %v", err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range cfg.Peers {
		if peer.ID == "peer-remove" {
			t.Fatalf("peer still present")
		}
	}
	for _, folder := range cfg.Folders {
		if folder.ID == "remove-me" {
			t.Fatalf("folder still present")
		}
	}
}
