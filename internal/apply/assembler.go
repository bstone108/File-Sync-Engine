package apply

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filesyncengine/internal/block"
)

type blockSource struct {
	path  string
	block block.Block
}

// AssembleFromLocalBlocks stages target from reusable local blocks, verifies it,
// then atomically replaces targetPath. Missing blocks are an error; network/block
// providers will fill that gap in a later protocol slice.
func AssembleFromLocalBlocks(targetPath string, target block.Manifest, sources []block.Manifest) error {
	return AssembleFromLocalBlocksBeforeRename(targetPath, target, sources, nil)
}

func AssembleFromLocalBlocksBeforeRename(targetPath string, target block.Manifest, sources []block.Manifest, beforeRename func() error) error {
	index := indexSources(sources)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".*.staging")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	for _, want := range target.Blocks {
		source, ok := index[blockKey(want)]
		if !ok {
			_ = tmp.Close()
			return fmt.Errorf("missing block %d for %s", want.Index, target.Path)
		}
		if err := copyBlock(tmp, source, want); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := verifyManifest(tmpName, target); err != nil {
		return err
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func indexSources(sources []block.Manifest) map[string]blockSource {
	index := make(map[string]blockSource)
	for _, manifest := range sources {
		for _, b := range manifest.Blocks {
			key := blockKey(b)
			if _, exists := index[key]; !exists {
				index[key] = blockSource{path: manifest.Path, block: b}
			}
		}
	}
	return index
}

func blockKey(b block.Block) string {
	return fmt.Sprintf("%d:%s", b.Size, hex.EncodeToString(b.Hash))
}

func copyBlock(dst *os.File, source blockSource, want block.Block) error {
	in, err := os.Open(source.path)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := in.Seek(source.block.Offset, io.SeekStart); err != nil {
		return err
	}
	limited := io.LimitReader(in, int64(want.Size))
	if _, err := dst.Seek(want.Offset, io.SeekStart); err != nil {
		return err
	}
	_, err = io.CopyN(dst, limited, int64(want.Size))
	return err
}

func verifyManifest(path string, want block.Manifest) error {
	got, err := block.BuildManifest(path, want.BlockSize)
	if err != nil {
		return err
	}
	if got.Size != want.Size || len(got.Blocks) != len(want.Blocks) {
		return fmt.Errorf("assembled file does not match target size/block count")
	}
	for i := range want.Blocks {
		if got.Blocks[i].Size != want.Blocks[i].Size || !bytes.Equal(got.Blocks[i].Hash, want.Blocks[i].Hash) {
			return fmt.Errorf("assembled block %d failed verification", i)
		}
	}
	return nil
}
