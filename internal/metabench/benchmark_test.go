package metabench

import (
	"context"
	"testing"
)

func TestDefaultCandidateOrderEvaluatesPebbleBeforeFallbacks(t *testing.T) {
	got := DefaultCandidateNames()
	want := []string{"pebble", "badger", "bbolt"}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate order = %v, want %v", got, want)
		}
	}

	candidates := DefaultCandidates()
	if len(candidates) != len(want) {
		t.Fatalf("default candidate count = %d, want %d", len(candidates), len(want))
	}
	for i := range want {
		if candidates[i].Name() != want[i] {
			t.Fatalf("default candidates = %v, want %v", candidateNames(candidates), want)
		}
	}
}

func TestWorkloadExercisesImportUpdatesLookupsConcurrentReadersAndReopen(t *testing.T) {
	candidate := &recordingCandidate{}
	result, err := RunCandidate(context.Background(), candidate, WorkloadConfig{
		Folders:         2,
		FilesPerFolder:  3,
		BlocksPerFile:   2,
		LazyHashUpdates: 4,
		LookupCount:     5,
		StatusReaders:   2,
	})
	if err != nil {
		t.Fatalf("RunCandidate returned error: %v", err)
	}
	wantEvents := []string{"open", "large-import", "lazy-hash-updates", "content-lookups", "status-readers", "close", "reopen", "verify-after-reopen", "close"}
	if len(candidate.events) != len(wantEvents) {
		t.Fatalf("events = %v, want %v", candidate.events, wantEvents)
	}
	for i := range wantEvents {
		if candidate.events[i] != wantEvents[i] {
			t.Fatalf("events = %v, want %v", candidate.events, wantEvents)
		}
	}
	if result.ImportedFiles != 6 || result.ImportedBlocks != 12 || result.LazyHashUpdates != 4 || result.ContentLookups != 5 || result.StatusReads == 0 || !result.Reopened {
		t.Fatalf("unexpected workload result: %+v", result)
	}
}

func TestReportIncludesHostFactsAndWarnsAgainstSingleHostSelection(t *testing.T) {
	report := FormatMarkdownReport([]Result{{Candidate: "pebble", ImportedFiles: 1}}, HostFacts{OS: "linux", Kernel: "test-kernel", CPU: "test-cpu", RAM: "1 GiB", WorkingDir: "/tmp/fse-bench", LoadAverage: "0.1 0.2 0.3"})
	for _, needle := range []string{"test-kernel", "test-cpu", "/tmp/fse-bench", "single host", "repeat finalists on other hardware", "pebble"} {
		if !contains(report, needle) {
			t.Fatalf("report missing %q:\n%s", needle, report)
		}
	}
}

type recordingCandidate struct {
	events []string
}

func (c *recordingCandidate) Name() string { return "recording" }

func (c *recordingCandidate) Open(path string) (Store, error) {
	c.events = append(c.events, "open")
	return &recordingStore{candidate: c}, nil
}

type recordingStore struct {
	candidate *recordingCandidate
}

func (s *recordingStore) ImportFiles(ctx context.Context, folders, filesPerFolder, blocksPerFile int) error {
	s.candidate.events = append(s.candidate.events, "large-import")
	return nil
}

func (s *recordingStore) UpdateLazyHashes(ctx context.Context, updates int) error {
	s.candidate.events = append(s.candidate.events, "lazy-hash-updates")
	return nil
}

func (s *recordingStore) LookupContentHashes(ctx context.Context, lookups int) error {
	s.candidate.events = append(s.candidate.events, "content-lookups")
	return nil
}

func (s *recordingStore) RunStatusReaders(ctx context.Context, readers int) (int, error) {
	s.candidate.events = append(s.candidate.events, "status-readers")
	return readers, nil
}

func (s *recordingStore) VerifyAfterReopen(ctx context.Context) error {
	s.candidate.events = append(s.candidate.events, "verify-after-reopen")
	return nil
}

func (s *recordingStore) Close() error {
	s.candidate.events = append(s.candidate.events, "close")
	return nil
}

func (c *recordingCandidate) Reopen(path string) (Store, error) {
	c.events = append(c.events, "reopen")
	return &recordingStore{candidate: c}, nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func candidateNames(candidates []Candidate) []string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.Name())
	}
	return names
}
