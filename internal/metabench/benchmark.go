package metabench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Candidate interface {
	Name() string
	Open(path string) (Store, error)
	Reopen(path string) (Store, error)
}

type Store interface {
	ImportFiles(ctx context.Context, folders, filesPerFolder, blocksPerFile int) error
	UpdateLazyHashes(ctx context.Context, updates int) error
	LookupContentHashes(ctx context.Context, lookups int) error
	RunStatusReaders(ctx context.Context, readers int) (int, error)
	VerifyAfterReopen(ctx context.Context) error
	Close() error
}

type WorkloadConfig struct {
	Folders         int
	FilesPerFolder  int
	BlocksPerFile   int
	LazyHashUpdates int
	LookupCount     int
	StatusReaders   int
}

type Result struct {
	Candidate       string
	ImportedFiles   int
	ImportedBlocks  int
	LazyHashUpdates int
	ContentLookups  int
	StatusReads     int
	Reopened        bool
	Duration        time.Duration
}

type HostFacts struct {
	OS          string
	Arch        string
	Kernel      string
	CPU         string
	RAM         string
	WorkingDir  string
	LoadAverage string
	GoVersion   string
}

func DefaultCandidateNames() []string {
	return []string{"pebble", "badger", "bbolt"}
}

func DefaultWorkloadConfig() WorkloadConfig {
	return WorkloadConfig{
		Folders:         4,
		FilesPerFolder:  50,
		BlocksPerFile:   4,
		LazyHashUpdates: 200,
		LookupCount:     200,
		StatusReaders:   4,
	}
}

func RunCandidate(ctx context.Context, candidate Candidate, cfg WorkloadConfig) (Result, error) {
	cfg = normalizeConfig(cfg)
	dir, err := os.MkdirTemp("", "fse-metabench-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)

	start := time.Now()
	store, err := candidate.Open(filepath.Join(dir, candidate.Name()))
	if err != nil {
		return Result{}, err
	}
	if err := store.ImportFiles(ctx, cfg.Folders, cfg.FilesPerFolder, cfg.BlocksPerFile); err != nil {
		store.Close()
		return Result{}, err
	}
	if err := store.UpdateLazyHashes(ctx, cfg.LazyHashUpdates); err != nil {
		store.Close()
		return Result{}, err
	}
	if err := store.LookupContentHashes(ctx, cfg.LookupCount); err != nil {
		store.Close()
		return Result{}, err
	}
	reads, err := store.RunStatusReaders(ctx, cfg.StatusReaders)
	if err != nil {
		store.Close()
		return Result{}, err
	}
	if err := store.Close(); err != nil {
		return Result{}, err
	}

	store, err = candidate.Reopen(filepath.Join(dir, candidate.Name()))
	if err != nil {
		return Result{}, err
	}
	if err := store.VerifyAfterReopen(ctx); err != nil {
		store.Close()
		return Result{}, err
	}
	if err := store.Close(); err != nil {
		return Result{}, err
	}

	return Result{
		Candidate:       candidate.Name(),
		ImportedFiles:   cfg.Folders * cfg.FilesPerFolder,
		ImportedBlocks:  cfg.Folders * cfg.FilesPerFolder * cfg.BlocksPerFile,
		LazyHashUpdates: cfg.LazyHashUpdates,
		ContentLookups:  cfg.LookupCount,
		StatusReads:     reads,
		Reopened:        true,
		Duration:        time.Since(start),
	}, nil
}

func normalizeConfig(cfg WorkloadConfig) WorkloadConfig {
	def := DefaultWorkloadConfig()
	if cfg.Folders <= 0 {
		cfg.Folders = def.Folders
	}
	if cfg.FilesPerFolder <= 0 {
		cfg.FilesPerFolder = def.FilesPerFolder
	}
	if cfg.BlocksPerFile <= 0 {
		cfg.BlocksPerFile = def.BlocksPerFile
	}
	if cfg.LazyHashUpdates <= 0 {
		cfg.LazyHashUpdates = def.LazyHashUpdates
	}
	if cfg.LookupCount <= 0 {
		cfg.LookupCount = def.LookupCount
	}
	if cfg.StatusReaders <= 0 {
		cfg.StatusReaders = def.StatusReaders
	}
	return cfg
}

func FormatMarkdownReport(results []Result, host HostFacts) string {
	var b strings.Builder
	b.WriteString("# Metadata DB Candidate Benchmark\n\n")
	b.WriteString("## Host facts\n\n")
	fmt.Fprintf(&b, "- OS/arch: %s/%s\n", value(host.OS), value(host.Arch))
	fmt.Fprintf(&b, "- Kernel: %s\n", value(host.Kernel))
	fmt.Fprintf(&b, "- CPU: %s\n", value(host.CPU))
	fmt.Fprintf(&b, "- RAM: %s\n", value(host.RAM))
	fmt.Fprintf(&b, "- Working directory/storage path: %s\n", value(host.WorkingDir))
	fmt.Fprintf(&b, "- Load average: %s\n", value(host.LoadAverage))
	fmt.Fprintf(&b, "- Go version: %s\n\n", value(host.GoVersion))
	b.WriteString("## Results\n\n")
	b.WriteString("| Candidate | Imported files | Imported blocks | Lazy hash updates | Content lookups | Status reads | Reopened | Duration |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: |\n")
	for _, result := range results {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %t | %s |\n", result.Candidate, result.ImportedFiles, result.ImportedBlocks, result.LazyHashUpdates, result.ContentLookups, result.StatusReads, result.Reopened, result.Duration)
	}
	b.WriteString("\n## Interpretation\n\n")
	b.WriteString("These measurements are a same-host comparison only. Do not make the final metadata DB selection from a single host; treat this as low-end stress evidence and repeat finalists on other hardware before selecting the production backend.\n")
	return b.String()
}

func CollectHostFacts(workingDir string) HostFacts {
	return HostFacts{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		WorkingDir: workingDir,
		GoVersion:  runtime.Version(),
	}
}

func value(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}
