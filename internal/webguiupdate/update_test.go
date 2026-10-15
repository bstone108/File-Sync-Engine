package webguiupdate

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/webgui"
)

func TestAutoUpdateDoesNotDownloadWhenGUIEnvIsUnsetOrOptedOut(t *testing.T) {
	hits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer server.Close()
	catalog := writeCatalog(t, t.TempDir(), server.URL+"/gui.zip", "2026.10.15.09", strings.Repeat("ab", 32), "")
	for _, tc := range []struct {
		gui, auto, outcome string
	}{
		{gui: "", auto: "", outcome: "disabled"},
		{gui: "false", auto: "true", outcome: "disabled"},
		{gui: "true", auto: "false", outcome: "opted-out"},
	} {
		result := Check(context.Background(), Options{
			GUIEnabledEnv: tc.gui, AutoUpdateEnv: tc.auto, CatalogSources: []string{catalog}, HTTPClient: server.Client(),
		})
		if result.Outcome != tc.outcome || result.Auto && tc.outcome != "opted-out" && tc.gui == "true" {
			t.Fatalf("%s/%s result = %+v", tc.gui, tc.auto, result)
		}
	}
	if hits != 0 {
		t.Fatalf("disabled checks downloaded %d times", hits)
	}
}

func TestCheckInstallsNewerCompatiblePackageAndKeepsPrevious(t *testing.T) {
	dir := t.TempDir()
	oldZip := filepath.Join(dir, "old.zip")
	writeGUIZip(t, oldZip, "2026.10.15.01", "<h1>old</h1>")
	oldSum := shaOf(t, oldZip)
	installDir := filepath.Join(dir, "web")
	if _, err := webgui.InstallLocalPackage(webgui.InstallOptions{PackagePath: oldZip, InstallDir: installDir, Version: "2026.10.15.01", ChecksumSHA256: oldSum, KeepPrevious: true}); err != nil {
		t.Fatal(err)
	}
	newZip := filepath.Join(dir, "new-src.zip")
	writeGUIZip(t, newZip, "2026.10.15.02", "<h1>new</h1>")
	payload, err := os.ReadFile(newZip)
	if err != nil {
		t.Fatal(err)
	}
	sum := shaOf(t, newZip)
	var futureHits int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "future") {
			futureHits++
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	catalogPath := filepath.Join(dir, "schema-versions.json")
	body := `{
		"schema":"fse-gui-schema-catalog-v1",
		"packages":[
			{"guiVersion":"2026.10.15.01","minEngineSchema":1,"maxEngineSchema":1,"downloadURL":"` + server.URL + `/old.zip","checksumSHA256":"` + oldSum + `"},
			{"guiVersion":"2026.10.16.01","minEngineSchema":9,"maxEngineSchema":9,"downloadURL":"` + server.URL + `/future.zip","checksumSHA256":"` + strings.Repeat("cd", 32) + `"},
			{"guiVersion":"2026.10.15.02","minEngineSchema":1,"maxEngineSchema":1,"downloadURL":"` + server.URL + `/gui.zip","checksumSHA256":"` + sum + `"}
		]
	}`
	if err := os.WriteFile(catalogPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	writeNodeConfig(t, configPath)
	result := Check(context.Background(), Options{
		GUIEnabledEnv: "true", CatalogSources: []string{catalogPath}, ConfigPath: configPath,
		InstallDir: installDir, PackageDir: filepath.Join(dir, "packages"),
		CurrentVersion: "2026.10.15.01", CurrentSum: oldSum, HTTPClient: server.Client(),
	})
	if result.Outcome != "updated" || result.Version != "2026.10.15.02" {
		t.Fatalf("result = %+v", result)
	}
	if futureHits != 0 {
		t.Fatalf("incompatible package was downloaded %d times", futureHits)
	}
	got, err := os.ReadFile(filepath.Join(installDir, "index.html"))
	if err != nil || string(got) != "<h1>new</h1>" {
		t.Fatalf("index = %q err=%v", got, err)
	}
	prev, err := os.ReadFile(filepath.Join(installDir+".previous", "index.html"))
	if err != nil || string(prev) != "<h1>old</h1>" {
		t.Fatalf("previous = %q err=%v", prev, err)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil || cfg.WebGUI.Version != "2026.10.15.02" || !cfg.WebGUI.Enabled {
		t.Fatalf("config = %+v err=%v", cfg.WebGUI, err)
	}
}

func TestCheckRollsBackWhenTheNewPackageDoesNotServe(t *testing.T) {
	dir := t.TempDir()
	oldZip := filepath.Join(dir, "old.zip")
	writeGUIZip(t, oldZip, "2026.10.15.01", "<h1>old</h1>")
	oldSum := shaOf(t, oldZip)
	installDir := filepath.Join(dir, "web")
	if _, err := webgui.InstallLocalPackage(webgui.InstallOptions{PackagePath: oldZip, InstallDir: installDir, Version: "2026.10.15.01", ChecksumSHA256: oldSum}); err != nil {
		t.Fatal(err)
	}
	newZip := filepath.Join(dir, "new-src.zip")
	writeGUIZip(t, newZip, "2026.10.15.02", "<h1>new</h1>")
	sum := shaOf(t, newZip)
	payload, err := os.ReadFile(newZip)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	catalog := writeCatalog(t, dir, server.URL+"/gui.zip", "2026.10.15.02", sum, "")
	result := Check(context.Background(), Options{
		GUIEnabledEnv: "true", CatalogSources: []string{catalog}, InstallDir: installDir,
		PackageDir: filepath.Join(dir, "packages"), CurrentVersion: "2026.10.15.01", CurrentSum: oldSum,
		HTTPClient: server.Client(), Probe: func(string) error { return os.ErrInvalid },
	})
	if result.Outcome != "rolled-back" || result.Version != "2026.10.15.01" {
		t.Fatalf("result = %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(installDir, "index.html"))
	if err != nil || string(got) != "<h1>old</h1>" {
		t.Fatalf("restored index = %q err=%v", got, err)
	}
}

func TestCheckRequiresSignatureWhenPublicKeyIsSet(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	newZip := filepath.Join(dir, "new-src.zip")
	writeGUIZip(t, newZip, "2026.10.15.02", "<h1>signed</h1>")
	payload, err := os.ReadFile(newZip)
	if err != nil {
		t.Fatal(err)
	}
	sum := shaOf(t, newZip)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	installDir := filepath.Join(dir, "web")
	badCatalog := writeCatalog(t, dir, server.URL+"/gui.zip", "2026.10.15.02", sum, "")
	bad := Check(context.Background(), Options{
		GUIEnabledEnv: "true", CatalogSources: []string{badCatalog}, InstallDir: installDir,
		PackageDir: filepath.Join(dir, "packages"), PublicKey: base64.StdEncoding.EncodeToString(pub),
		HTTPClient: server.Client(),
	})
	if bad.Outcome != "failed" {
		t.Fatalf("unsigned package outcome = %s", bad.Outcome)
	}
	if _, err := os.Stat(filepath.Join(installDir, "index.html")); !os.IsNotExist(err) {
		t.Fatalf("unsigned package was installed: %v", err)
	}
	signedCatalog := writeCatalog(t, dir, server.URL+"/gui.zip", "2026.10.15.02", sum, sig)
	good := Check(context.Background(), Options{
		GUIEnabledEnv: "true", CatalogSources: []string{signedCatalog}, InstallDir: installDir,
		PackageDir: filepath.Join(dir, "packages-ok"), PublicKey: base64.StdEncoding.EncodeToString(pub),
		HTTPClient: server.Client(),
	})
	if good.Outcome != "updated" || good.Version != "2026.10.15.02" {
		t.Fatalf("signed result = %+v", good)
	}
}

func TestParseIntervalDefaultsShortAndInvalidValues(t *testing.T) {
	if ParseInterval("") != 6*time.Hour || ParseInterval("nope") != 6*time.Hour || ParseInterval("1s") != 6*time.Hour {
		t.Fatal("short or invalid intervals must use the six hour default")
	}
	if ParseInterval("1h") != time.Hour {
		t.Fatal("one hour is an accepted interval")
	}
}

func writeCatalog(t *testing.T, dir, downloadURL, version, sum, signature string) string {
	t.Helper()
	pkg := map[string]any{
		"guiVersion": version, "minEngineSchema": 1, "maxEngineSchema": 1,
		"downloadURL": downloadURL, "checksumSHA256": sum,
	}
	if signature != "" {
		pkg["signature"] = signature
	}
	raw, err := json.Marshal(map[string]any{"schema": "fse-gui-schema-catalog-v1", "packages": []any{pkg}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "schema-"+version+".json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeGUIZip(t *testing.T, path, version, index string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	schema, err := zw.Create("schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Write([]byte(`{"guiVersion":"` + version + `","minEngineSchema":1,"maxEngineSchema":1}`)); err != nil {
		t.Fatal(err)
	}
	page, err := zw.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Write([]byte(index)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeNodeConfig(t *testing.T, path string) {
	t.Helper()
	raw, err := json.MarshalIndent(config.Config{NodeName: "node-a"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFileAtomic(path, append(raw, '\n'), 0o600); err != nil {
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
