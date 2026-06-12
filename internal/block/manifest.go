package block

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

type Manifest struct {
	Path                           string  `json:"path"`
	Size                           int64   `json:"size"`
	BlockSize                      int     `json:"blockSize"`
	Blocks                         []Block `json:"blocks"`
	HashState                      string  `json:"hashState,omitempty"`
	Damaged                        bool    `json:"damaged,omitempty"`
	ModTimeUnixNano                int64   `json:"modTimeUnixNano,omitempty"`
	ChangeTimeUnixNano             int64   `json:"changeTimeUnixNano,omitempty"`
	SeedBaselineModTimeUnixNano    int64   `json:"seedBaselineModTimeUnixNano,omitempty"`
	SeedBaselineChangeTimeUnixNano int64   `json:"seedBaselineChangeTimeUnixNano,omitempty"`
}

type Block struct {
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Size   int    `json:"size"`
	Hash   []byte `json:"hash"`
}

type DeltaPlan struct {
	Needed []Block `json:"needed"`
	Reused []Reuse `json:"reused,omitempty"`
}

type Reuse struct {
	TargetIndex int    `json:"targetIndex"`
	SourcePath  string `json:"sourcePath"`
	SourceIndex int    `json:"sourceIndex"`
}

func BuildManifest(path string, blockSize int) (Manifest, error) {
	if blockSize <= 0 {
		return Manifest{}, fmt.Errorf("blockSize must be positive")
	}
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{Path: path, Size: st.Size(), BlockSize: blockSize, HashState: "complete", ModTimeUnixNano: st.ModTime().UnixNano()}
	buf := make([]byte, blockSize)
	for index := 0; ; index++ {
		n, err := io.ReadFull(file, buf)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			// final partial block
		} else if err != nil {
			return Manifest{}, err
		}
		h := sha256.Sum256(buf[:n])
		manifest.Blocks = append(manifest.Blocks, Block{
			Index:  index,
			Offset: int64(index * blockSize),
			Size:   n,
			Hash:   append([]byte(nil), h[:]...),
		})
		if n < blockSize {
			break
		}
	}
	return manifest, nil
}

func PlanDelta(base Manifest, target Manifest) DeltaPlan {
	baseByIndex := make(map[int]Block, len(base.Blocks))
	for _, b := range base.Blocks {
		baseByIndex[b.Index] = b
	}
	plan := DeltaPlan{}
	for _, want := range target.Blocks {
		have, ok := baseByIndex[want.Index]
		if !ok || have.Size != want.Size || !bytes.Equal(have.Hash, want.Hash) {
			plan.Needed = append(plan.Needed, want)
		}
	}
	return plan
}

func PlanContentDelta(sources []Manifest, target Manifest) DeltaPlan {
	available := map[string]Reuse{}
	for _, source := range sources {
		for _, block := range source.Blocks {
			key := blockKey(block)
			if _, exists := available[key]; !exists {
				available[key] = Reuse{SourcePath: source.Path, SourceIndex: block.Index}
			}
		}
	}
	plan := DeltaPlan{}
	for _, want := range target.Blocks {
		if reuse, ok := available[blockKey(want)]; ok {
			reuse.TargetIndex = want.Index
			plan.Reused = append(plan.Reused, reuse)
			continue
		}
		plan.Needed = append(plan.Needed, want)
	}
	return plan
}

func blockKey(block Block) string {
	return string(block.Hash)
}
