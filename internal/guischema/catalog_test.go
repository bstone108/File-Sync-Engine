package guischema

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/schemaversion"
)

func TestSelectNewestCompatiblePicksHighestAndRejectsIncompatible(t *testing.T) {
	packages := []Package{
		{GUIVersion: "2026.10.01.01", MinEngineSchema: 1, MaxEngineSchema: 1, DownloadURL: "https://example.test/old.zip", ChecksumSHA256: stringsRepeat(64)},
		{GUIVersion: "2026.10.15.02", MinEngineSchema: 2, MaxEngineSchema: 2, DownloadURL: "https://example.test/newer-incompatible.zip", ChecksumSHA256: stringsRepeat(64)},
		{GUIVersion: "2026.10.15.01", MinEngineSchema: 1, MaxEngineSchema: 1, DownloadURL: "https://example.test/current.zip", ChecksumSHA256: stringsRepeat(64)},
		{GUIVersion: "0.0.0-dev", MinEngineSchema: 1, MaxEngineSchema: 1, DownloadURL: "https://example.test/dev.zip", ChecksumSHA256: stringsRepeat(64)},
	}
	got, err := SelectNewestCompatible(packages, schemaversion.Engine)
	if err != nil {
		t.Fatal(err)
	}
	if got.GUIVersion != "2026.10.15.01" {
		t.Fatalf("newest compatible = %s, want 2026.10.15.01 (newer schema 2 package must be rejected)", got.GUIVersion)
	}
}

func TestSelectNewestCompatibleNoneAvailable(t *testing.T) {
	packages := []Package{
		{GUIVersion: "9.0.0", MinEngineSchema: 9, MaxEngineSchema: 9, DownloadURL: "https://example.test/future.zip", ChecksumSHA256: stringsRepeat(64)},
	}
	_, err := SelectNewestCompatible(packages, schemaversion.Engine)
	if !errors.Is(err, ErrNoneCompatible) {
		t.Fatalf("err = %v, want ErrNoneCompatible", err)
	}
}

func TestResolveNewestCompatibleVerifiesChecksumAndPackageSchema(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "gui.zip")
	writeSchemaZip(t, zipPath, `{"guiVersion":"2026.10.15.01","minEngineSchema":1,"maxEngineSchema":1}`)
	sum := fileSHA256(t, zipPath)
	body, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gui.zip" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	catalogPath := filepath.Join(dir, "schema-versions.json")
	catalog := []byte(`{"schema":"fse-gui-schema-catalog-v1","packages":[
		{"guiVersion":"2026.10.15.02","minEngineSchema":4,"maxEngineSchema":4,"downloadURL":"` + server.URL + `/nope.zip","checksumSHA256":"` + stringsRepeat(64) + `"},
		{"guiVersion":"2026.10.15.01","minEngineSchema":1,"maxEngineSchema":1,"downloadURL":"` + server.URL + `/gui.zip","checksumSHA256":"` + sum + `"}
	]}`)
	if err := os.WriteFile(catalogPath, catalog, 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveNewestCompatible(context.Background(), catalogPath, filepath.Join(dir, "downloads"), schemaversion.Engine, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.GUIVersion != "2026.10.15.01" || resolved.LocalPath == "" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if _, err := os.Stat(resolved.LocalPath); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRejectsPackageWhoseSchemaJSONIsIncompatible(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "gui.zip")
	writeSchemaZip(t, zipPath, `{"guiVersion":"9.0.0","minEngineSchema":9,"maxEngineSchema":9}`)
	sum := fileSHA256(t, zipPath)
	body, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	catalogPath := filepath.Join(dir, "schema-versions.json")
	catalog := []byte(`{"schema":"fse-gui-schema-catalog-v1","packages":[
		{"guiVersion":"9.0.0","minEngineSchema":1,"maxEngineSchema":1,"downloadURL":"` + server.URL + `/gui.zip","checksumSHA256":"` + sum + `"}
	]}`)
	if err := os.WriteFile(catalogPath, catalog, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveNewestCompatible(context.Background(), catalogPath, filepath.Join(dir, "downloads"), schemaversion.Engine, server.Client()); err == nil {
		t.Fatal("expected package schema.json outside the engine range to be rejected")
	}
}

func writeSchemaZip(t *testing.T, path, schemaJSON string) {
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
	if _, err := entry.Write([]byte(schemaJSON)); err != nil {
		t.Fatal(err)
	}
	index, err := zw.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Write([]byte("<!doctype html><title>File Sync Engine</title>")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func stringsRepeat(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
