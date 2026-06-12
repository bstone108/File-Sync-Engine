package metabench

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cockroachdb/pebble"
	badger "github.com/dgraph-io/badger/v4"
	bolt "go.etcd.io/bbolt"
)

func DefaultCandidates() []Candidate {
	return []Candidate{PebbleCandidate{}, BadgerCandidate{}, BBoltCandidate{}}
}

type PebbleCandidate struct{}

func (PebbleCandidate) Name() string                        { return "pebble" }
func (c PebbleCandidate) Open(path string) (Store, error)   { return openPebble(c.Name(), path) }
func (c PebbleCandidate) Reopen(path string) (Store, error) { return openPebble(c.Name(), path) }

func openPebble(name, path string) (Store, error) {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		return nil, err
	}
	return &kvStore{name: name, put: func(key, value []byte) error { return db.Set(key, value, pebble.Sync) }, get: func(key []byte) ([]byte, error) {
		value, closer, err := db.Get(key)
		if err != nil {
			return nil, err
		}
		defer closer.Close()
		return append([]byte(nil), value...), nil
	}, close: db.Close}, nil
}

type BadgerCandidate struct{}

func (BadgerCandidate) Name() string                        { return "badger" }
func (c BadgerCandidate) Open(path string) (Store, error)   { return openBadger(c.Name(), path) }
func (c BadgerCandidate) Reopen(path string) (Store, error) { return openBadger(c.Name(), path) }

func openBadger(name, path string) (Store, error) {
	db, err := badger.Open(badger.DefaultOptions(path).WithLogger(nil))
	if err != nil {
		return nil, err
	}
	return &kvStore{name: name, put: func(key, value []byte) error {
		return db.Update(func(txn *badger.Txn) error { return txn.Set(key, value) })
	}, get: func(key []byte) ([]byte, error) {
		var out []byte
		err := db.View(func(txn *badger.Txn) error {
			item, err := txn.Get(key)
			if err != nil {
				return err
			}
			return item.Value(func(value []byte) error {
				out = append([]byte(nil), value...)
				return nil
			})
		})
		return out, err
	}, close: db.Close}, nil
}

type BBoltCandidate struct{}

func (BBoltCandidate) Name() string                        { return "bbolt" }
func (c BBoltCandidate) Open(path string) (Store, error)   { return openBBolt(c.Name(), path) }
func (c BBoltCandidate) Reopen(path string) (Store, error) { return openBBolt(c.Name(), path) }

func openBBolt(name, path string) (Store, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(filepath.Join(path, "metadata.db"), 0o600, nil)
	if err != nil {
		return nil, err
	}
	bucket := []byte("metadata")
	if err := db.Update(func(tx *bolt.Tx) error { _, err := tx.CreateBucketIfNotExists(bucket); return err }); err != nil {
		db.Close()
		return nil, err
	}
	return &kvStore{name: name, put: func(key, value []byte) error {
		return db.Update(func(tx *bolt.Tx) error { return tx.Bucket(bucket).Put(key, value) })
	}, get: func(key []byte) ([]byte, error) {
		var out []byte
		err := db.View(func(tx *bolt.Tx) error {
			value := tx.Bucket(bucket).Get(key)
			if value == nil {
				return fmt.Errorf("key not found")
			}
			out = append([]byte(nil), value...)
			return nil
		})
		return out, err
	}, close: db.Close}, nil
}

type kvStore struct {
	name  string
	put   func(key, value []byte) error
	get   func(key []byte) ([]byte, error)
	close func() error
}

func (s *kvStore) ImportFiles(ctx context.Context, folders, filesPerFolder, blocksPerFile int) error {
	for folder := 0; folder < folders; folder++ {
		for file := 0; file < filesPerFolder; file++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.put(fileKey(folder, file), encodeUint64(uint64(blocksPerFile))); err != nil {
				return err
			}
			for block := 0; block < blocksPerFile; block++ {
				hash := syntheticHash(folder, file, block)
				if err := s.put(blockKey(hash, folder, file, block), encodeUint64(uint64(block))); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *kvStore) UpdateLazyHashes(ctx context.Context, updates int) error {
	for update := 0; update < updates; update++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.put(lazyKey(update), syntheticHash(update, update*3, update*7)); err != nil {
			return err
		}
	}
	return nil
}

func (s *kvStore) LookupContentHashes(ctx context.Context, lookups int) error {
	for lookup := 0; lookup < lookups; lookup++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, _ = s.get(blockKey(syntheticHash(0, lookup, 0), 0, lookup, 0))
	}
	return nil
}

func (s *kvStore) RunStatusReaders(ctx context.Context, readers int) (int, error) {
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for reader := 0; reader < readers; reader++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if err := ctx.Err(); err != nil {
					errs <- err
					return
				}
				_, _ = s.get(fileKey(id%2, i))
			}
		}(reader)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return 0, err
		}
	}
	return readers * 20, nil
}

func (s *kvStore) VerifyAfterReopen(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.get(fileKey(0, 0))
	return err
}

func (s *kvStore) Close() error { return s.close() }

func fileKey(folder, file int) []byte {
	return []byte(fmt.Sprintf("folder/%04d/file/%08d", folder, file))
}
func lazyKey(update int) []byte { return []byte(fmt.Sprintf("lazy/%08d", update)) }
func blockKey(hash []byte, folder, file, block int) []byte {
	return []byte(fmt.Sprintf("block/%x/%04d/%08d/%04d", hash, folder, file, block))
}
func syntheticHash(a, b, c int) []byte {
	var buf [24]byte
	binary.LittleEndian.PutUint64(buf[0:8], uint64(a))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(b))
	binary.LittleEndian.PutUint64(buf[16:24], uint64(c))
	h := sha256.Sum256(buf[:])
	return h[:]
}
func encodeUint64(v uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return append([]byte(nil), buf[:]...)
}
