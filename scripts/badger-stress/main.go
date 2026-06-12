package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

type counters struct {
	sets       uint64
	deletes    uint64
	gcRuns     uint64
	gcSkipped  uint64
	reopens    uint64
	errors     uint64
	lastSeq    uint64
	lastDisk   uint64
	lastLSM    uint64
	lastVLog   uint64
	phaseGrow  atomic.Bool
}

func main() {
	var dir string
	var targetBytes uint64
	var duration time.Duration
	var valueSize int
	var batchSize int
	var shareCount int
	var statsEvery time.Duration
	var gcEvery time.Duration
	var reopenEvery time.Duration
	var syncWrites bool
	var seed int64
	var keepVersions int

	flag.StringVar(&dir, "dir", "/opt/data/file-sync-badger-stress/db", "Badger database directory")
	flag.Uint64Var(&targetBytes, "target-bytes", 30*1024*1024*1024, "database directory size target before switching from grow to mutation phase")
	flag.DurationVar(&duration, "duration", 48*time.Hour, "total run duration")
	flag.IntVar(&valueSize, "value-size", 4096, "synthetic metadata value size in bytes")
	flag.IntVar(&batchSize, "batch-size", 1000, "write batch size during grow phase")
	flag.IntVar(&shareCount, "shares", 64, "number of synthetic shares/folders to distribute keys across")
	flag.DurationVar(&statsEvery, "stats-every", 30*time.Second, "stats logging interval")
	flag.DurationVar(&gcEvery, "gc-every", 10*time.Minute, "attempt value-log GC this often during mutation phase")
	flag.DurationVar(&reopenEvery, "reopen-every", 2*time.Hour, "close/reopen database this often; 0 disables")
	flag.BoolVar(&syncWrites, "sync-writes", false, "force Badger SyncWrites for more durable but slower writes")
	flag.Int64Var(&seed, "seed", 20260523, "deterministic RNG seed")
	flag.IntVar(&keepVersions, "versions", 1, "Badger NumVersionsToKeep")
	flag.Parse()

	if valueSize < 256 {
		fatalf("value-size must be >= 256")
	}
	if batchSize < 1 {
		fatalf("batch-size must be >= 1")
	}
	if shareCount < 1 {
		fatalf("shares must be >= 1")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatalf("create db dir: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deadline := time.Now().Add(duration)

	c := &counters{}
	c.phaseGrow.Store(true)

	db, err := openDB(dir, syncWrites, keepVersions)
	if err != nil {
		fatalf("open badger: %v", err)
	}
	defer db.Close()

	fmt.Printf("badger-stress start dir=%s target=%s duration=%s valueSize=%d batchSize=%d shares=%d syncWrites=%v seed=%d\n", dir, humanBytes(targetBytes), duration, valueSize, batchSize, shareCount, syncWrites, seed)

	go statsLoop(ctx, dir, db, c, statsEvery, deadline)

	rng := rand.New(rand.NewSource(seed))
	lastGC := time.Now()
	lastReopen := time.Now()
	checkpointPath := filepath.Join(dir, "_stress_checkpoint.txt")
	seq := loadSeqCheckpoint(checkpointPath)
	if seq > 0 {
		atomic.StoreUint64(&c.lastSeq, seq)
		fmt.Printf("loaded stress checkpoint lastSeq=%d path=%s\n", seq, checkpointPath)
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			fmt.Println("badger-stress received stop signal")
			return
		default:
		}

		disk := mustDirSize(dir)
		atomic.StoreUint64(&c.lastDisk, disk)
		if disk < targetBytes {
			c.phaseGrow.Store(true)
			var err error
			seq, err = growBatch(db, seq, batchSize, valueSize, shareCount)
			atomic.StoreUint64(&c.lastSeq, seq)
			_ = saveSeqCheckpoint(checkpointPath, seq)
			if err != nil {
				atomic.AddUint64(&c.errors, 1)
				fmt.Printf("ERROR grow batch: %v\n", err)
				time.Sleep(time.Second)
				continue
			}
			atomic.AddUint64(&c.sets, uint64(batchSize))
			continue
		}

		c.phaseGrow.Store(false)
		if seq == 0 {
			seq = atomic.LoadUint64(&c.lastSeq)
			if seq == 0 {
				seq = 1
			}
		}
		if err := mutateBatch(db, rng, seq, valueSize, shareCount); err != nil {
			atomic.AddUint64(&c.errors, 1)
			fmt.Printf("ERROR mutate batch: %v\n", err)
			time.Sleep(time.Second)
			continue
		}
		atomic.AddUint64(&c.sets, 70)
		atomic.AddUint64(&c.deletes, 30)
		_ = saveSeqCheckpoint(checkpointPath, seq)

		if gcEvery > 0 && time.Since(lastGC) >= gcEvery {
			lastGC = time.Now()
			if err := db.RunValueLogGC(0.5); err != nil {
				atomic.AddUint64(&c.gcSkipped, 1)
				fmt.Printf("value-log-gc skipped err=%v\n", err)
			} else {
				atomic.AddUint64(&c.gcRuns, 1)
				fmt.Println("value-log-gc completed discardRatio=0.5")
			}
		}

		if reopenEvery > 0 && time.Since(lastReopen) >= reopenEvery {
			lastReopen = time.Now()
			fmt.Println("reopen-check closing database")
			if err := db.Close(); err != nil {
				atomic.AddUint64(&c.errors, 1)
				fmt.Printf("ERROR close for reopen: %v\n", err)
			}
			db, err = openDB(dir, syncWrites, keepVersions)
			if err != nil {
				atomic.AddUint64(&c.errors, 1)
				fatalf("reopen badger failed: %v", err)
			}
			atomic.AddUint64(&c.reopens, 1)
			fmt.Println("reopen-check opened database")
		}
	}

	fmt.Println("badger-stress completed duration")
}

func loadSeqCheckpoint(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var seq uint64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &seq); err != nil {
		return 0
	}
	return seq
}

func saveSeqCheckpoint(path string, seq uint64) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d\n", seq)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func openDB(dir string, syncWrites bool, versions int) (*badger.DB, error) {
	opts := badger.DefaultOptions(dir).
		WithLogger(nil).
		WithSyncWrites(syncWrites).
		WithNumVersionsToKeep(versions)
	// Keep the stress run intentionally memory-bounded. The first unattended run
	// reached ~5.75GiB Sys memory and was OOM-killed around 8.4GiB apparent DB
	// size, so use smaller memtables/caches while preserving the large-DB shape.
	opts = opts.
		WithMemTableSize(32 << 20).
		WithNumMemtables(2).
		WithBaseTableSize(16 << 20).
		WithBlockCacheSize(128 << 20).
		WithIndexCacheSize(128 << 20).
		WithNumCompactors(2).
		WithNumLevelZeroTables(4).
		WithNumLevelZeroTablesStall(8).
		WithValueLogFileSize(1 << 30)
	return badger.Open(opts)
}

func growBatch(db *badger.DB, start uint64, batchSize, valueSize, shareCount int) (uint64, error) {
	wb := db.NewWriteBatch()
	defer wb.Cancel()
	seq := start
	for i := 0; i < batchSize; i++ {
		seq++
		key := makeKey(seq, shareCount, "manifest")
		value := makeValue(seq, valueSize)
		if err := wb.Set(key, value); err != nil {
			return seq, err
		}
		if seq%5 == 0 {
			idxKey := makeKey(seq, shareCount, "block-index")
			idxVal := makeValue(seq^0xBAD600D, valueSize/2)
			if err := wb.Set(idxKey, idxVal); err != nil {
				return seq, err
			}
		}
	}
	return seq, wb.Flush()
}

func mutateBatch(db *badger.DB, rng *rand.Rand, maxSeq uint64, valueSize, shareCount int) error {
	return db.Update(func(txn *badger.Txn) error {
		for i := 0; i < 70; i++ {
			seq := uint64(rng.Int63n(int64(maxSeq))) + 1
			kind := "manifest"
			if rng.Intn(4) == 0 {
				kind = "block-index"
			}
			if err := txn.Set(makeKey(seq, shareCount, kind), makeValue(seq^uint64(time.Now().UnixNano()), valueSize)); err != nil {
				return err
			}
		}
		for i := 0; i < 30; i++ {
			seq := uint64(rng.Int63n(int64(maxSeq))) + 1
			kind := "manifest"
			if rng.Intn(4) == 0 {
				kind = "block-index"
			}
			if err := txn.Delete(makeKey(seq, shareCount, kind)); err != nil && err != badger.ErrKeyNotFound {
				return err
			}
		}
		return nil
	})
}

func makeKey(seq uint64, shareCount int, kind string) []byte {
	share := seq % uint64(shareCount)
	h := fnv.New64a()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], seq)
	_, _ = h.Write(buf[:])
	return []byte(fmt.Sprintf("fse/v1/share/%04d/%s/%016x/%020d", share, kind, h.Sum64(), seq))
}

func makeValue(seq uint64, size int) []byte {
	if size < 256 {
		size = 256
	}
	out := make([]byte, size)
	binary.BigEndian.PutUint64(out[0:8], seq)
	for off := 8; off < size; off += sha256.Size {
		var b [16]byte
		binary.BigEndian.PutUint64(b[0:8], seq)
		binary.BigEndian.PutUint64(b[8:16], uint64(off))
		sum := sha256.Sum256(b[:])
		copy(out[off:], sum[:])
	}
	// Make the front look like metadata with hashes, sizes, and path-like fields.
	digest := sha256.Sum256(out[64:])
	hexDigest := hex.EncodeToString(digest[:])
	prefix := fmt.Sprintf("{\"seq\":%d,\"path\":\"folder-%04d/file-%020d.bin\",\"sha256\":\"%s\",\"blocks\":[", seq, seq%64, seq, hexDigest)
	copy(out, []byte(prefix))
	return out
}

func statsLoop(ctx context.Context, dir string, db *badger.DB, c *counters, every time.Duration, deadline time.Time) {
	if every <= 0 {
		every = 30 * time.Second
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			lsm, vlog := db.Size()
			atomic.StoreUint64(&c.lastLSM, uint64(lsm))
			atomic.StoreUint64(&c.lastVLog, uint64(vlog))
			disk := mustDirSize(dir)
			phase := "mutate"
			if c.phaseGrow.Load() {
				phase = "grow"
			}
			fmt.Printf("STATS ts=%s phase=%s disk=%s lsm=%s vlog=%s sets=%d deletes=%d gcRuns=%d gcSkipped=%d reopens=%d errors=%d lastSeq=%d heap=%s sys=%s goroutines=%d remaining=%s\n",
				time.Now().Format(time.RFC3339), phase, humanBytes(disk), humanBytes(uint64(lsm)), humanBytes(uint64(vlog)),
				atomic.LoadUint64(&c.sets), atomic.LoadUint64(&c.deletes), atomic.LoadUint64(&c.gcRuns), atomic.LoadUint64(&c.gcSkipped), atomic.LoadUint64(&c.reopens), atomic.LoadUint64(&c.errors), atomic.LoadUint64(&c.lastSeq),
				humanBytes(mem.HeapAlloc), humanBytes(mem.Sys), runtime.NumGoroutine(), time.Until(deadline).Round(time.Second))
		}
	}
}

func mustDirSize(root string) uint64 {
	var total uint64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += uint64(info.Size())
		return nil
	})
	return total
}

func humanBytes(v uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	f := float64(v)
	u := 0
	for f >= 1024 && u < len(units)-1 {
		f /= 1024
		u++
	}
	if u == 0 {
		return fmt.Sprintf("%d%s", v, units[u])
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".") + units[u]
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
