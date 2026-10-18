package foldershare

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filesyncengine/internal/config"
)

func TestJournalStoresBlockHashesWithoutSubHashes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	Reconcile(config.Config{Folders: []config.FolderConfig{{
		ID: "shared", Path: root, Enabled: true, Mode: config.ModeSendReceive,
	}}}, configPath, nil)
	data, err := os.ReadFile(JournalPath(configPath, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"blocks"`) || !strings.Contains(text, `"hash"`) {
		t.Fatalf("journal missing block list: %s", text)
	}
	if strings.Contains(text, "subHash") || strings.Contains(text, "subSize") {
		t.Fatalf("journal stored sub-block metadata: %s", text)
	}
}

func TestLocalBlockReuseSkipsNetwork(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = nil
	t.Cleanup(func() { fileRetryBackoff = previous })

	payload := []byte("shared-block-body")
	journal := journalFor(t, "shared", "b.txt", payload)
	var blockHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sync-journal" {
			http.ServeFile(w, r, journal)
			return
		}
		blockHits++
		http.Error(w, "block was requested", http.StatusInternalServerError)
	}))
	defer server.Close()

	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(localRoot, "a.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	Reconcile(localConfig(localRoot, server.URL), filepath.Join(t.TempDir(), "local.json"), nil)
	got, err := os.ReadFile(filepath.Join(localRoot, "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reused body = %q", got)
	}
	if blockHits != 0 {
		t.Fatalf("network block requests = %d", blockHits)
	}
}

func TestShortBlockIsNotCommitted(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = nil
	t.Cleanup(func() { fileRetryBackoff = previous })

	payload := bytes.Repeat([]byte{'Z'}, 32)
	journal := journalFor(t, "shared", "short.bin", payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sync-journal" {
			http.ServeFile(w, r, journal)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	localRoot := t.TempDir()
	Reconcile(localConfig(localRoot, server.URL), filepath.Join(t.TempDir(), "local.json"), nil)
	if _, err := os.Stat(filepath.Join(localRoot, "short.bin")); !os.IsNotExist(err) {
		t.Fatalf("short block was committed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "short.bin.fse-sync-state")); !os.IsNotExist(err) {
		t.Fatalf("bad block was recorded in the partial map: %v", err)
	}
}

func TestRestartFetchesOnlyMissingBlocks(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = nil
	t.Cleanup(func() { fileRetryBackoff = previous })

	payload := bytes.Repeat([]byte{'C'}, TransferBlockSize*2)
	entry := entryFor("wide.bin", payload)
	journal := journalForEntries(t, "shared", []Entry{entry})
	var phase int
	var second []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sync-journal" {
			http.ServeFile(w, r, journal)
			return
		}
		index := r.URL.Query().Get("index")
		if phase == 0 && index == "1" {
			http.Error(w, "connection dropped", http.StatusBadGateway)
			return
		}
		if phase == 1 && r.URL.Path == "/v1/folder-block" && r.URL.Query().Get("sub") == "" {
			second = append(second, index)
		}
		serveBlockPayload(w, r, payload)
	}))
	defer server.Close()

	localRoot := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "local.json")
	Reconcile(localConfig(localRoot, server.URL), configPath, nil)
	if _, err := os.Stat(filepath.Join(localRoot, "wide.bin")); !os.IsNotExist(err) {
		t.Fatalf("incomplete file was published: %v", err)
	}
	state, err := os.ReadFile(filepath.Join(localRoot, "wide.bin.fse-sync-state"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state), `"have": [`) || strings.Contains(string(state), "subHash") || strings.Contains(string(state), "subSize") {
		t.Fatalf("partial map = %s", state)
	}
	var stored partialState
	if err := json.Unmarshal(state, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Have) != 1 || stored.Have[0] != 0 {
		t.Fatalf("have = %v", stored.Have)
	}

	phase = 1
	Reconcile(localConfig(localRoot, server.URL), configPath, nil)
	got, err := os.ReadFile(filepath.Join(localRoot, "wide.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("resumed %d bytes", len(got))
	}
	if strings.Join(second, ",") != "1" {
		t.Fatalf("blocks requested after restart = %v", second)
	}
	if _, err := os.Stat(filepath.Join(localRoot, "wide.bin.fse-sync-state")); !os.IsNotExist(err) {
		t.Fatalf("state file left behind: %v", err)
	}
}

func TestMissingBlockComesFromNextPeer(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = nil
	t.Cleanup(func() { fileRetryBackoff = previous })

	payload := bytes.Repeat([]byte{'D'}, TransferBlockSize+3)
	journal := journalFor(t, "shared", "split.bin", payload)
	serve := func(failSecond bool, seen *[]string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/sync-journal" {
				http.ServeFile(w, r, journal)
				return
			}
			if r.URL.Path == "/v1/folder-block" && r.URL.Query().Get("sub") == "" {
				*seen = append(*seen, r.URL.Query().Get("index"))
			}
			if failSecond && r.URL.Query().Get("index") == "1" {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			serveBlockPayload(w, r, payload)
		})
	}
	var first, second []string
	serverA := httptest.NewServer(serve(true, &first))
	defer serverA.Close()
	serverB := httptest.NewServer(serve(false, &second))
	defer serverB.Close()

	localRoot := t.TempDir()
	cfg := localConfig(localRoot, serverA.URL)
	cfg.Peers = append(cfg.Peers, config.PeerConfig{
		ID: "other", APIKey: "secret",
		Endpoints: []config.EndpointConfig{{Kind: "manual", Address: serverB.URL}},
	})
	Reconcile(cfg, filepath.Join(t.TempDir(), "local.json"), nil)
	got, err := os.ReadFile(filepath.Join(localRoot, "split.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("merged %d bytes", len(got))
	}
	if strings.Join(first, ",") != "0,1" || strings.Join(second, ",") != "1" {
		t.Fatalf("peer A blocks %v, peer B blocks %v", first, second)
	}
}

func TestSubBlockBenchmarkAgainstWholeBlock(t *testing.T) {
	previous := fileRetryBackoff
	fileRetryBackoff = nil
	t.Cleanup(func() { fileRetryBackoff = previous })

	block := bytes.Repeat([]byte{'A'}, TransferBlockSize)
	started := time.Now()
	_ = SubHashes(block, SubBlockSize)
	_ = SubHashes(block, SubBlockSize)
	cpu := time.Since(started)
	if cpu > time.Second {
		t.Fatalf("sub-hash cpu %s is not negligible", cpu)
	}

	smallOld := append(append([]byte{}, block...), block...)
	smallNew := append([]byte{}, smallOld...)
	smallNew[10] ^= 0xff
	small := measureEdit(t, "small.bin", smallOld, smallNew)
	if small.fullBlocks != 0 || small.subRanges != 1 || small.fileBytes != SubBlockSize {
		t.Fatalf("small edit transferred %+v", small)
	}
	if small.wireBytes*8 >= TransferBlockSize {
		t.Fatalf("small edit wire %d is not an 8x win over %d", small.wireBytes, TransferBlockSize)
	}

	scatteredOld := append([]byte{}, smallOld...)
	scatteredNew := append([]byte{}, scatteredOld...)
	scatteredNew[10] ^= 0xff
	scatteredNew[TransferBlockSize+20] ^= 0xff
	scattered := measureEdit(t, "scattered.bin", scatteredOld, scatteredNew)
	if scattered.fullBlocks != 0 || scattered.subRanges != 2 || scattered.fileBytes != SubBlockSize*2 {
		t.Fatalf("scattered edit transferred %+v", scattered)
	}

	denseOld := append([]byte{}, block...)
	denseNew := append([]byte{}, denseOld...)
	for offset := 0; offset < len(denseNew); offset += SubBlockSize {
		denseNew[offset] ^= 0xff
	}
	dense := measureEdit(t, "dense.bin", denseOld, denseNew)
	if dense.fullBlocks != 1 || dense.subRanges != 0 {
		t.Fatalf("dense edit transferred %+v", dense)
	}
	if dense.metaBytes <= 0 || dense.metaBytes > 4096 {
		t.Fatalf("dense metadata overhead = %d", dense.metaBytes)
	}

	const bytesPerSecond = 8_000_000 / 8
	t.Logf("BENCH small-edit file_bytes=%d meta_bytes=%d wire_bytes=%d whole_block=%d ratio=%.4f 8mbit_wire=%.3fs 8mbit_whole=%.3fs cpu=%s",
		small.fileBytes, small.metaBytes, small.wireBytes, TransferBlockSize,
		float64(small.wireBytes)/float64(TransferBlockSize),
		float64(small.wireBytes)/bytesPerSecond, float64(TransferBlockSize)/bytesPerSecond, cpu)
	t.Logf("BENCH scattered-two-blocks file_bytes=%d meta_bytes=%d wire_bytes=%d whole_blocks=%d ratio=%.4f 8mbit_wire=%.3fs 8mbit_whole=%.3fs",
		scattered.fileBytes, scattered.metaBytes, scattered.wireBytes, TransferBlockSize*2,
		float64(scattered.wireBytes)/float64(TransferBlockSize*2),
		float64(scattered.wireBytes)/bytesPerSecond, float64(TransferBlockSize*2)/bytesPerSecond)
	t.Logf("BENCH dense-one-block file_bytes=%d meta_bytes=%d wire_bytes=%d whole_block=%d overhead_ratio=%.6f 8mbit_wire=%.3fs 8mbit_whole=%.3fs",
		dense.fileBytes, dense.metaBytes, dense.wireBytes, TransferBlockSize,
		float64(dense.metaBytes)/float64(TransferBlockSize),
		float64(dense.wireBytes)/bytesPerSecond, float64(TransferBlockSize)/bytesPerSecond)
}

type transferMeter struct {
	fullBlocks int
	subRanges  int
	fileBytes  int
	metaBytes  int
	wireBytes  int
}

func measureEdit(t *testing.T, name string, oldBody, newBody []byte) transferMeter {
	t.Helper()
	entry := entryFor(name, newBody)
	entry.Revision = 2
	journal := journalForEntries(t, "shared", []Entry{entry})
	var mu sync.Mutex
	var meter transferMeter
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sync-journal" {
			http.ServeFile(w, r, journal)
			return
		}
		rec := &countingWriter{ResponseWriter: w}
		serveBlockPayload(rec, r, newBody)
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/v1/folder-block-subhashes" {
			meter.metaBytes += rec.n
		} else if r.URL.Query().Get("sub") != "" {
			meter.subRanges++
			meter.fileBytes += rec.n
		} else if r.URL.Path == "/v1/folder-block" {
			meter.fullBlocks++
			meter.fileBytes += rec.n
		}
		meter.wireBytes += rec.n
	}))
	defer server.Close()

	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(localRoot, name), oldBody, 0o644); err != nil {
		t.Fatal(err)
	}
	Reconcile(localConfig(localRoot, server.URL), filepath.Join(t.TempDir(), "local.json"), nil)
	got, err := os.ReadFile(filepath.Join(localRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBody) {
		t.Fatalf("%s converged to %d bytes", name, len(got))
	}
	if _, err := os.Stat(filepath.Join(localRoot, name+".fse-sync-state")); !os.IsNotExist(err) {
		t.Fatalf("%s left partial state behind: %v", name, err)
	}
	return meter
}

type countingWriter struct {
	http.ResponseWriter
	n int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.n += n
	return n, err
}

func TestHashSubsMatchesSliceHashes(t *testing.T) {
	payload := bytes.Repeat([]byte("abcd"), 50)
	want := SubHashes(payload[:100], 40)
	got, err := HashSubs(bytes.NewReader(payload[:100]), 100, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("hashes=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hash %d = %s", i, got[i])
		}
	}
}

func TestPreferSubRangesKeepsSparseEdits(t *testing.T) {
	if !preferSubRanges(SubBlockSize, TransferBlockSize, 1) {
		t.Fatal("one sub-block should replace the whole block")
	}
	if preferSubRanges(TransferBlockSize/2, TransferBlockSize, TransferBlockSize/SubBlockSize/2) {
		t.Fatal("half the block is not a clear enough win")
	}
	if preferSubRanges(TransferBlockSize, TransferBlockSize, TransferBlockSize/SubBlockSize) {
		t.Fatal("a fully changed block should be fetched whole")
	}
	whole := float64(TransferBlockSize)
	sparse := float64(SubBlockSize + 2048)
	if sparse*8 >= whole {
		t.Fatalf("modeled sparse transfer %.0f is not 8x smaller than %.0f", sparse, whole)
	}
}
