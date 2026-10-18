package foldershare

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/structuredlog"
)

func TestDownloadContinuesPastEightSecondBodyStall(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = nil
	t.Cleanup(func() { fileRetryBackoff = previous })

	payload := bytes.Repeat([]byte{'A'}, 64)
	journal := journalFor(t, "shared", "slow.bin", payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sync-journal" {
			http.ServeFile(w, r, journal)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(payload[:1]); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(9 * time.Second)
		_, _ = w.Write(payload[1:])
	}))
	defer server.Close()

	localRoot := t.TempDir()
	Reconcile(localConfig(localRoot, server.URL), filepath.Join(t.TempDir(), "local.json"), nil)
	got, err := os.ReadFile(filepath.Join(localRoot, "slow.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("synced %d bytes", len(got))
	}
}

func TestReconcileContinuesAfterOneFileFails(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = nil
	t.Cleanup(func() { fileRetryBackoff = previous })

	good := []byte("good-file")
	bad := []byte("bad-file")
	journal := journalForEntries(t, "shared", []Entry{entryFor("bad.txt", bad), entryFor("good.txt", good)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sync-journal":
			http.ServeFile(w, r, journal)
		case "/v1/folder-block":
			if r.URL.Query().Get("path") == "bad.txt" {
				http.Error(w, "nope", http.StatusInternalServerError)
				return
			}
			serveBlockPayload(w, r, good)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	localRoot := t.TempDir()
	Reconcile(localConfig(localRoot, server.URL), filepath.Join(t.TempDir(), "local.json"), nil)
	if _, err := os.Stat(filepath.Join(localRoot, "bad.txt")); !os.IsNotExist(err) {
		t.Fatalf("failed file was stored: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(localRoot, "good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(good) {
		t.Fatalf("good body = %q", got)
	}
}

func TestDownloadResumesPartial(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { fileRetryBackoff = previous })

	payload := bytes.Repeat([]byte{'B'}, 32)
	journal := journalFor(t, "shared", "part.bin", payload)
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sync-journal" {
			http.ServeFile(w, r, journal)
			return
		}
		hits++
		if hits == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			conn, bufrw, err := hj.Hijack()
			if err != nil {
				return
			}
			_, _ = bufrw.WriteString("HTTP/1.1 206 Partial Content\r\nContent-Length: 4\r\n\r\n")
			_, _ = bufrw.Write(payload[:4])
			_ = bufrw.Flush()
			_ = conn.Close()
			return
		}
		serveBlockPayload(w, r, payload)
	}))
	defer server.Close()

	localRoot := t.TempDir()
	Reconcile(localConfig(localRoot, server.URL), filepath.Join(t.TempDir(), "local.json"), nil)
	got, err := os.ReadFile(filepath.Join(localRoot, "part.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("resumed body len %d", len(got))
	}
	if hits < 2 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestSyncErrorsAreStructured(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fse.jsonl")
	if err := structuredlog.Configure(structuredlog.Options{Level: "info", Output: logPath}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(structuredlog.Reset)

	Reconcile(localConfig(t.TempDir(), "http://127.0.0.1:1"), filepath.Join(t.TempDir(), "local.json"), nil)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "folder.sync.journal_failed") {
		t.Fatalf("log = %s", data)
	}
}

func localConfig(root, peerURL string) config.Config {
	return config.Config{
		NodeName: "local",
		Folders: []config.FolderConfig{{
			ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendReceive,
		}},
		Peers: []config.PeerConfig{{
			ID: "remote", APIKey: "secret",
			Endpoints: []config.EndpointConfig{{Kind: "manual", Address: peerURL}},
		}},
	}
}

func journalFor(t *testing.T, folder, name string, payload []byte) string {
	t.Helper()
	return journalForEntries(t, folder, []Entry{entryFor(name, payload)})
}

func journalForEntries(t *testing.T, folder string, entries []Entry) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "remote.json")
	if err := saveJournal(configPath, folder, indexEntries(entries)); err != nil {
		t.Fatal(err)
	}
	return JournalPath(configPath, folder)
}

func entryFor(name string, payload []byte) Entry {
	sum := sha256.Sum256(payload)
	return Entry{
		Path: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(payload)),
		Revision: 1, Origin: "remote", Blocks: blocksFromBytes(payload),
	}
}

func serveBlockPayload(w http.ResponseWriter, r *http.Request, payload []byte) {
	if r.URL.Path == "/v1/folder-block-subhashes" {
		index, blockSize, subSize := blockParams(r)
		data := sliceBlock(payload, index, blockSize, -1, subSize)
		if data == nil {
			http.Error(w, "range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"hashes": SubHashes(data, subSize)})
		return
	}
	index, blockSize, subSize := blockParams(r)
	sub := -1
	if raw := r.URL.Query().Get("sub"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "sub", http.StatusBadRequest)
			return
		}
		sub = parsed
	}
	data := sliceBlock(payload, index, blockSize, sub, subSize)
	if data == nil {
		http.Error(w, "range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}

func blockParams(r *http.Request) (index, blockSize, subSize int) {
	index, _ = strconv.Atoi(r.URL.Query().Get("index"))
	blockSize = TransferBlockSize
	if raw := r.URL.Query().Get("blockSize"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			blockSize = parsed
		}
	}
	subSize = SubBlockSize
	if raw := r.URL.Query().Get("subSize"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			subSize = parsed
		}
	}
	return index, blockSize, subSize
}

func sliceBlock(payload []byte, index, blockSize, sub, subSize int) []byte {
	if index < 0 || blockSize <= 0 {
		return nil
	}
	start := index * blockSize
	if start >= len(payload) {
		return nil
	}
	end := start + blockSize
	if end > len(payload) {
		end = len(payload)
	}
	if sub >= 0 {
		if subSize <= 0 {
			return nil
		}
		rel := sub * subSize
		if rel >= end-start {
			return nil
		}
		subEnd := start + rel + subSize
		if subEnd > end {
			subEnd = end
		}
		start += rel
		end = subEnd
	}
	return payload[start:end]
}
