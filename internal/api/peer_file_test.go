package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/foldershare"
)

func TestFolderIndexAndFileDownloadExposeConfiguredFolderData(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "doc.txt"), []byte("peer data"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{NodeName: "node-a", FoldersState: []FolderState{{ID: "docs", Path: root, Mode: "sendonly", Status: "configured"}}}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/folder-index?folder=docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-FSE-API-Key", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", resp.StatusCode)
	}
	var index struct {
		Files []struct {
			RelativePath string `json:"relativePath"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		t.Fatal(err)
	}
	if len(index.Files) != 1 || index.Files[0].RelativePath != "doc.txt" {
		t.Fatalf("unexpected index: %+v", index)
	}

	req, err = http.NewRequest(http.MethodGet, httpServer.URL+"/v1/folder-file?folder=docs&path=doc.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-FSE-API-Key", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("file status = %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "peer data" {
		t.Fatalf("downloaded %q", string(data))
	}
}

func TestFolderBlockDownloadReturnsRequestedBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "chunked.txt"), []byte("abcdefghij"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{NodeName: "node-a", FoldersState: []FolderState{{ID: "docs", Path: root, Mode: "sendonly", Status: "configured"}}}, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/folder-block?folder=docs&path=chunked.txt&index=1&blockSize=4", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "efgh" {
		t.Fatalf("block body = %q", got)
	}
}

func TestFolderFileAndBlockRejectPathTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "doc.txt"), []byte("peer data"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{NodeName: "node-a", FoldersState: []FolderState{{ID: "docs", Path: root, Mode: "sendonly", Status: "configured"}}}, "secret")

	cases := []string{
		"/v1/folder-file?folder=docs&path=../escape.txt",
		"/v1/folder-file?folder=docs&path=/absolute.txt",
		"/v1/folder-block?folder=docs&path=../escape.txt&index=0&blockSize=4",
		"/v1/folder-block?folder=docs&path=/absolute.txt&index=0&blockSize=4",
	}
	for _, path := range cases {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-FSE-API-Key", "secret")
		rec := httptest.NewRecorder()
		server.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestFolderFileAndBlockRejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	server := NewServer(State{NodeName: "node-a", FoldersState: []FolderState{{ID: "docs", Path: root, Mode: "sendonly", Status: "configured"}}}, "secret")

	cases := []string{
		"/v1/folder-file?folder=docs&path=link.txt",
		"/v1/folder-block?folder=docs&path=link.txt&index=0&blockSize=4",
	}
	for _, path := range cases {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-FSE-API-Key", "secret")
		rec := httptest.NewRecorder()
		server.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); got == "outside secret" {
			t.Fatalf("%s leaked outside file content", path)
		}
	}
}

func TestFolderBlockSubhashAndManifestAreNotStored(t *testing.T) {
	root := t.TempDir()
	payload := bytes.Repeat([]byte("abcd"), 50)
	if err := os.WriteFile(filepath.Join(root, "chunked.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{NodeName: "node-a", FoldersState: []FolderState{{ID: "docs", Path: root, Mode: "sendonly", Status: "configured"}}}, "secret")
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/folder-block-subhashes?folder=docs&path=chunked.txt&index=0&blockSize=100&subSize=40", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subhash status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := foldershare.SubHashes(payload[:100], 40)
	if len(got.Hashes) != len(want) {
		t.Fatalf("hashes=%v", got.Hashes)
	}
	for i := range want {
		if got.Hashes[i] != want[i] {
			t.Fatalf("hash %d = %s", i, got.Hashes[i])
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/folder-block?folder=docs&path=chunked.txt&index=0&blockSize=100&sub=1&subSize=40", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != string(payload[40:80]) {
		t.Fatalf("subrange status=%d body=%q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/folder-manifest?folder=docs&path=chunked.txt", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"blockSize"`)) || bytes.Contains(rec.Body.Bytes(), []byte("subHash")) {
		t.Fatalf("manifest status=%d body=%s", rec.Code, rec.Body.String())
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("handler stored %d entries, want %d", len(after), len(before))
	}
}

func TestFolderBlockDownloadRejectsOutOfRangeIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{NodeName: "node-a", FoldersState: []FolderState{{ID: "docs", Path: root, Mode: "sendonly", Status: "configured"}}}, "secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/folder-block?folder=docs&path=small.txt&index=5&blockSize=4", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
