package foldershare

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"filesyncengine/internal/config"
	"filesyncengine/internal/link"
)

const (
	// SmallFileBatchCount is how many tiny files share one request.
	SmallFileBatchCount = 32
	// SmallFileBatchBytes is the largest file pulled in that request.
	// Larger files stay on the single-block path.
	SmallFileBatchBytes = 256 << 10
)

// BatchFile is one file inside a small-file batch.
type BatchFile struct {
	Path string
	Data []byte
}

type batchRequest struct {
	Files []batchRequestFile `json:"files"`
}

type batchRequestFile struct {
	Path  string `json:"path"`
	Index int    `json:"index"`
}

// WriteFileBatch writes length-prefixed file bodies. Lengths are bounded by
// the constants above; callers do not allocate from a peer-supplied size.
func WriteFileBatch(w io.Writer, files []BatchFile) error {
	for _, file := range files {
		path := []byte(file.Path)
		if len(path) == 0 || len(path) > 4096 || len(file.Data) > TransferBlockSize {
			return fmt.Errorf("batch file %q does not fit", file.Path)
		}
		if err := binary.Write(w, binary.BigEndian, uint32(len(path))); err != nil {
			return err
		}
		if _, err := w.Write(path); err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, uint32(len(file.Data))); err != nil {
			return err
		}
		if _, err := w.Write(file.Data); err != nil {
			return err
		}
	}
	return nil
}

// ReadFileBatch reads the frames WriteFileBatch produced.
func ReadFileBatch(r io.Reader) ([]BatchFile, error) {
	var out []BatchFile
	pathBuf := make([]byte, 4096)
	dataBuf := make([]byte, TransferBlockSize)
	for {
		var pathLen uint32
		if err := binary.Read(r, binary.BigEndian, &pathLen); err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
		if pathLen == 0 || int(pathLen) > len(pathBuf) {
			return out, fmt.Errorf("batch path length %d is invalid", pathLen)
		}
		if _, err := io.ReadFull(r, pathBuf[:pathLen]); err != nil {
			return out, err
		}
		var dataLen uint32
		if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
			return out, err
		}
		if int(dataLen) > len(dataBuf) {
			return out, fmt.Errorf("batch body length %d is invalid", dataLen)
		}
		if _, err := io.ReadFull(r, dataBuf[:dataLen]); err != nil {
			return out, err
		}
		out = append(out, BatchFile{
			Path: string(pathBuf[:pathLen]),
			Data: append([]byte(nil), dataBuf[:dataLen]...),
		})
		if len(out) > SmallFileBatchCount {
			return out, fmt.Errorf("batch has too many files")
		}
	}
}

func smallBatchEligible(entry Entry) bool {
	return entry.Size > 0 && entry.Size <= SmallFileBatchBytes && len(entry.Blocks) == 1 && entry.Blocks[0].Index == 0 && entry.Blocks[0].Size == int(entry.Size)
}

type batchOutcome struct {
	writes  int
	fetched int
}

func applySmallBatches(peer config.PeerConfig, folder config.FolderConfig, jobs []transferJob, byPath map[string]Entry, mu *sync.Mutex, remainMu *sync.Mutex, remaining *int64, note func(delta, left int64)) (batchOutcome, []transferJob, error) {
	var outcome batchOutcome
	var rest []transferJob
	var small []transferJob
	for _, job := range jobs {
		if smallBatchEligible(job.entry) {
			small = append(small, job)
			continue
		}
		rest = append(rest, job)
	}
	if len(small) < 2 {
		return outcome, jobs, nil
	}
	var applyErr error
	for start := 0; start < len(small); start += SmallFileBatchCount {
		end := start + SmallFileBatchCount
		if end > len(small) {
			end = len(small)
		}
		chunk := small[start:end]
		bodies, err := fetchSmallBatch(peer, folder.ID, chunk)
		if err != nil {
			rest = append(rest, chunk...)
			continue
		}
		for _, job := range chunk {
			body, ok := bodies[job.entry.Path]
			if !ok || !hashEquals(body, job.entry.Blocks[0].Hash) || int64(len(body)) != job.entry.Size {
				rest = append(rest, job)
				continue
			}
			if err := writeSmallFile(job.target, job.entry, body); err != nil {
				applyErr = fmt.Errorf("download %s: %w", job.entry.Path, err)
				rest = append(rest, job)
				continue
			}
			mu.Lock()
			byPath[job.entry.Path] = job.entry
			mu.Unlock()
			outcome.writes++
			outcome.fetched++
			remainMu.Lock()
			*remaining -= job.entry.Size
			if *remaining < 0 {
				*remaining = 0
			}
			left := *remaining
			remainMu.Unlock()
			note(job.entry.Size, left)
		}
	}
	return outcome, rest, applyErr
}

func writeSmallFile(target string, entry Entry, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".fse-sync-tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return finishDownload(tmp, target, entry)
}

func fetchSmallBatch(peer config.PeerConfig, folderID string, jobs []transferJob) (map[string][]byte, error) {
	reqBody := batchRequest{Files: make([]batchRequestFile, 0, len(jobs))}
	for _, job := range jobs {
		reqBody.Files = append(reqBody.Files, batchRequestFile{Path: job.entry.Path, Index: job.entry.Blocks[0].Index})
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	var raw []byte
	_, err = link.Dial(context.Background(), peer, func(ctx context.Context, base string, client *http.Client) error {
		endpoint := base + "/v1/folder-batch?folder=" + url.QueryEscape(folderID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("X-FSE-API-Key", peer.APIKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("folder batch: %s", resp.Status)
		}
		read, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(jobs))*(TransferBlockSize+4096)+1))
		if err != nil {
			return err
		}
		raw = read
		return nil
	})
	if err != nil {
		return nil, err
	}
	files, err := ReadFileBatch(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(files))
	for _, file := range files {
		out[file.Path] = file.Data
	}
	return out, nil
}
