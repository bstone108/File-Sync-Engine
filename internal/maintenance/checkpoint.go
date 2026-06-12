package maintenance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type FileCheckpoint struct {
	Path string
}

func (f FileCheckpoint) LoadMaintenanceCursor() (Cursor, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Cursor{}, nil
	}
	if err != nil {
		return Cursor{}, err
	}
	var cursor Cursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return Cursor{}, err
	}
	return cursor, nil
}

func (f FileCheckpoint) SaveMaintenanceCursor(cursor Cursor) error {
	if f.Path == "" {
		return errors.New("maintenance checkpoint path is required")
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cursor, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".maintenance-cursor-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, f.Path)
}
