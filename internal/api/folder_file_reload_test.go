package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
)

func TestFolderFileReadsFolderSavedBeforeHotReload(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	if _, _, err := config.EnsureFile(configPath); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	folderPath := filepath.Join(dir, "shared")
	if err := os.MkdirAll(folderPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderPath, "note.txt"), []byte("hello-from-disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Folders = append(cfg.Folders, config.FolderConfig{
		ID: "shared", Path: folderPath, Enabled: true, Mode: config.ModeSendReceive, BlockSize: config.DefaultBlockSize,
	})
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	server := NewServer(State{NodeName: cfg.NodeName, ConfigPath: configPath}, cfg.API.Key)
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	request, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/folder-file?folder=shared&path=note.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-FSE-API-Key", cfg.API.Key)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %s", response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello-from-disk\n" {
		t.Fatalf("body = %q", body)
	}
}
