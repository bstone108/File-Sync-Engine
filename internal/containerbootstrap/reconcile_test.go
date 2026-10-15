package containerbootstrap

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/config"
)

func TestReconcileWebGUIDisablesWhenEnvIsUnset(t *testing.T) {
	t.Setenv("FSE_WEB_GUI_ENABLED", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := config.Config{NodeName: "node-a", WebGUI: config.WebGUIConfig{Enabled: true, Listen: "0.0.0.0:8385"}}
	if err := writeConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	updated, err := ReconcileWebGUI(path, cfg, DefaultsFromEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if updated.WebGUI.Enabled {
		t.Fatal("unset GUI env must disable the web GUI")
	}
}

func TestReconcileWebGUISkipsWhenNoneCompatible(t *testing.T) {
	t.Setenv("FSE_WEB_GUI_ENABLED", "true")
	dir := t.TempDir()
	catalog := filepath.Join(dir, "schema-versions.json")
	body := `{"schema":"fse-gui-schema-catalog-v1","packages":[{"guiVersion":"9.0.0","minEngineSchema":9,"maxEngineSchema":9,"downloadURL":"https://example.test/nope.zip","checksumSHA256":"` + strings.Repeat("ab", 32) + `"}]}`
	if err := os.WriteFile(catalog, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FSE_WEB_GUI_SCHEMA_INDEX", catalog)
	path := filepath.Join(dir, "config.json")
	cfg := config.Config{NodeName: "node-a"}
	updated, err := ReconcileWebGUI(path, cfg, DefaultsFromEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if updated.WebGUI.Enabled || updated.WebGUI.PackagePath != "" {
		t.Fatalf("incompatible catalog must skip the GUI: %+v", updated.WebGUI)
	}
}

func TestReconcileWebGUISelectsNewestCompatiblePackage(t *testing.T) {
	t.Setenv("FSE_WEB_GUI_ENABLED", "true")
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "gui.zip")
	writeZip(t, zipPath)
	sum := shaOf(t, zipPath)
	payload, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "old") || strings.Contains(r.URL.Path, "future") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	catalog := `{
		"schema":"fse-gui-schema-catalog-v1",
		"packages":[
			{"guiVersion":"2026.10.01.01","minEngineSchema":1,"maxEngineSchema":1,"downloadURL":"` + server.URL + `/old.zip","checksumSHA256":"` + strings.Repeat("ab", 32) + `"},
			{"guiVersion":"2026.10.15.09","minEngineSchema":8,"maxEngineSchema":8,"downloadURL":"` + server.URL + `/future.zip","checksumSHA256":"` + strings.Repeat("cd", 32) + `"},
			{"guiVersion":"2026.10.15.02","minEngineSchema":1,"maxEngineSchema":1,"downloadURL":"` + server.URL + `/gui.zip","checksumSHA256":"` + sum + `"}
		]
	}`
	catalogPath := filepath.Join(dir, "schema-versions.json")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FSE_WEB_GUI_SCHEMA_INDEX", catalogPath)
	configPath := filepath.Join(dir, "config.json")
	updated, err := ReconcileWebGUI(configPath, config.Config{NodeName: "node-a"}, DefaultsFromEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if !updated.WebGUI.Enabled || updated.WebGUI.Version != "2026.10.15.02" || !strings.EqualFold(updated.WebGUI.ChecksumSHA256, sum) {
		t.Fatalf("resolver picked %+v", updated.WebGUI)
	}
	if _, err := os.Stat(updated.WebGUI.PackagePath); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	entry, err := zw.Create("schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(`{"guiVersion":"2026.10.15.02","minEngineSchema":1,"maxEngineSchema":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func shaOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
