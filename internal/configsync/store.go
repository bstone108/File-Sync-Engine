package configsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Store is the on-disk config database for one engine.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open loads an existing replica or creates one for instanceID.
// An existing file keeps the UUID already stored in it.
func Open(path, groupID, instanceID string) (*Store, error) {
	if path == "" {
		return nil, errors.New("config sync path is required")
	}
	store := &Store{path: path}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := ValidateInstanceID(instanceID); err != nil {
			return nil, err
		}
		if err := store.Save(NewReplica(groupID, instanceID)); err != nil {
			return nil, err
		}
		return store, nil
	} else if err != nil {
		return nil, err
	}
	replica, err := store.Load()
	if err != nil {
		return nil, err
	}
	if err := ValidateInstanceID(replica.InstanceID); err != nil {
		return nil, err
	}
	return store, nil
}

// Load reads the replica.
func (s *Store) Load() (Replica, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Save writes the replica atomically.
func (s *Store) Save(replica Replica) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(replica)
}

// Update loads, mutates, and saves the replica.
func (s *Store) Update(fn func(*Replica) error) (Replica, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replica, err := s.load()
	if err != nil {
		return Replica{}, err
	}
	if err := fn(&replica); err != nil {
		return Replica{}, err
	}
	if err := s.save(replica); err != nil {
		return Replica{}, err
	}
	return replica, nil
}

func (s *Store) load() (Replica, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return Replica{}, err
	}
	var replica Replica
	if err := json.Unmarshal(raw, &replica); err != nil {
		return Replica{}, err
	}
	if replica.Records == nil {
		replica.Records = map[string]InstanceRecord{}
	}
	if replica.Connections == nil {
		replica.Connections = map[string]Connection{}
	}
	return replica, nil
}

func (s *Store) save(replica Replica) error {
	raw, err := json.MarshalIndent(replica, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "config-sync-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}
